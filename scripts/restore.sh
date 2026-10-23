#!/usr/bin/env bash
# SOJ 恢复脚本：解密 age 备份归档 → 恢复 MongoDB（mongorestore --drop）与测试数据卷。
#
# 用法:
#   ./scripts/restore.sh <soj-backup-*.v1.tar.age> <私钥文件> [--yes]
#   ./scripts/restore.sh <soj-backup-*.v1.tar.age> --pull-key [--yes]   # Tier 0：私钥从服务器密钥卷取
#   ./scripts/restore.sh <soj-backup-*.v1.tar.age> --pull-key --verify  # 只解密+完整性校验，不恢复（备份巡检用）
#   ./scripts/restore.sh <旧版明文备份目录> [--yes]                       # 兼容 v0.4.9 前的目录式备份
#
# 注意:
#   - Tier 1 必须用「启用离线私钥」时一次性保存的私钥文件；丢失 = 永久无法恢复；
#   - 恢复会覆盖现有数据库与测试数据卷（mongorestore --drop），默认二次确认，--yes 跳过；
#   - age 解密在一次性 alpine 容器内完成，宿主机无需安装 age；mongorestore 用 mongo 容器内置的。
set -euo pipefail

ARCHIVE=${1:-}
KEY=${2:-}
VERIFY=0
YES=0
for arg in "${@:3}"; do
  case "$arg" in
    --verify) VERIFY=1 ;;
    --yes) YES=1 ;;
  esac
done
COMPOSE=${COMPOSE:-docker compose}

[ -n "$ARCHIVE" ] || { echo "用法: $0 <soj-backup-*.v1.tar.age> <私钥文件|--pull-key> [--yes]"; exit 1; }
[ -e "$ARCHIVE" ] || { echo "!! 归档不存在: $ARCHIVE"; exit 1; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

if [ -d "$ARCHIVE" ]; then
  # ---- 旧版明文目录式备份（mongo.archive.gz + testdata.tar.gz 两文件）----
  echo "==> 检测到旧版明文目录备份，直接恢复"
  [ -f "$ARCHIVE/mongo.archive.gz" ] && [ -f "$ARCHIVE/testdata.tar.gz" ] \
    || { echo "!! 目录里缺少 mongo.archive.gz / testdata.tar.gz"; exit 1; }
  cp "$ARCHIVE/mongo.archive.gz" "$ARCHIVE/testdata.tar.gz" "$WORK/"
elif [[ "$ARCHIVE" == *.tar.age ]]; then
  # ---- v0.4.9 加密归档 ----
  if [ "$KEY" = "--pull-key" ]; then
    echo "==> 从服务器密钥卷取 Tier 0 私钥（仅经本机管道进临时目录，退出即清理）"
    $COMPOSE exec -T soj cat /data/keys/backup.key > "$WORK/backup.key"
    KEY="$WORK/backup.key"
  fi
  [ -n "$KEY" ] && [ -f "$KEY" ] || {
    echo "!! 需要私钥文件：Tier 1 用启用离线私钥时保存的那份；Tier 0 可传 --pull-key"
    exit 1
  }
  echo "==> 解密 $(basename "$ARCHIVE")（alpine 容器内 age，宿主机无需安装）"
  docker run --rm -i \
    -v "$WORK":/w -v "$(cd "$(dirname "$KEY")" && pwd)":/keydir:ro \
    alpine:3.21 \
    sh -c "apk add --no-cache age >/dev/null 2>&1 && age -d -i \"/keydir/$(basename "$KEY")\" -o /w/backup.tar" \
    < "$ARCHIVE" \
    || {
      echo "!! 解密失败：私钥不匹配或归档损坏。"
      echo "   Tier 1 私钥丢失 = 加密备份永久无法恢复；Tier 0 请用 --pull-key 从服务器密钥卷取。"
      exit 1
    }
  tar -xf "$WORK/backup.tar" -C "$WORK"
else
  echo "!! 无法识别的归档格式（期望 soj-backup-*.v1.tar.age 或旧版备份目录）"
  exit 1
fi

echo "归档内容："
ls -lh "$WORK" | grep -Ev "^total|^d" || true

# --verify：只解密 + 完整性校验（gzip/tar 结构），不恢复——备份巡检/演练第一步
if [ "$VERIFY" = "1" ]; then
  NEED_DB=0
  case "$ARCHIVE" in
    *soj-backup-db-*) NEED_DB=1 ;;
    *) [ -f "$WORK/mongo.archive.gz" ] && NEED_DB=1 ;;
  esac
  if [ "$NEED_DB" = "1" ] && [ ! -f "$WORK/mongo.archive.gz" ]; then
    echo "!! db 层归档缺少 mongo.archive.gz"; exit 1
  fi
  for gz in "$WORK"/*.gz; do
    [ -e "$gz" ] || continue
    gzip -t "$gz" || { echo "!! 完整性校验失败: $(basename "$gz")"; exit 1; }
    echo "gzip 校验通过: $(basename "$gz")"
  done
  echo "✅ 归档可解密、结构完整（未执行恢复）"
  exit 0
fi

[ -f "$WORK/mongo.archive.gz" ] || [ -f "$WORK/testdata.tar.gz" ] \
  || { echo "!! 归档内缺少 mongo.archive.gz / testdata.tar.gz（版本不符？）"; exit 1; }

if [ "$YES" != "1" ]; then
  echo ""
  echo "警告：恢复将【覆盖】现有数据库（mongorestore --drop）与测试数据卷。"
  read -r -p "确认恢复？输入 yes 继续: " ANSWER
  [ "$ANSWER" = "yes" ] || { echo "已取消"; exit 1; }
fi

if [ -f "$WORK/mongo.archive.gz" ]; then
  echo "==> 恢复 MongoDB（--drop 覆盖）"
  $COMPOSE exec -T mongo mongorestore --drop --archive --gzip < "$WORK/mongo.archive.gz"
fi

if [ -f "$WORK/testdata.tar.gz" ]; then
  echo "==> 恢复测试数据卷"
  $COMPOSE exec -T soj sh -c 'tar -xzf - -C /data/testdata' < "$WORK/testdata.tar.gz"
else
  echo "==> db 层归档：仅恢复数据库（测试数据卷不动）"
fi

if [ -f "$WORK/uploads.tar.gz" ]; then
  echo "==> 恢复题面图片"
  $COMPOSE exec -T soj sh -c 'mkdir -p /data/uploads && tar -xzf - -C /data/uploads' < "$WORK/uploads.tar.gz"
else
  echo "==> 归档无题面图片包（v0.4.9 及更早的备份），跳过"
fi

echo "==> 重启站点容器（清掉内存缓存，按恢复后的数据重新加载）"
$COMPOSE restart soj

echo "==> 恢复完成"

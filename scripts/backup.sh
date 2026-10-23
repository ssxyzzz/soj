#!/usr/bin/env bash
# SOJ 分层加密备份脚本（v0.15.1）：走服务端 internal/backup 同一加密模块（age 密文归档）。
#
# 分层（按数据变化频率，面向万题库设计）：
#   db     每日（cron）：仅数据库（用户/提交/成绩/题目元数据，MB 级），保留 30 份
#   media  每周（cron）：测试数据卷 + 题面图片（低频变更、体积大头），保留 4 份
#   all    全量（面板按钮 / 手动兼容），保留 4 份
#
# 用法: ./scripts/backup.sh [--tier db|media|all] [--with-snapshots] [--offsite] [--install-cron] [输出目录]
#   --with-snapshots  同时做本地硬链接快照（testdata+uploads 每日回滚点，保留 14 个，
#                     未变更文件零占用；需 compose 里 backup_snaps 卷）
#   --offsite         备份成功后把密文副本 rsync 到异地（SOJ_OFFSITE 环境变量 = ssh 别名；
#                     保留最近 30 份 db + 1 份 media/all）
#   --install-cron    安装两条 crontab（每日 db+快照 04:17 / 每周日 media 04:20），幂等
#   输出目录默认 ./backups（密文另在服务器 /data/backups 自动留存一份）
set -euo pipefail

TIER=all
WITH_SNAPSHOTS=0
OFFSITE=0
INSTALL_CRON=0
OUT="backups"
while [ $# -gt 0 ]; do
  case "$1" in
    --tier) TIER="${2:-}"; shift 2 ;;
    --tier=*) TIER="${1#--tier=}"; shift ;;
    db|media|all) TIER="$1"; shift ;;
    --with-snapshots) WITH_SNAPSHOTS=1; shift ;;
    --offsite) OFFSITE=1; shift ;;
    --install-cron) INSTALL_CRON=1; shift ;;
    -*) echo "未知参数: $1"; exit 2 ;;
    *) OUT="$1"; shift ;;
  esac
done
case "$TIER" in db|media|all) ;; *) echo "--tier 需为 db / media / all"; exit 2 ;; esac

COMPOSE=${COMPOSE:-docker compose}
LOG_TAG="[$(date '+%F %T')][tier=$TIER]"

log() { echo "$LOG_TAG $*"; }
die() { echo "$LOG_TAG !! $*" >&2; exit 1; }

# 防重入：手动备份与 cron 撞车时后到者直接退出
exec 9>/tmp/soj-backup.lock
flock -n 9 || { log "已有备份在跑，跳过本次"; exit 0; }

# ---- cron 安装（幂等：按标记行替换）----
if [ "$INSTALL_CRON" = "1" ]; then
  OFF_FLAG=""
  [ "$OFFSITE" = "1" ] && OFF_FLAG=" --offsite"
  CRON_SOJ="# SOJ-BACKUP-CRON"
  LINE1="$CRON_SOJ 17 4 * * * cd \$HOME/soj && ./scripts/backup.sh --tier db --with-snapshots$OFF_FLAG >> backups/cron.log 2>&1"
  LINE2="$CRON_SOJ 20 4 * * 0 cd \$HOME/soj && ./scripts/backup.sh --tier media$OFF_FLAG >> backups/cron.log 2>&1"
  (crontab -l 2>/dev/null | grep -v "$CRON_SOJ"; echo "$LINE1"; echo "$LINE2") | crontab - \
    || die "crontab 安装失败（服务器上执行）"
  echo "已安装备份 crontab："
  crontab -l | grep "$CRON_SOJ"
  exit 0
fi

mkdir -p "$OUT"

# ---- 本地硬链接快照（testdata/uploads 每日回滚点；未变更文件零占用）----
# 时间戳经 -e 环境变量传入容器脚本，脚本体零拼接（无注入面）
if [ "$WITH_SNAPSHOTS" = "1" ]; then
  log "==> 本地硬链接快照（保留 14 个）"
  TS=$(date +%Y%m%d-%H%M%S)
  $COMPOSE exec -T -e SOJ_SNAP_TS="$TS" soj sh -c '
    set -e
    mkdir -p "/data/snaps/$SOJ_SNAP_TS/testdata" "/data/snaps/$SOJ_SNAP_TS/uploads"
    cp -al /data/testdata/. "/data/snaps/$SOJ_SNAP_TS/testdata/" 2>/dev/null || true
    cp -al /data/uploads/.  "/data/snaps/$SOJ_SNAP_TS/uploads/"  2>/dev/null || true
    ls -1d /data/snaps/20*/ 2>/dev/null | sort -r | tail -n +15 | xargs -r rm -rf
  ' || log "!! 快照失败（backup_snaps 卷未挂载或镜像缺 cp -al？）——继续备份流程"
  log "snapshot: $TS"
fi

# ---- 服务端加密备份（密钥模式自动：未初始化→Tier 0，已有密钥沿用）----
log "==> 触发服务端加密备份（分层: $TIER）"
$COMPOSE exec -T soj /app/soj --backup-run --backup-tier "$TIER" || die "服务端备份失败"

case "$TIER" in
  db)    PATTERN='soj-backup-db-*' ;;
  media) PATTERN='soj-backup-media-*' ;;
  *)     PATTERN='soj-backup-2*' ;;
esac
# 模式经 -e 环境变量传入容器脚本，脚本体零拼接（无注入面）
FILE=$($COMPOSE exec -T -e SOJ_BK_PATTERN="$PATTERN" soj sh -c 'ls -t /data/backups/$SOJ_BK_PATTERN.v1.tar.age 2>/dev/null | head -1')
[ -n "$FILE" ] || die "备份产物未找到（pattern: $PATTERN）"

NAME=$(basename "$FILE")
log "==> 复制 $NAME 到本机 $OUT/"
$COMPOSE cp "soj:$FILE" "$OUT/"

# ---- 本机保留清理（与服务端同口径：db 30 / media·all 4）----
case "$TIER" in
  db) KEEP=30 ;;
  *) KEEP=4 ;;
esac
ls -1t "$OUT/$PATTERN.v1.tar.age" 2>/dev/null | tail -n +"$((KEEP + 1))" | while read -r old; do
  rm -f "$old" && log "清理超保留旧档: $(basename "$old")"
done

# ---- 异地副本（可选：SOJ_OFFSITE = ssh 别名；最近 30 份 db + 1 份全量/媒体）----
if [ "$OFFSITE" = "1" ]; then
  OFF_HOST="${SOJ_OFFSITE:?--offsite 需要 SOJ_OFFSITE 环境变量（ssh 别名）}"
  log "==> 异地副本 → $OFF_HOST:~/soj-offsite/"
  rsync -a "$OUT/$NAME" "$OFF_HOST:~/soj-offsite/" || die "异地 rsync 失败"
  ssh "$OFF_HOST" '
    cd ~/soj-offsite 2>/dev/null || exit 0
    ls -1t soj-backup-db-*.v1.tar.age 2>/dev/null | tail -n +31 | xargs -r rm -f
    ls -1t soj-backup-media-*.v1.tar.age soj-backup-2*.v1.tar.age 2>/dev/null | tail -n +2 | xargs -r rm -f
  ' || log "!! 异地端保留清理失败（不影响本机备份）"
fi

log "==> 完成: $OUT/$NAME"
log "    age 密文归档（服务器留存一份）；校验: ./scripts/restore.sh $OUT/$NAME --pull-key --verify"
ls -lh "$OUT" | tail -5

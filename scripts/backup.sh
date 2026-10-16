#!/usr/bin/env bash
# SOJ 备份脚本：走服务端 internal/backup 同一加密打包模块（age 密文归档）。
# 产物落服务器备份卷（/data/backups，坏盘恢复依据），并复制一份到本机（异地副本）。
# 用法: ./scripts/backup.sh [本机输出目录]（默认 ./backups）
set -euo pipefail

OUT="${1:-backups}"
mkdir -p "$OUT"

COMPOSE=${COMPOSE:-docker compose}

echo "==> 触发服务端加密备份（密钥模式自动：未初始化→Tier 0，已有密钥沿用）"
$COMPOSE exec -T soj /app/soj --backup-run

FILE=$($COMPOSE exec -T soj sh -c "ls -t /data/backups/soj-backup-*.v1.tar.age 2>/dev/null | head -1")
if [ -z "$FILE" ]; then
  echo "!! 备份产物未找到" >&2
  exit 1
fi

echo "==> 复制 $(basename "$FILE") 到本机 $OUT/"
$COMPOSE cp soj:"$FILE" "$OUT/"

echo "==> 完成: $OUT/$(basename "$FILE")"
echo "    age 密文归档（服务器留存一份）；解密恢复: ./scripts/restore.sh <归档> <私钥文件|--pull-key>"
ls -lh "$OUT"

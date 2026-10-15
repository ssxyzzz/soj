#!/usr/bin/env bash
# SOJ 备份脚本：导出 MongoDB + 打包测试数据卷。
# 用法: ./scripts/backup.sh [输出目录]（默认 ./backups/<日期时间>）
set -euo pipefail

OUT="${1:-backups/$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$OUT"

COMPOSE=${COMPOSE:-docker compose}

echo "==> 导出 MongoDB 到 $OUT/mongo.archive.gz"
$COMPOSE exec -T mongo mongodump --uri "mongodb://localhost:27017/?directConnection=true" --archive --gzip > "$OUT/mongo.archive.gz"

echo "==> 打包测试数据卷到 $OUT/testdata.tar.gz"
docker run --rm -v soj_testdata:/data:ro -v "$(cd "$OUT" && pwd)":/backup alpine:3.21 \
  tar czf /backup/testdata.tar.gz -C /data .

echo "==> 完成: $OUT"
ls -lh "$OUT"

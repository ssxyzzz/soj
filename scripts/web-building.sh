#!/usr/bin/env bash
# 编译前端（在 node:22-alpine 容器内执行，本机无需安装 Node）。
# 产物:
#   web/dist/                    前端静态资源
#   server/internal/webui/dist/  供后端 embed 的副本（build-server 依赖它）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "==> npm install + vite build（node:22-alpine 容器）"
docker run --rm -v "$ROOT/web":/src -w /src -u "$(id -u):$(id -g)" \
  -e HOME=/tmp node:22-alpine \
  sh -c 'npm install --no-audit --no-fund && npm run build'

echo "==> 同步到后端 embed 目录"
rm -rf "$ROOT/server/internal/webui/dist"
cp -r "$ROOT/web/dist" "$ROOT/server/internal/webui/dist"

echo "==> 前端编译完成"
echo "    下一步: ./scripts/server-building.sh 编译后端二进制"

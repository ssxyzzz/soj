#!/usr/bin/env bash
# 编译后端（在 golang:1.26-alpine 容器内执行，本机无需安装 Go）。
# 产物: server/bin/soj —— 静态单文件二进制，前端页面已嵌入。
# 注意: 需要先执行 ./scripts/web-building.sh（前端构建产物会被嵌入）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/server/internal/webui/dist"

if [ ! -f "$DIST/index.html" ]; then
  echo "错误: 未找到前端构建产物 $DIST/index.html"
  echo "请先执行: ./scripts/web-building.sh"
  exit 1
fi

echo "==> go build（golang:1.26-alpine 容器，模块代理 goproxy.cn）"
docker run --rm -v "$ROOT/server":/src -w /src -u "$(id -u):$(id -g)" \
  -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath \
  -e GOPROXY=https://goproxy.cn,direct \
  golang:1.26-alpine \
  sh -c 'CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/soj ./cmd/soj'

echo "==> 后端编译完成: server/bin/soj"
echo "    运行示例（需先有 mongo 与沙箱）:"
echo "    SOJ_ADDR=:8080 SOJ_MONGO_URI='mongodb://127.0.0.1:27017/?directConnection=true' \\"
echo "      SOJ_SANDBOX_ADDR=http://127.0.0.1:5050 SOJ_DATA_DIR=/tmp/soj-testdata ./server/bin/soj"

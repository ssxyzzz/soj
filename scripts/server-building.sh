#!/usr/bin/env bash
# 编译后端（在 golang:1.26-alpine 容器内执行，本机无需安装 Go）。
# 产物: server/bin/soj —— 静态单文件二进制，前端页面已嵌入。
# 注意: 需要先执行 ./scripts/web-building.sh（前端构建产物会被嵌入）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_LOG=/tmp/soj-server-building.log

C_INFO='\033[1;36m' C_OK='\033[1;32m' C_ERR='\033[1;31m' C_END='\033[0m'
banner() { echo -e "${C_INFO}════════════════════════════════════════════\n  🚀 $1\n════════════════════════════════════════════${C_END}"; }
step()   { echo -e "${C_INFO}==> $1${C_END}"; }
ok()     { echo -e "${C_OK}✅ $1${C_END}"; }
err()    { echo -e "${C_ERR}❌ $1${C_END}"; }

# 读秒计时器：后台进程每秒刷新一次"已用时"（青色）
TIMER_START=0
TIMER_PID=0
start_timer() {
  TIMER_START=$(date +%s)
  (
    while :; do
      E=$(( $(date +%s) - TIMER_START ))
      printf "\r${C_INFO}⏱ 编译中... %02d:%02d${C_END}    " $((E/60)) $((E%60))
      sleep 1
    done
  ) &
  TIMER_PID=$!
}
stop_timer() {
  kill "$TIMER_PID" 2>/dev/null || true
  wait "$TIMER_PID" 2>/dev/null || true
  E=$(( $(date +%s) - TIMER_START ))
  printf "\r${C_OK}⏱ 本次用时: %02d:%02d${C_END}                                        \n" $((E/60)) $((E%60))
}
on_fail() {
  kill "$TIMER_PID" 2>/dev/null || true
  err "编译失败！日志最后 20 行（$BUILD_LOG）:"
  tail -20 "$BUILD_LOG" 2>/dev/null || true
}
trap on_fail ERR

banner "SOJ 后端编译开始（server-building）"

step "[1/2] go build（golang:1.26-alpine 容器，实时读秒）"
start_timer
docker run --rm -v "$ROOT/server":/src -w /src -u "$(id -u):$(id -g)" \
  -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath \
  -e GOPROXY=https://goproxy.cn,direct \
  golang:1.26-alpine \
  sh -c 'CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/soj ./cmd/soj' > "$BUILD_LOG" 2>&1
stop_timer

step "[2/2] 重建并重启 SOJ 容器（网站即刻更新）"
if [ -n "$(cd "$ROOT" && docker compose ps -q soj 2>/dev/null)" ]; then
  (cd "$ROOT" && docker compose up -d --build soj)
  ok "部署完成！浏览器 Ctrl+F5 强刷即可看到最新版本"
else
  echo -e "${C_INFO}==> 未检测到 SOJ 容器，跳过部署${C_END}"
  echo -e "    需要更新网站时执行: ${C_INFO}cd $ROOT && docker compose up -d --build soj${C_END}"
fi

ok "全部完成！"

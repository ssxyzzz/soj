#!/usr/bin/env bash
# 编译前端（在 node:22-alpine 容器内执行，本机无需安装 Node）。
# 产物:
#   web/dist/                    前端静态资源
#   server/internal/webui/dist/  供后端 embed 的副本（server-building 依赖它）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_LOG=/tmp/soj-web-building.log

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

banner "SOJ 前端编译开始（web-building）"

step "[1/3] npm install + vite build（node:22-alpine 容器，实时读秒）"
start_timer
docker run --rm -v "$ROOT/web":/src -w /src -u "$(id -u):$(id -g)" \
  -e HOME=/tmp node:22-alpine \
  sh -c 'npm install --no-audit --no-fund && npm run build' > "$BUILD_LOG" 2>&1
stop_timer

step "[2/3] 同步到后端 embed 目录"
rm -rf "$ROOT/server/internal/webui/dist"
cp -r "$ROOT/web/dist" "$ROOT/server/internal/webui/dist"

step "[3/3] 重建并重启 SOJ 容器（网站即刻更新）"
if [ -n "$(cd "$ROOT" && docker compose ps -q soj 2>/dev/null)" ]; then
  (cd "$ROOT" && docker compose up -d --build soj)
  ok "部署完成！浏览器 Ctrl+F5 强刷即可看到最新版本"
else
  echo -e "${C_INFO}==> 未检测到 SOJ 容器，跳过部署${C_END}"
  echo -e "    需要更新网站时执行: ${C_INFO}cd $ROOT && docker compose up -d --build soj${C_END}"
fi

ok "全部完成！"

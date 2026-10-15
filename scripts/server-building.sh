#!/usr/bin/env bash
# 编译后端（本机 Go 工具链，模块代理 goproxy.cn）。
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

banner "SOJ 后端编译开始（server-building，本机工具链）"

# 本机工具链：优先用户目录安装，其次系统 PATH
[ -x "$HOME/.local/opt/go/bin/go" ] && export PATH="$HOME/.local/opt/go/bin:$PATH"
[ -d "$HOME/.local/opt/node-v22.21.1-linux-x64" ] && export PATH="$HOME/.local/opt/node-v22.21.1-linux-x64/bin:$PATH"
if ! command -v npm >/dev/null 2>&1; then
  err "找不到 npm。安装方法见 plan.md 部署运维章节（用户目录免 sudo 安装）"
  exit 1
fi

step "[1/2] go build（实时读秒，增量编译有缓存）"
start_timer
(cd "$ROOT/server" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/soj ./cmd/soj) > "$BUILD_LOG" 2>&1
stop_timer

step "[2/2] 热替换进运行中的 SOJ 容器（秒级）"
SOJ_CID="$(cd "$ROOT" && docker compose ps -q soj)"
if [ -n "$SOJ_CID" ]; then
  docker cp "$ROOT/server/bin/soj" "$SOJ_CID":/app/soj
  (cd "$ROOT" && docker compose restart soj)
  ok "部署完成！本机新二进制已热替换（容器重建时会回退为镜像内版本，属正常）"
  echo -e "    如需完整重建镜像: ${C_INFO}docker compose up -d --build soj${C_END}"
else
  echo -e "${C_INFO}==> 未检测到 SOJ 容器，跳过部署${C_END}"
  echo -e "    需要部署网站时执行: ${C_INFO}cd $ROOT && docker compose up -d --build soj${C_END}"
fi

ok "全部完成！"

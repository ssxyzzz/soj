#!/usr/bin/env bash
# 编译后端（本机 Go 工具链，模块代理 goproxy.cn）。
# 产物: server/bin/soj —— 静态单文件二进制，前端页面已嵌入。
# 注意: 需要先执行 ./scripts/web-building.sh（前端构建产物会被嵌入）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG=/tmp/soj-server-building.log
CUR_LOG=$LOG   # on_fail 展示当前失败步骤的日志（run_live 每步更新）

C_INFO='\033[1;36m' C_OK='\033[1;32m' C_ERR='\033[1;31m' C_END='\033[0m'
banner() { echo -e "${C_INFO}════════════════════════════════════════════\n  🚀 $1\n════════════════════════════════════════════${C_END}"; }
step()   { echo -e "${C_INFO}==> $1${C_END}"; }
ok()     { echo -e "${C_OK}✅ $1${C_END}"; }
err()    { echo -e "${C_ERR}❌ $1${C_END}"; }

# ── 实时进度区 ──────────────────────────────────────────────────────────
# run_live <说明> <日志文件> <命令...>：命令后台执行、输出全量落日志；
# 前台每秒重画「计时 1 行 + 日志最新 LIVE_LINES 行」（光标上移重画，不刷屏）。
# 失败时打 ❌ + 用时后返回非零，交由 on_fail 展示日志；Ctrl+C 会一并杀掉后台命令。
LIVE_LINES=3
run_live() {
  local label="$1" log="$2"; shift 2
  local pid start rc=0 e last have first=1
  CUR_LOG="$log"
  start=$(date +%s)
  "$@" >"$log" 2>&1 &
  pid=$!
  trap 'kill "$pid" 2>/dev/null || true' INT TERM
  while kill -0 "$pid" 2>/dev/null; do
    e=$(( $(date +%s) - start ))
    last="$(tail -n "$LIVE_LINES" "$log" 2>/dev/null | cut -c1-"${COLUMNS:-110}")"
    have=0; [ -n "$last" ] && have=$(printf '%s\n' "$last" | wc -l)
    if [ "$first" -eq 1 ]; then first=0; else printf '\033[%dA\033[J' "$((LIVE_LINES + 1))"; fi
    printf "${C_INFO}⏱ %s... %02d:%02d${C_END}\n" "$label" $((e/60)) $((e%60))
    if [ -n "$last" ]; then printf '%s\n' "$last" | sed 's/^/  │ /'; fi
    while [ "$have" -lt "$LIVE_LINES" ]; do printf '\n'; have=$((have+1)); done
    sleep 1
  done
  wait "$pid" || rc=$?
  trap - INT TERM
  e=$(( $(date +%s) - start ))
  printf '\033[%dA\033[J' "$((LIVE_LINES + 1))"
  if [ "$rc" -eq 0 ]; then
    printf "${C_OK}⏱ %s完成，用时 %02d:%02d${C_END}\n" "$label" $((e/60)) $((e%60))
  else
    printf "${C_ERR}❌ %s失败，用时 %02d:%02d${C_END}\n" "$label" $((e/60)) $((e%60))
    return "$rc"
  fi
}
on_fail() {
  err "编译失败！日志最后 20 行（$CUR_LOG）:"
  tail -20 "$CUR_LOG" 2>/dev/null || true
}
trap on_fail ERR

banner "SOJ 后端编译开始（server-building，本机工具链）"

# 本机工具链：优先用户目录安装，其次系统 PATH
[ -x "$HOME/.local/opt/go/bin/go" ] && export PATH="$HOME/.local/opt/go/bin:$PATH"

go_build() {
  cd "$ROOT/server"
  # -v 把正在编译的包名打进行日志：实时区有真实进度可看；热缓存时几乎无输出、秒完
  CGO_ENABLED=0 go build -v -trimpath -ldflags="-s -w" -o bin/soj ./cmd/soj
}

step "[1/2] go build（实时进度；增量编译有缓存）"
run_live "go build" "$LOG" go_build

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

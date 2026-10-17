#!/usr/bin/env bash
# 编译前端并部署（本机 Node + Go 工具链，缺失时自动装到 ~/.local/opt，npm 走 npmmirror）。
# 常规路径 = 宿主机全量构建（前端产物嵌入二进制）+ docker cp 热替换，秒级发版；
# 镜像重建仅在容器不在运行时兜底（平时镜像内版本会落后，用 make image 对齐）。
# 产物:
#   web/dist/                    前端静态资源
#   server/internal/webui/dist/  供后端 embed 的副本（server-building 依赖它）
#   server/bin/soj               含最新前端的二进制（与 server-building 同一路径）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG=/tmp/soj-web-npm.log            # 编译与热替换各步
DOCKER_LOG=/tmp/soj-web-docker.log  # 兜底镜像重建
CUR_LOG=$LOG                        # on_fail 展示当前失败步骤的日志（run_live 每步更新）

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

banner "SOJ 前端编译开始（web-building，本机工具链）"

# 本机工具链：缺 Go/Node 自动装到 ~/.local/opt（见 scripts/toolchain.sh）
source "$ROOT/scripts/toolchain.sh"
ensure_toolchain

web_build() {
  cd "$ROOT/web"
  # 依赖未变化时跳过 npm install：package.json + package-lock.json 的联合哈希
  # 存在 node_modules/.soj-dep-hash 标记文件里，一致则视为依赖树未动（install 成功后才写标记，
  # 半途失败下次仍会重装）
  local sig
  sig="$(sha256sum package.json package-lock.json 2>/dev/null | sha256sum | cut -c1-64)"
  if [ -d node_modules ] && [ -f node_modules/.soj-dep-hash ] \
      && [ "$(cat node_modules/.soj-dep-hash)" = "$sig" ]; then
    echo "依赖未变化（lock 哈希一致），跳过 npm install"
  else
    npm install --no-audit --no-fund
    printf '%s' "$sig" > node_modules/.soj-dep-hash
  fi
  npm run build
}
sync_embed() {
  rm -rf "$ROOT/server/internal/webui/dist"
  cp -r "$ROOT/web/dist" "$ROOT/server/internal/webui/dist"
}
go_build() {
  cd "$ROOT/server"
  # -v 把正在编译的包名打进行日志：实时区有真实进度可看；热缓存时几乎无输出、秒完
  CGO_ENABLED=0 go build -v -trimpath -ldflags="-s -w" -o bin/soj ./cmd/soj
}
deploy_swap() {
  docker cp "$ROOT/server/bin/soj" "$SOJ_CID":/app/soj
  cd "$ROOT" && docker compose restart soj
}
docker_up() {
  cd "$ROOT"
  docker compose up -d --build soj
}

step "[1/4] npm install + vite build（实时进度）"
run_live "前端编译" "$LOG" web_build

step "[2/4] 同步到后端 embed 目录"
run_live "同步 embed" "$LOG" sync_embed

step "[3/4] 编译含新前端的二进制（增量编译有缓存）"
run_live "go build" "$LOG" go_build

step "[4/4] 部署（热替换，秒级）"
SOJ_CID="$(cd "$ROOT" && docker compose ps -q soj)"
if [ -n "$SOJ_CID" ]; then
  run_live "热替换+重启" "$LOG" deploy_swap
  ok "部署完成！浏览器 Ctrl+F5 强刷即可看到最新版本"
  echo -e "    镜像内版本暂落后于热替换版本（容器重建会回退），定期 ${C_INFO}make image${C_END} 对齐"
else
  step "  未检测到运行中的 SOJ 容器，兜底重建镜像并启动"
  run_live "镜像重建+启动" "$DOCKER_LOG" docker_up
  ok "部署完成！浏览器 Ctrl+F5 强刷即可看到最新版本"
fi

ok "全部完成！"

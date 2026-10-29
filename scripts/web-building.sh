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

# 彩色提示与实时进度区与 server-building / check 共用（scripts/progress.sh）
source "$ROOT/scripts/progress.sh"
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
  ensure_web_deps "$ROOT"   # 依赖哈希跳装（toolchain.sh，与 check.sh 共用）
  npm run build
}
sync_embed() {
  rm -rf "$ROOT/server/internal/webui/dist"
  cp -r "$ROOT/web/dist" "$ROOT/server/internal/webui/dist"
}
go_build() {
  cd "$ROOT/server"
  # 版本注入（v0.28 发版可追溯）：与镜像构建同源，/api/health 回显核对。VERSION
  # 环境变量优先（make up/bin 传入——服务器目录无 .git，git describe 会回退 dev，
  # 部署同步脚本从开发机带真实版本过来）
  local ver
  ver="${VERSION:-$(git -C "$ROOT" describe --always --dirty 2>/dev/null || echo dev)}"
  CGO_ENABLED=0 go build -v -trimpath -ldflags="-s -w -X soj/internal/server.Version=$ver" -o bin/soj ./cmd/soj
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
  echo -e "    版本核对：curl localhost/api/health 的 version 字段；例行发版用 ${C_INFO}make up${C_END}（先过门禁、镜像恒对齐，容器重建不回退）"
else
  step "  未检测到运行中的 SOJ 容器，兜底重建镜像并启动"
  run_live "镜像重建+启动" "$DOCKER_LOG" docker_up
  ok "部署完成！浏览器 Ctrl+F5 强刷即可看到最新版本"
fi

ok "全部完成！"

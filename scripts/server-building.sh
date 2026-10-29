#!/usr/bin/env bash
# 编译后端（本机 Go 工具链，缺失时自动装到 ~/.local/opt，模块代理 goproxy.cn）。
# 产物: server/bin/soj —— 静态单文件二进制，前端页面已嵌入。
# 注意: 需要先执行 ./scripts/web-building.sh（前端构建产物会被嵌入）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG=/tmp/soj-server-building.log
CUR_LOG=$LOG   # on_fail 展示当前失败步骤的日志（run_live 每步更新）

# 彩色提示与实时进度区与 web-building / check 共用（scripts/progress.sh）
source "$ROOT/scripts/progress.sh"
on_fail() {
  err "编译失败！日志最后 20 行（$CUR_LOG）:"
  tail -20 "$CUR_LOG" 2>/dev/null || true
}
trap on_fail ERR

banner "SOJ 后端编译开始（server-building，本机工具链）"

# 本机工具链：缺 Go 自动装到 ~/.local/opt（见 scripts/toolchain.sh）
source "$ROOT/scripts/toolchain.sh"
ensure_go

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

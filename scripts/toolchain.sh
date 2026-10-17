# 工具链自举（被 web-building.sh / server-building.sh source，须在 run_live/step 等定义之后 source）：
# 缺 Go / Node 就自动下载安装到 ~/.local/opt（用户目录免 sudo），并配好模块代理与 PATH。
# 系统已有的工具链优先（command -v 命中即跳过安装）。
GO_VERSION=1.27.2     # 满足 go.mod 的 go 1.26；升级只改这里
NODE_VERSION=22.21.1  # 安装目录约定 ~/.local/opt/node-vX；系统已有 npm 则不装
OPT="$HOME/.local/opt"

case "$(uname -m)" in
  x86_64)  GO_ARCH=amd64; NODE_ARCH=x64   ;;
  aarch64) GO_ARCH=arm64; NODE_ARCH=arm64 ;;
  *) echo "不支持的 CPU 架构: $(uname -m)" >&2; exit 1 ;;
esac

# 宿主机 go build 的模块代理（尊重用户已有的 GOPROXY 环境变量）
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

# fetch <url> <目标文件>：curl 优先，wget 兜底（都没有则报错）
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --connect-timeout 15 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -T 15 -t 3 -O "$2" "$1"
  else
    echo "缺少 curl / wget，无法自动下载工具链" >&2; return 1
  fi
}

# add_path_line <bin目录>：幂等地写进 ~/.bashrc，新开终端直接可用
add_path_line() {
  local line="export PATH=\"$1:\$PATH\""
  grep -qsF "$line" "$HOME/.bashrc" && return 0
  echo "$line # soj 工具链自举添加" >> "$HOME/.bashrc"
}

ensure_go() {
  if [ -x "$OPT/go/bin/go" ]; then
    export PATH="$OPT/go/bin:$PATH"
  elif ! command -v go >/dev/null 2>&1; then
    local tgz="$OPT/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    mkdir -p "$OPT"
    step "  未检测到 Go，自动安装 go${GO_VERSION} → $OPT/go（免 sudo）"
    # 主源 golang.google.cn（国内可达），失败换阿里云镜像
    run_live "下载 Go" "$LOG" \
      fetch "https://golang.google.cn/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" "$tgz" \
      || run_live "下载 Go（备用源）" "$LOG" \
        fetch "https://mirrors.aliyun.com/golang/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" "$tgz"
    rm -rf "$OPT/go"   # 残留的半成品/旧版安装目录，解压前清掉
    run_live "解压 Go" "$LOG" tar -C "$OPT" -xzf "$tgz"
    rm -f "$tgz"
    export PATH="$OPT/go/bin:$PATH"
    add_path_line "$OPT/go/bin"
    # 模块代理持久化（make import / soj-dev 等脱离脚本也能拉包）；
    # 检查/写入都临时剥离 GOPROXY 环境变量（本文件已 export），只认 go env 里的持久配置，不覆盖用户设置
    if [ "$(env -u GOPROXY go env GOPROXY 2>/dev/null)" = "https://proxy.golang.org,direct" ]; then
      env -u GOPROXY go env -w GOPROXY=https://goproxy.cn,direct
    fi
    ok "Go 安装完成：$(go env GOVERSION)"
  fi
}

ensure_node() {
  command -v npm >/dev/null 2>&1 && return 0   # 系统已有 Node/npm，直接用
  local dir="$OPT/node-v${NODE_VERSION}-linux-${NODE_ARCH}"
  if [ -x "$dir/bin/npm" ]; then
    export PATH="$dir/bin:$PATH"
    return 0
  fi
  local tgz="$OPT/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.gz"
  mkdir -p "$OPT"
  step "  未检测到 npm，自动安装 Node v${NODE_VERSION} → $dir（免 sudo）"
  # 主源 npmmirror，失败换 nodejs.org 官方
  run_live "下载 Node" "$LOG" \
    fetch "https://npmmirror.com/mirrors/node/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.gz" "$tgz" \
    || run_live "下载 Node（备用源）" "$LOG" \
      fetch "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.gz" "$tgz"
  rm -rf "$dir"
  run_live "解压 Node" "$LOG" tar -C "$OPT" -xzf "$tgz"
  rm -f "$tgz"
  export PATH="$dir/bin:$PATH"
  add_path_line "$dir/bin"
  ok "Node 安装完成：$(node -v) / npm v$(npm -v)"
}

# web-building 用：Go 与 Node 都确保就绪
ensure_toolchain() {
  ensure_node
  ensure_go
  command -v npm >/dev/null 2>&1 || { err "npm 自动安装失败，请手动安装 Node ≥18 后重试"; exit 1; }
  command -v go  >/dev/null 2>&1 || { err "go 自动安装失败，请手动安装 Go ≥1.26 后重试"; exit 1; }
  ok "工具链就绪：$(go env GOVERSION 2>/dev/null) + Node $(node -v 2>/dev/null)"
}

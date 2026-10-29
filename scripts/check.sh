#!/usr/bin/env bash
# 全量门禁：代码检查（ESLint + go vet + staticcheck 如有）+ Go 单测 + 前端类型检查
# （vue-tsc）。Makefile 的 check 目标与 GitHub Actions CI 共用本入口；提示风格与
# 编译/发版脚本一致（banner/step/⏱ 实时进度）。非交互环境（CI、输出重定向）没有
# 终端，光标重画会变成乱码转义序列——退化为直接透传输出（日志仍落盘）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG=/tmp/soj-check.log
CUR_LOG=$LOG  # on_fail 展示当前失败步骤的日志（run_live/run_step 每步更新）

source "$ROOT/scripts/progress.sh"
source "$ROOT/scripts/toolchain.sh"   # ensure_web_deps：typecheck/eslint 吃 node_modules，先保依赖

on_fail() {
  err "门禁未通过！日志最后 20 行（$CUR_LOG）:"
  tail -20 "$CUR_LOG" 2>/dev/null || true
}
trap on_fail ERR

if [ -t 1 ]; then
  run_step() { run_live "$@"; }
else
  run_step() {
    local label="$1" log="$2"; shift 2
    CUR_LOG="$log"
    echo -e "${C_INFO}⏱ ${label}...${C_END}"
    "$@" 2>&1 | tee "$log"
  }
fi

START=$(date +%s)

do_lint()      { make -C "$ROOT" lint; }
do_gotest()    { (cd "$ROOT/server" && go test ./...); }
do_typecheck() { (cd "$ROOT/web" && npm run typecheck); }

banner "SOJ 门禁检查开始（check：lint + go test + typecheck）"

# 依赖自检先行：服务器 rsync 不带 node_modules 且 up 是 check 先于 web-building，
# 依赖变更时不先装会在 eslint/vue-tsc 先炸（lock 哈希一致时零成本跳过）
step "[1/4] 前端依赖自检（lock 哈希一致则跳装 npm install）"
run_step "依赖自检" "$LOG" ensure_web_deps "$ROOT"

step "[2/4] 代码检查（ESLint + go vet + staticcheck 如有）"
run_step "代码检查" "$LOG" do_lint

step "[3/4] Go 单元测试（无本地 Mongo 时按既有约定 skip）"
run_step "go test" "$LOG" do_gotest

step "[4/4] 前端类型检查（vue-tsc）"
run_step "类型检查" "$LOG" do_typecheck

E=$(( $(date +%s) - START )); (( E > 0 )) || E=0
ok "$(printf '门禁全绿：lint + go test + typecheck（总用时 %02d:%02d）' $((E/60)) $((E%60)))"

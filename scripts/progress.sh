# 输出提示与实时进度区（web-building / server-building / check 共用，source 引入）：
# banner/step/ok/err 彩色提示 + run_live「计时 + 日志尾部」每秒重画的实时区。
# 用方自带 LOG/CUR_LOG 变量与各自的 on_fail/trap（失败文案因场景而异）。

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
    e=$(( $(date +%s) - start )); (( e > 0 )) || e=0  # WSL2 NTP 校时会整秒回拨，钳非负
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
  e=$(( $(date +%s) - start )); (( e > 0 )) || e=0
  printf '\033[%dA\033[J' "$((LIVE_LINES + 1))"
  if [ "$rc" -eq 0 ]; then
    printf "${C_OK}⏱ %s完成，用时 %02d:%02d${C_END}\n" "$label" $((e/60)) $((e%60))
  else
    printf "${C_ERR}❌ %s失败，用时 %02d:%02d${C_END}\n" "$label" $((e/60)) $((e%60))
    return "$rc"
  fi
}

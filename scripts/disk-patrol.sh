#!/usr/bin/env bash
# SOJ 磁盘水位巡检（v0.27 长跑体检）：测试数据只增不减（题目无删除）、上传靠配额兜底
# 但无清理、备份虽有保留策略但前置护栏按层各有盲区——磁盘写满是长跑唯一「不做就必然
# 出事」的项（mongo 先死 → 全站 500 → 判题回写丢、恢复后自动重判）。容器日志已随
# compose 统一轮转（json-file 10m×3），本脚本只管水位。
#
# 用法: ./scripts/disk-patrol.sh [阈值百分比]     # 默认 85
#       ./scripts/disk-patrol.sh --install-cron   # 安装每日 08:30 巡检 cron（幂等）
# 超阈值：输出告警明细（cron 重定向进 backups/cron.log）并以退出码 2 结束，
# 方便外层监控接管（uptime 工具可配「脚本退出码非零」报警）。
set -euo pipefail

THRESHOLD=85
INSTALL_CRON=0
while [ $# -gt 0 ]; do
  case "$1" in
    --install-cron) INSTALL_CRON=1 ;;
    -*) echo "未知参数: $1"; exit 2 ;;
    *) THRESHOLD="$1" ;;
  esac
  shift
done
case "$THRESHOLD" in *[!0-9]*|'') echo "阈值需为 1-99 的整数"; exit 2 ;; esac
[ "$THRESHOLD" -ge 1 ] && [ "$THRESHOLD" -le 99 ] || { echo "阈值需为 1-99 的整数"; exit 2; }

TAG="[$(date '+%F %T')][disk-patrol]"
log() { echo "$TAG $*"; }

# ---- cron 安装（幂等：按标记行替换，同 backup.sh 范式） ----
if [ "$INSTALL_CRON" = "1" ]; then
  CRON_SOJ="# SOJ-DISK-PATROL-CRON"
  LINE="$CRON_SOJ 30 8 * * * cd \$HOME/soj && ./scripts/disk-patrol.sh >> backups/cron.log 2>&1"
  (crontab -l 2>/dev/null | grep -v "$CRON_SOJ"; echo "$LINE") | crontab - \
    || { echo "crontab 安装失败（服务器上执行）"; exit 1; }
  echo "已安装磁盘巡检 crontab："
  crontab -l | grep "$CRON_SOJ"
  exit 0
fi

# ---- 水位检查 ----
# 检查对象：根分区（docker 数据根通常在 /）、docker 目录（独立分区时）、仓库备份目录。
PATHS=("/")
[ -d /var/lib/docker ] && PATHS+=("/var/lib/docker")
[ -d backups ] && PATHS+=("$(pwd)/backups")

RC=0
for p in "${PATHS[@]}"; do
  read -r _ total used avail pct _ < <(df -P "$p" | tail -1)
  use=${pct%\%}
  if [ "$use" -ge "$THRESHOLD" ]; then
    log "!! $p 已用 ${use}%（${used}/${total}，剩 ${avail}）≥ 阈值 ${THRESHOLD}%"
    RC=2
  else
    log "ok $p 已用 ${use}%（${used}/${total}，剩 ${avail}）"
  fi
done

if [ "$RC" -ne 0 ]; then
  log "处置参考：docker system df；du -sm /var/lib/docker/volumes/* | sort -rn | head；"
  log "  清理旧备份归档（backup_data 卷按层保留策略自动 prune，手工包需自查）；"
  log "  testdata 只增不减，确认无误解后可整题目录评估收缩。"
fi
exit "$RC"

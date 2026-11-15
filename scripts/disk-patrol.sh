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

# ---- cron 安装（幂等：按行尾标记替换，同 backup.sh 范式） ----
# 标记必须在有效行尾：整行 # 注释 cron 永不执行（2026-11-01 迁移实锤旧机 cron 从未跑过）
if [ "$INSTALL_CRON" = "1" ]; then
  MARK="SOJ-DISK-PATROL-CRON"
  LINE="30 8 * * * cd \$HOME/soj && ./scripts/disk-patrol.sh >> backups/cron.log 2>&1 #$MARK"
  (crontab -l 2>/dev/null | grep -v "$MARK"; echo "$LINE") | crontab - \
    || { echo "crontab 安装失败（服务器上执行）"; exit 1; }
  echo "已安装磁盘巡检 crontab："
  crontab -l | grep "$MARK"
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

# ---- 沙箱文件仓库绊线（v0.97.9）----
# go-judge copyOutCached 文件仓库（-dir 落盘）：判后即清 + 沙箱 -file-timeout 30m TTL
# 双闸下稳态应恒近 0，瞬时峰值 = 并发 worker × 64MB 输出（最坏 ~1.6GB）。巡检时刻
# 超过 5GB = release 或 TTL 回归（判题侧 releaseResultFiles / 沙箱侧 -file-timeout
# 任一失效），按天巡检足以在酿成任何故障前发现——磁盘仓余量 500 倍，绊线求早不求急。
# 无沙箱容器（开发机/未部署）优雅跳过。
SB_STORE_MB=5120
# 退出码判定不能过管道（cut 恒 0 会吃掉 docker exec 的失败，容器缺失误报 ok 0MB）
if du_out=$(docker exec soj-sandbox-1 du -sm /var/lib/go-judge 2>/dev/null); then
  store=$(echo "$du_out" | cut -f1)
  if [ "${store:-0}" -gt "$SB_STORE_MB" ]; then
    log "!! 沙箱文件仓库 ${store}MB > ${SB_STORE_MB}MB——判题缓存释放或沙箱 TTL 疑似回归（正常判后即清、稳态近 0），查 judge 日志「清理测试点缓存文件失败」与 sandbox 启动参数 -file-timeout"
    RC=2
  else
    log "ok 沙箱文件仓库 ${store}MB / 绊线 ${SB_STORE_MB}MB"
  fi
else
  log "skip 沙箱文件仓库（无 soj-sandbox-1 容器，未部署机跳过）"
fi

if [ "$RC" -ne 0 ]; then
  log "处置参考：docker system df；du -sm /var/lib/docker/volumes/* | sort -rn | head；"
  log "  清理旧备份归档（backup_data 卷按层保留策略自动 prune，手工包需自查）；"
  log "  testdata 只增不减，确认无误解后可整题目录评估收缩。"
fi
exit "$RC"

#!/usr/bin/env bash
# 后端性能守卫（v0.66 批③闸门）：挂在 make lint 尾部——lint 报错即行为改变，
# agent 的必然路径就是 reviewer。
#
# 棘轮模式：存量违规冻结为基线（只降不升），新增即 fail；清偿存量后手动下调基线。
# 三条守卫（基线 2026-11-04 冻结）：
#   G1 repo 包无 projection 的 Find 调用（D 类慢查询源头）       基线 41
#   G2 http.Client 字面量无 Timeout（外部调用悬挂源头）           基线 0（硬性）
#   G3 server 包 time.Sleep（请求路径同步睡眠禁令）               基线 0（硬性）
set -uo pipefail
cd "$(dirname "$0")/.."

G1_BASE=40 # v0.66 批④下调 41→40：discuss 列表补 content 投影清偿一处
G2_BASE=0
G3_BASE=0
fail=0

# G1：Find(ctx 后 4 行窗口内无 SetProjection 即计一次（跨行链式调用的粗粒度近似，
# 棘轮口径下足够——只比数值不比语义）
g1=0
for f in server/internal/repo/*.go; do
	case "$f" in *_test.go) continue ;; esac
	n=$(awk '/\.Find\(ctx/ {buf=$0; for(i=0;i<4;i++){if(getline<=0) break; buf=buf"\n"$0} if(buf !~ /SetProjection/) c++} END{print c+0}' "$f")
	g1=$((g1+n))
done
if [ "$g1" -gt "$G1_BASE" ]; then
	echo "❌ 性能守卫 G1：repo 包无 projection 的 Find $g1 处 > 基线 $G1_BASE——新增列表查询必须带 SetProjection（范例 internal/repo/problem.go ListProblems）"
	fail=1
fi

# G2：http.Client{ 字面量 2 行窗口内无 Timeout 即违规（外部调用必须有界）
g2=0
for f in $(grep -rl 'http\.Client{' server/internal --include='*.go' | grep -v _test || true); do
	n=$(awk '/http\.Client\{/ {buf=$0; for(i=0;i<2;i++){if(getline<=0) break; buf=buf"\n"$0} if(buf !~ /Timeout/) c++} END{print c+0}' "$f")
	g2=$((g2+n))
done
if [ "$g2" -gt "$G2_BASE" ]; then
	echo "❌ 性能守卫 G2：http.Client 无 Timeout $g2 处 > 基线 $G2_BASE——外部调用必须带超时（范例 internal/sandbox/sandbox.go）"
	fail=1
fi

# G3：server 包请求路径禁止同步睡眠（等闸/退避走 ctx 感知等待，judge 包同理另议）
g3=$(grep -rn 'time\.Sleep(' server/internal/server --include='*.go' | grep -v _test | wc -l)
if [ "$g3" -gt "$G3_BASE" ]; then
	echo "❌ 性能守卫 G3：server 包 time.Sleep $g3 处 > 基线 $G3_BASE——请求路径禁止同步睡眠"
	fail=1
fi

if [ "$fail" -ne 0 ]; then
	exit 1
fi
echo "性能守卫：G1 ${g1}/${G1_BASE} · G2 ${g2}/${G2_BASE} · G3 ${g3}/${G3_BASE} ✓（棘轮：清偿存量后下调基线，只降不升）"

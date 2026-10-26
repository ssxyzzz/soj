<script setup lang="ts">
// 难度分布堆叠柱状图（v0.18.3；2026-10-26 三轮定稿口径）：
// 柱高按各档题数**相对最大档等比例**拉伸——题目最多的档顶到满，其余按比例；
// 柱内底部档位色段 = 已完成（AC）数占该档比例，上部 #F2F3F5 = 未完成；
// 上下全圆角胶囊（8px），无题的档不画。配色同源 main.css .diff-* 十档 --dc。
import { computed } from 'vue'

import { useLevelsStore } from '@/stores/levels'

const props = withDefaults(
  defineProps<{
    total: number[] // 每档题目数（D1–D10，长度 10）
    solved?: number[] // 每档已完成数（缺省全 0）
    height?: number // 柱区高度 px（默认 96）
  }>(),
  { solved: () => [], height: 96 },
)

const levels = useLevelsStore()

// 同源十档色轴（main.css .diff-1..10 的 --dc）
const COLORS = ['#b4793f', '#75868f', '#d99141', '#3358b8', '#237e6b', '#7c3aed', '#ee7e1f', '#e63946', '#d4af37', '#d4af37']

const bars = computed(() => {
  const out: { d: number; name: string; total: number; solved: number }[] = []
  for (let d = 1; d <= 10; d++) {
    const t = props.total[d - 1] ?? 0
    if (t <= 0) continue // 没有题的档不显示
    out.push({
      d,
      total: t,
      solved: Math.min(props.solved[d - 1] ?? 0, t),
      name: levels.cfg?.difficulty_names?.[d - 1] || `D${d}`,
    })
  }
  return out
})

// 柱高：题目最多的档顶满，其余按比例
const maxTotal = computed(() => Math.max(...props.total, 0))
function barPct(total: number): number {
  return maxTotal.value ? Math.max(8, Math.round((total / maxTotal.value) * 100)) : 8
}
// 柱内已完成段占比（底部沉色，余下为 #F2F3F5 未完成）
function solvedPct(total: number, solved: number): number {
  return total ? Math.round((solved / total) * 100) : 0
}
</script>

<template>
  <div class="diff-chart">
    <div
      v-for="b in bars"
      :key="b.d"
      class="dc-col"
      :title="`${b.name}：已完成 ${b.solved} / 共 ${b.total}`"
    >
      <span class="dc-count mono">{{ b.total }}</span>
      <div class="dc-track" :style="{ height: `${height}px` }">
        <div class="dc-bar" :style="{ height: `${barPct(b.total)}%` }">
          <div class="dc-rest" />
          <div
            class="dc-solved"
            :style="{ height: `${solvedPct(b.total, b.solved)}%`, background: COLORS[b.d - 1] }"
          />
        </div>
      </div>
      <span class="dc-name">{{ b.name }}</span>
    </div>
  </div>
</template>

<style scoped>
.diff-chart {
  display: flex;
  align-items: flex-end;
  gap: 14px;
}

.dc-col {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  min-width: 38px;
}

.dc-count {
  font-size: 11.5px;
  color: var(--soj-navy, #0b1f45);
}

.dc-track {
  width: 26px;
  display: flex;
  align-items: flex-end;
}

/* 胶囊柱：上下全圆角，内部按比例分层 */
.dc-bar {
  position: relative;
  width: 100%;
  border-radius: 8px;
  overflow: hidden;
}

/* 未完成：#F2F3F5（拍板色） */
.dc-rest {
  position: absolute;
  inset: 0;
  background: #f2f3f5;
}

/* 已完成：档位色，沉底按比例 */
.dc-solved {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
}

.dc-col:hover .dc-solved {
  opacity: 0.92;
}

.dc-name {
  font-size: 10.5px;
  color: var(--soj-ink-3, #5b6b85);
  white-space: nowrap;
}
</style>

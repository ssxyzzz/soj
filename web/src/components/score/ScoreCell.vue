<script setup lang="ts">
import { computed } from 'vue'

import type { ScoreCellView } from './types'

const props = defineProps<{ cell: ScoreCellView; canInspect: boolean }>()
const emit = defineEmits<{ (e: 'inspect'): void }>()

const errorLabel = computed(() => (props.cell.errors > 99 ? '99+' : String(props.cell.errors)))
const tip = computed(() => {
  const parts = [`得分 ${props.cell.score}`]
  if (props.cell.state === 'untried') parts.push('未做')
  else if (props.cell.errors > 0) parts.push(`未满分 ${props.cell.errors} 次`)
  return parts.join(' · ')
})

function onClick() {
  if (props.canInspect) emit('inspect')
}
</script>

<template>
  <span
    class="score-cell"
    :class="[`is-${cell.state}`, { clickable: canInspect }]"
    :title="tip"
    :aria-label="tip"
    :tabindex="canInspect ? 0 : undefined"
    @click.stop="onClick"
    @keydown.enter.prevent="onClick"
  >
    <span class="score-num">{{ cell.score }}</span>
    <span v-if="cell.errors > 0" class="score-errors">{{ errorLabel }}</span>
  </span>
</template>

<style scoped>
/* v3 .sc：整格铺色（满分绿底 / 部分橙底 / 零分红底 / 未做灰字），圆角 8、mono。
   得分恒居中（有无角标都对齐）；未满分次数 = 绝对定位角标，钉在背景右下圆弧内，
   不参与布局、不撑宽药丸（2026-10-27 拍板：分数对齐优先，角标贴弧度） */
.score-cell {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 36px;
  height: 26px;
  padding: 0 7px;
  border-radius: 8px;
  font-family: var(--font-mono);
  font-size: 12.5px;
  font-weight: var(--soj-fw-bold);
  line-height: 1;
  text-align: center;
  user-select: none;
  font-variant-numeric: tabular-nums;
}

.score-cell.clickable {
  cursor: pointer;
}

.score-cell.clickable:hover {
  filter: brightness(0.96);
}

.is-full {
  background: var(--soj-ac-bg);
  color: var(--soj-ac);
}

.is-partial {
  background: var(--soj-tle-bg);
  color: var(--soj-tle);
}

.is-zero {
  background: var(--soj-wa-bg);
  color: var(--soj-wa);
}

.is-untried {
  color: var(--soj-ink-3);
}

/* 未满分次数：无背景纯数字，钉背景右下圆弧内（right 4px 起避开 8px 圆角），贴底缘 */
.score-errors {
  position: absolute;
  right: 4px;
  bottom: 2px;
  color: var(--soj-wa);
  font-size: 10px;
  font-weight: var(--soj-fw-bold);
  line-height: 1;
  pointer-events: none;
}
</style>

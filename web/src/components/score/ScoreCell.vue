<script setup lang="ts">
import { computed } from 'vue'

import type { ScoreCellView } from './types'

const props = defineProps<{ cell: ScoreCellView; canInspect: boolean; me?: boolean }>()
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
    :class="[`is-${cell.state}`, { clickable: canInspect, me: me }]"
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
/* v3 .sc：整格铺色（满分绿底 / 部分橙底 / 零分红底 / 未做灰字），圆角 8、mono；
   角标（未满分次数）绝对定位在右下角 */
.score-cell {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 36px;
  height: 26px;
  padding: 0 8px;
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

/* 我的行：主蓝描边圈出（v3 .sc.me-cell） */
.score-cell.me {
  outline: 2px solid var(--soj-blue-line);
  outline-offset: 1px;
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

.score-errors {
  position: absolute;
  right: -6px;
  bottom: -4px;
  box-sizing: border-box;
  min-width: 14px;
  height: 14px;
  padding: 0 2px;
  border-radius: 7px;
  background: var(--soj-wa);
  color: #fff;
  font-size: var(--soj-fs-9);
  font-weight: var(--soj-fw-emphasis);
  line-height: 14px;
  text-align: center;
  pointer-events: none;
}
</style>

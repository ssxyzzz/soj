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
/* 只显示数字；角标绝对定位在右下角，超出部分落在 el-table .cell 的 12px 内距里 */
.score-cell {
  position: relative;
  display: inline-block;
  min-width: 24px;
  font-weight: var(--soj-fw-emphasis);
  line-height: 1.4;
  text-align: center;
  user-select: none;
}

.score-cell.clickable {
  cursor: pointer;
}

.score-cell.clickable:hover .score-num {
  text-decoration: underline;
}

.is-full .score-num {
  color: var(--el-color-success);
}

.is-partial .score-num {
  color: var(--el-color-warning);
}

.is-zero .score-num {
  color: var(--el-color-danger);
}

.is-untried .score-num {
  color: var(--el-text-color-secondary);
}

.score-errors {
  position: absolute;
  right: -8px;
  bottom: -3px;
  box-sizing: border-box;
  min-width: 14px;
  height: 14px;
  padding: 0 2px;
  border-radius: 7px;
  background: var(--el-color-danger);
  color: var(--el-color-white);
  font-size: var(--soj-fs-9);
  font-weight: var(--soj-fw-emphasis);
  line-height: 14px;
  text-align: center;
  pointer-events: none;
}
</style>

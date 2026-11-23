<script setup lang="ts">
// 判题中逐点实时进度条（v0.98 全站推送 P3）：已出结果的点按 index 点亮（复用 .dot
// 语义色类），未跑的画虚线占位块；编译期显示「编译中…」。
// 本组件纯展示：拿到的 sub 是 useCasePlayback 的回放展示快照（v0.99.1）——快题的色块
// 由回放按 ≥100ms/点 逐个喂进来（detail 只含已点亮的点、case_done 同步），节奏不在这里。
import { computed } from 'vue'

import { statusStyle } from '@/utils/format'
import type { Submission } from '@/types'

const props = defineProps<{ sub: Submission }>()

const liveCases = computed(() => [...(props.sub.detail ?? [])].sort((a, b) => a.index - b.index))
const liveRemaining = computed(() => {
  const total = props.sub.case_total ?? 0
  const done = liveCases.value.length
  return Array.from({ length: Math.max(0, total - done) }, (_, i) => done + i + 1)
})
</script>

<template>
  <div class="case-live">
    <span class="stage-label">
      {{ sub.stage === 'compile' ? '编译中…' : `判题中 ${sub.case_done ?? 0}/${sub.case_total ?? 0}` }}
    </span>
    <span v-for="c in liveCases" :key="c.index" class="case-chip" :class="statusStyle(c.status).dot">
      {{ statusStyle(c.status).abbr }}
    </span>
    <span v-for="n in liveRemaining" :key="`w${n}`" class="case-chip gray case-wait" :title="`测试点 ${n}`" />
  </div>
</template>

<style scoped>
/* 色块复用 .dot 语义色类（green/red/yellow/blue/gray，main.css 全局工具类），
   占位块虚线空心表示「未跑到」，宽屏换行铺开 */
.case-live {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: var(--soj-sp-10) var(--soj-sp-14);
  background: var(--el-fill-color-lighter);
  border-radius: var(--el-border-radius-base);
}

.case-live .stage-label {
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-secondary);
  margin-right: 4px;
}

.case-chip {
  min-width: 34px;
  height: 22px;
  padding: 0 7px;
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-white);
}

.case-chip.green {
  background: var(--soj-success);
}

.case-chip.red {
  background: var(--soj-danger);
}

.case-chip.yellow {
  background: var(--soj-warning);
}

.case-chip.blue {
  background: var(--el-color-primary);
}

.case-chip.gray {
  background: var(--el-fill-color);
  color: var(--el-text-color-placeholder);
}

.case-chip.case-wait {
  background: transparent;
  border: 1px dashed var(--el-border-color);
}
</style>

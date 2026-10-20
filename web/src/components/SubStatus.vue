<script setup lang="ts">
import { computed } from 'vue'

import { statusStyle } from '@/utils/format'

// 提交状态三件套：小圆灯（.dot）+ 英文简称 + 同色分数（可缺省，如测试点明细）。
// 灯色/简称来自 utils/format 的 statusMap；悬停中文说明沿用各调用方的 status-tip tooltip。
// 排队/评测中（Pending/Judging）不渲染分数——重测入队即清零旧分（v0.7.1 验收反馈：
// 不再出现「Judging + 旧分」），首次提交此期间本就无分。
const props = defineProps<{ status: string; score?: number | null }>()
const meta = computed(() => statusStyle(props.status))
const showScore = computed(
  () => props.status !== 'Pending' && props.status !== 'Judging' && props.score != null,
)
</script>

<template>
  <span class="sub-status" :class="meta.dot">
    <span class="dot" :class="meta.dot" />
    <span class="abbr">{{ meta.abbr }}</span>
    <span v-if="showScore" class="score">{{ score }}</span>
  </span>
</template>

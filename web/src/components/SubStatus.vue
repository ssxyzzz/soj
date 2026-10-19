<script setup lang="ts">
import { computed } from 'vue'

import { statusStyle } from '@/utils/format'

// 提交状态三件套：小圆灯（.dot）+ 英文简称 + 同色分数（可缺省，如测试点明细）。
// 灯色/简称来自 utils/format 的 statusMap；悬停中文说明沿用各调用方的 status-tip tooltip。
const props = defineProps<{ status: string; score?: number | null }>()
const meta = computed(() => statusStyle(props.status))
</script>

<template>
  <span class="sub-status" :class="meta.dot">
    <span class="dot" :class="meta.dot" />
    <span class="abbr">{{ meta.abbr }}</span>
    <span v-if="score != null" class="score">{{ score }}</span>
  </span>
</template>

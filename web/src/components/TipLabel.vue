<script setup lang="ts">
// 标签 + 小问号（悬浮 tooltip 详细说明）：全站问号提示的唯一实现（v0.72 收编）。
// 此前 SwitchRow / ProblemForm / ImportView 各自内联同款 markup，新场景一律用本组件；
// 性能口径：仅用于标签/标题旁（表单帮助、说明收编），禁止进大表格/高频刷新列表行内（v0.62/63）。
import { QuestionFilled } from '@element-plus/icons-vue'

defineProps<{ label?: string; tip?: string }>()
</script>

<template>
  <span class="label-with-tip">
    <template v-if="label">{{ label }}</template>
    <el-tooltip v-if="tip" placement="top" :content="tip">
      <el-icon class="dim"><QuestionFilled /></el-icon>
    </el-tooltip>
  </span>
</template>

<style scoped>
.label-with-tip {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-4);
}
</style>

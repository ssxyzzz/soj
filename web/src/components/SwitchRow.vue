<script setup lang="ts">
// 场景开关行（v0.7.3 统一形态）：标签文字 + 问号小图标（悬浮 tooltip 详细说明）—— 右侧开关。
// 创建比赛 / 发起考试弹窗共用；比赛/考试详情管理卡的三行 scene-row 为同款形态（先行落地）。
import { QuestionFilled } from '@element-plus/icons-vue'

withDefaults(
  defineProps<{
    label: string
    tip?: string
    modelValue: boolean
    disabled?: boolean
  }>(),
  { tip: '', disabled: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()
</script>

<template>
  <div class="switch-row">
    <span class="label-with-tip">
      {{ label }}
      <el-tooltip v-if="tip" placement="top" :content="tip">
        <el-icon class="dim"><QuestionFilled /></el-icon>
      </el-tooltip>
    </span>
    <el-switch
      :model-value="modelValue"
      :disabled="disabled"
      @update:model-value="(v: string | number | boolean) => emit('update:modelValue', !!v)"
    />
  </div>
</template>

<style scoped>
.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.label-with-tip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
</style>

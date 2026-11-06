<script setup lang="ts">
// 场景开关行（v0.7.3 统一形态）：标签文字 + 问号小图标（悬浮 tooltip 详细说明）—— 右侧开关。
// 创建比赛 / 发起考试弹窗共用；比赛/考试详情管理卡的三行 scene-row 为同款形态（先行落地）。
// v0.72 问号部分收编 TipLabel 唯一实现。
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
    <TipLabel v-if="tip" :label="label" :tip="tip" />
    <span v-else>{{ label }}</span>
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
  gap: var(--soj-sp-8);
}
</style>

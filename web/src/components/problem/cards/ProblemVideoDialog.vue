<script setup lang="ts">
// 视频讲解弹窗（v0.90 统一批③收编）：答案解析卡的视频入口，题库/作业做题页与比赛/考试
// 做题页原各一份逐字重复，此处唯一实现。URL 由父层门控（题面 solution_video_url 存在才给入口）。
import AnswerAnalysisCard from '@/components/problem/AnswerAnalysisCard.vue'

defineProps<{ modelValue: boolean; url?: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="视频讲解"
    width="880px"
    top="6vh"
    destroy-on-close
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <AnswerAnalysisCard v-if="url" :url="url" />
  </el-dialog>
</template>

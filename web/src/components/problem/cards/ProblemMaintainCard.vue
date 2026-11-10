<script setup lang="ts">
// 题目维护卡（v0.90 统一批③收编）：编辑题目 + 批量重测，题库/作业做题页与比赛/考试
// 做题页共用——原两份近同款卡 + 两份逐字重复的 rejudgeAll 函数，此处唯一实现。
// 显隐门控（can_edit_statement）由页面 v-if 决定（卡不自拉题目详情）。
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'

import { judgeQueueApi } from '@/api/judgeQueue'
import RailCard from '@/components/RailCard.vue'

const props = defineProps<{ problemId: number }>()

const router = useRouter()
const rejudgeRunning = ref(false)

async function rejudgeAll() {
  if (rejudgeRunning.value) return
  try {
    await ElMessageBox.confirm(
      '将把本题所有已出结果的提交重新评测（低优先级，不影响学生新提交的评测顺序），确定？',
      '批量重测',
      { type: 'warning', confirmButtonText: '批量重测', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  rejudgeRunning.value = true
  try {
    const r = await judgeQueueApi.rejudgeProblem(props.problemId)
    ElMessage.success(r.count > 0 ? `已加入重测队列：${r.count} 条` : '本题没有可重测的提交')
  } finally {
    rejudgeRunning.value = false
  }
}
</script>

<template>
  <RailCard ov="管理" title="题目维护" class="aside-card">
    <div class="entry-actions">
      <el-button type="primary" @click="router.push(`/problems/${problemId}/edit`)">编辑题目</el-button>
      <el-button :loading="rejudgeRunning" @click="rejudgeAll">批量重测本题</el-button>
    </div>
  </RailCard>
</template>

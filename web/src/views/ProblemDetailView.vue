<script setup lang="ts">
// 题目详情（v0.5.4 ② 拆分后主视图）：题面/锁定指引/隐藏开关/右列入口卡 + 提交链路；
// 题解、我的提交、所有代码三个弹窗拆至 components/problem/*（自治拉数据）；编辑题目为独立路由页。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { problemApi, submissionApi } from '@/api'
import { judgeQueueApi } from '@/api/judgeQueue'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import PageShell from '@/components/PageShell.vue'
import ObjectiveAnswer from '@/components/ObjectiveAnswer.vue'
import ObjectiveSolutionCard from '@/components/ObjectiveSolutionCard.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import MySubmissionsDialog from '@/components/problem/MySubmissionsDialog.vue'
import AllCodesDialog from '@/components/problem/AllCodesDialog.vue'
import ProblemInfoCard from '@/components/problem/ProblemInfoCard.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import SolutionsDialog from '@/components/problem/SolutionsDialog.vue'
import { useUserStore } from '@/stores/user'
import { isQueueStatus, statusStyle } from '@/utils/format'
import type { ObjectiveAnswer as ObjAnswer, ProblemDetail } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
let timer: ReturnType<typeof setInterval> | undefined

// 右列：提交 / 在线编程 / 题解 / 我的提交 / 所有代码 五个入口，内容全部进弹窗
const showSubmit = ref(false)
const showWorkbench = ref(false)
const submitting = ref(false)
const showMine = ref(false)
const showOthers = ref(false)
const showSolutions = ref(false)
const mineDialogRef = ref<InstanceType<typeof MySubmissionsDialog>>()

// 赛事隐藏期自动重定向（v0.7.3 优先级模型）：比赛收录的题一律进比赛做题页，
// 其次考试——覆盖赛前/赛中/赛后未发布全程，admin/root 同规则（编辑入口在赛事做题页）。
// 锁信息两个来源：详情 200 响应体（running/特权视角）、隐藏期 404 的 failData。
function roundProblemRoute(p: { locked?: boolean; lock_kind?: string; contest_id?: number; exam_id?: number }): string {
  if (!p?.locked) return ''
  const pid = route.params.id
  return p.lock_kind === 'exam' && p.exam_id
    ? `/exams/${p.exam_id}/problems/${pid}`
    : `/contests/${p.contest_id}/problems/${pid}`
}

const canSetHidden = computed(() => !!problem.value?.can_set_hidden)
const submitDisabled = computed(
  () =>
    !userStore.token ||
    // 客观题无测试数据概念（题干即全部），不受 has_cases 约束
    (problem.value?.type !== 'objective' && problem.value?.has_cases === false),
)

async function load() {
  loading.value = true
  try {
    problem.value = await problemApi.detail(Number(route.params.id))
    const redirect = roundProblemRoute(problem.value)
    if (redirect) {
      router.replace(redirect)
      return
    }
  } catch (err) {
    const lock = (err as Error & { data?: { locked?: boolean } })?.data
    const redirect = lock ? roundProblemRoute(lock) : ''
    router.replace(redirect || '/problems')
  } finally {
    loading.value = false
  }
}

// 批量重测（v0.7.1）：本题全部已出结果的提交低优先级重排队，不挤占学生新提交
const rejudgeAllRunning = ref(false)
async function rejudgeAll() {
  const p = problem.value
  if (!p || rejudgeAllRunning.value) return
  try {
    await ElMessageBox.confirm(
      `将把本题所有已出结果的提交重新评测（低优先级，不影响学生新提交的评测顺序），确定？`,
      '批量重测',
      { type: 'warning', confirmButtonText: '批量重测', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  rejudgeAllRunning.value = true
  try {
    const r = await judgeQueueApi.rejudgeProblem(p.id)
    ElMessage.success(r.count > 0 ? `已加入重测队列：${r.count} 条` : '本题没有可重测的提交')
  } finally {
    rejudgeAllRunning.value = false
  }
}
const submitDisabledReason = computed(() => {
  // 客观题无测试数据概念（题干即全部），不受 has_cases 约束
  if (problem.value?.type !== 'objective' && problem.value?.has_cases === false) {
    return '本题暂无数据，无法评测。请联系管理员上传数据'
  }
  if (!userStore.token) return '请先登录'
  return ''
})

// 题目信息卡（v0.5.5）：统计 + 标签（共享组件）；比赛/考试做题页同款

// 客观题（v0.9）：提交走作答弹窗，在线编程不适用；函数题（v0.9.1）：模板预填 + 语言锁 C++
const isObjective = computed(() => problem.value?.type === 'objective')
const isFunction = computed(() => problem.value?.type === 'function')
const funcTemplate = computed(() => problem.value?.function?.template_code || '')
const isInteractive = computed(() => problem.value?.type === 'interactive')

function openSubmitEntry() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  showSubmit.value = true
}

function openWorkbenchEntry() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  showWorkbench.value = true
}

function openSolutions() {
  if (!problem.value?.can_view_solutions) {
    ElMessage.warning('该题拿到满分后才能查看题解')
    return
  }
  showSolutions.value = true
}

function openMine() {
  showMine.value = true
}

function openOthers() {
  if (!problem.value?.can_view_others) {
    ElMessage.warning('该题拿到满分后可查看他人的代码')
    return
  }
  showOthers.value = true
}

async function handleSubmit(lang: string, code: string) {
  if (!problem.value) return
  submitting.value = true
  try {
    const r = await submissionApi.create(problem.value.id, lang, code)
    ElMessage.success(`已提交 #${r.id}，正在判题`)
    showSubmit.value = false
    showWorkbench.value = false
    mineDialogRef.value?.refresh(1)
    startPolling(r.id)
  } catch {
    // 错误提示已由 axios 拦截器统一弹出
  } finally {
    submitting.value = false
  }
}

// 客观题提交（v0.9）：作答即判，提交后刷新题面（满分后解析/答案随之出现）
async function handleObjectiveSubmit(answers: ObjAnswer[]) {
  if (!problem.value) return
  submitting.value = true
  try {
    const r = await submissionApi.createObjective(problem.value.id, answers)
    ElMessage.success(`已提交 #${r.id}`)
    showSubmit.value = false
    mineDialogRef.value?.refresh(1)
    startPolling(r.id)
    await load()
  } catch {
    // 错误提示已由 axios 拦截器统一弹出
  } finally {
    submitting.value = false
  }
}

function startPolling(id: number) {
  stopPolling()
  timer = setInterval(async () => {
    try {
      const s = await submissionApi.get(id)
      if (!isQueueStatus(s.status)) {
        stopPolling()
        mineDialogRef.value?.refresh()
        const st = statusStyle(s.status)
        ElMessage.success(`#${id} 判题完成：${st.label}${s.score != null ? ` · ${s.score} 分` : ''}`)
      }
    } catch {
      stopPolling()
    }
  }, 1200)
}

function stopPolling() {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

// 单题学生版试卷（v0.10）：独立打印视图，仅编辑者；无答案/解析/后台信息
function openPaper() {
  if (!problem.value) return
  const url = router.resolve(`/problems/${problem.value.id}/paper`).href
  window.open(url, '_blank', 'noopener')
}

// 隐藏开关（仅题面编辑者可见；状态 tag 已删，按钮文案即状态）
async function toggleHidden() {
  if (!problem.value) return
  const target = !problem.value.hidden
  await problemApi.setHidden(problem.value.id, target)
  problem.value.hidden = target
  ElMessage.success(target ? '题目已隐藏，无权限的用户看不到本题' : '已取消隐藏')
}

onMounted(load)

onUnmounted(stopPolling)
</script>

<template>
  <div v-loading="loading">
    <template v-if="problem">
      <PageShell>
        <ProblemStatement :problem="problem">
          <template #extra>
            <el-button
              v-if="canSetHidden"
              size="small"
              :type="problem.hidden ? 'danger' : 'default'"
              @click="toggleHidden"
            >
              {{ problem.hidden ? '已隐藏' : '隐藏' }}
            </el-button>
          </template>
        </ProblemStatement>

        <!-- 客观题（v0.9.3）：作答区在主列题面下方（页面上直接勾选/填写，不弹窗） -->
        <ObjectiveAnswer
          v-if="isObjective && problem.questions?.length"
          :questions="problem.questions"
          :disabled="!!submitDisabledReason || !userStore.token"
          :disabled-reason="userStore.token ? submitDisabledReason : '登录后作答'"
          :submitting="submitting"
          @submit="handleObjectiveSubmit"
        />

        <!-- 参考答案与解析（v0.9.3 独立成卡、置于作答区下方）：questions_full 仅满分∨老师门槛下发 -->
        <ObjectiveSolutionCard
          v-if="isObjective && problem.questions_full?.length"
          :questions="problem.questions_full"
        />

        <template #aside>
        <el-card v-if="problem.can_edit_statement" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="router.push(`/problems/${problem.id}/edit`)">编辑题目</el-button>
            <el-button plain style="width: 100%" :loading="rejudgeAllRunning" @click="rejudgeAll">批量重测本题</el-button>
            <el-button plain style="width: 100%" @click="openPaper">导出试卷</el-button>
          </div>
        </el-card>

        <el-card class="aside-card">
          <div class="entry-actions">
            <el-button
              v-if="!isObjective"
              type="primary"
              plain
              style="width: 100%"
              :disabled="submitDisabled"
              @click="openSubmitEntry"
            >
              提交
            </el-button>
            <el-button
              v-if="!isObjective"
              type="success"
              plain
              style="width: 100%"
              :disabled="submitDisabled"
              @click="openWorkbenchEntry"
            >
              在线编程
            </el-button>
            <el-button type="warning" plain style="width: 100%" @click="openSolutions">题解</el-button>
            <el-button v-if="userStore.token" plain style="width: 100%" @click="openMine">我的提交</el-button>
            <el-button plain style="width: 100%" @click="openOthers">所有代码</el-button>
            <div v-if="submitDisabledReason" class="dim center">{{ submitDisabledReason }}</div>
          </div>
        </el-card>

        <ProblemInfoCard :problem="problem" />
      </template>
    </PageShell>

      <SubmitDialog
      v-if="!isObjective"
      v-model="showSubmit"
      :title="`#${problem.id} ${problem.title}`"
      :submitting="submitting"
      :initial-code="isFunction ? funcTemplate : undefined"
      :lang-only="isFunction ? 'cpp' : isInteractive ? ['cpp', 'c'] : undefined"
      @submit="handleSubmit"
    />


    <CodeWorkbench
      v-model="showWorkbench"
      :title="`#${problem.id} ${problem.title}`"
      :problem="problem"
      :submitting="submitting"
      :initial-code="isFunction ? funcTemplate : undefined"
      :lang-only="isFunction ? 'cpp' : isInteractive ? ['cpp', 'c'] : undefined"
      @submit="handleSubmit"
    />

    <SolutionsDialog v-model="showSolutions" :problem-id="problem.id" />

    <MySubmissionsDialog ref="mineDialogRef" v-model="showMine" :problem-id="problem.id" />

    <AllCodesDialog v-model="showOthers" :problem-id="problem.id" :problem-title="problem.title" />
</template>
  </div>
</template>

<style scoped>
.center {
  text-align: center;
}
</style>

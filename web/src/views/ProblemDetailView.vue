<script setup lang="ts">
// 题目详情（v3-full 重制）：面包屑 + 画布题面（ProblemStatement）+ 宽副列 356 五卡
// （题目维护 → 提交本题（内嵌语言选择 + 深色代码框直接提交）→ 题目信息 → 我的提交
// mini 行卡 → 评测须知）；客观题作答区在主列，弹窗链路（题解/我的提交/所有代码/在线编程）保留。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { assignmentApi, problemApi, submissionApi } from '@/api'
import { judgeQueueApi } from '@/api/judgeQueue'
import { usePolling } from '@/composables/usePolling'
import { useStatementFocus } from '@/composables/useStatementFocus'
import CodeEditor from '@/components/CodeEditor.vue'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import PageCrumbs from '@/components/PageCrumbs.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import AiTutorPanel from '@/components/AiTutorPanel.vue'
import ObjectiveAnswer from '@/components/ObjectiveAnswer.vue'
import ObjectiveSolutionCard from '@/components/ObjectiveSolutionCard.vue'
import SubStatus from '@/components/SubStatus.vue'
import MySubmissionsDialog from '@/components/problem/MySubmissionsDialog.vue'
import AllCodesDialog from '@/components/problem/AllCodesDialog.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import AnswerAnalysisCard from '@/components/problem/AnswerAnalysisCard.vue'
import { defineAsyncComponent } from 'vue'
// 讨论/题解弹窗按需加载（加载优化批）：不点开不下载对应 chunk（题解链含 MdEditor），
// 题面首开闭包瘦身；首次打开多一次懒加载往返
const DiscussDialog = defineAsyncComponent(() => import('@/components/discuss/DiscussDialog.vue'))
const SolutionsDialog = defineAsyncComponent(() => import('@/components/problem/SolutionsDialog.vue'))
import { useSiteStore } from '@/stores/site'
import { useUserStore } from '@/stores/user'
import { useGameFeastStore } from '@/stores/gameFeast'
import { pickTaunt } from '@/utils/gameText'
import { autocompleteOn, defaultLang, languageOptions } from '@/utils/code'
import { fmtRel, fmtShort, isQueueStatus, statusStyle } from '@/utils/format'
import type { AssignmentDetail, ObjectiveAnswer as ObjAnswer, ProblemDetail, Submission } from '@/types'

const route = useRoute()
const router = useRouter()
const site = useSiteStore()
// 题面专注模式（v0.79）：真值时右栏/独立头/面包屑让位，题面铺满
const { focused: stmtFocus } = useStatementFocus()
const userStore = useUserStore()
const feast = useGameFeastStore()

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)

// 右列弹窗：在线编程 / 榜单 / 答案解析（视频+文字题解）/ 我的提交（提交走副列内嵌编辑器）
const showWorkbench = ref(false)
const submitting = ref(false)
const showMine = ref(false)
const showOthers = ref(false)
const showSolutions = ref(false)
const showVideo = ref(false)
const showDiscuss = ref(false)
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

// ---------- 作业做题上下文（v0.77 统一头） ----------
// 路由 assignments/:aid/problems/:id（拍板①新专属路由，与比赛/考试做题页对齐）：
// 拉到作业详情即渲染「面包屑 + 切题」统一头；直链且非班级成员 403 时置空降级普通题页
// （立项边界：不挡做题）。普通题库入口无 aid，全程不走此分支。
const assignmentCtx = ref<AssignmentDetail | null>(null)
const inAssignment = computed(() => route.name === 'assignment-problem')

const assignCrumbItems = computed(() => {
  const a = assignmentCtx.value
  if (!a) return []
  const items: { label: string; to?: string }[] = [{ label: a.class_name || '班级', to: `/classes/${a.class_id}` }]
  items.push({ label: a.title, to: `/assignments/${a.id}` })
  if (problem.value) items.push({ label: problem.value.title })
  return items
})

async function loadAssignmentCtx() {
  assignmentCtx.value = null
  if (!inAssignment.value) return
  try {
    assignmentCtx.value = await assignmentApi.detail(Number(route.params.aid))
  } catch {
    assignmentCtx.value = null // 403/不存在：降级普通题页，不挡做题
  }
}

function goAssignProblem(pid: number) {
  if (pid === Number(route.params.id)) return
  router.push(`/assignments/${route.params.aid}/problems/${pid}`)
}

const canSetHidden = computed(() => !!problem.value?.can_set_hidden)
const canSetDuplicate = computed(() => !!problem.value?.can_set_duplicate)
const submitDisabled = computed(
  () =>
    !userStore.token ||
    // 客观题无测试数据概念（题干即全部），不受 has_cases 约束
    (problem.value?.type !== 'objective' && problem.value?.has_cases === false),
)

async function load() {
  loading.value = true
  loadAssignmentCtx() // 并行拉作业上下文（自带 try/catch，失败降级不阻塞做题）
  try {
    const pid = Number(route.params.id)
    const p = await problemApi.detail(pid)
    // 快速切题竞态守卫：慢响应落地时路由已指向新题则丢弃（旧响应晚于新题赋值会整页回退旧题）
    if (pid !== Number(route.params.id)) return
    problem.value = p
    const redirect = roundProblemRoute(p)
    if (redirect) {
      router.replace(redirect)
      return
    }
    // 切题即刷新副列「我的提交」mini 行卡（原先只在挂载/提交后拉，同实例切题时停留旧题数据）
    loadRecentMine()
  } catch (err) {
    const lock = (err as Error & { data?: { locked?: boolean } })?.data
    const redirect = lock ? roundProblemRoute(lock) : ''
    router.replace(redirect || '/problems')
  } finally {
    loading.value = false
  }
}

// 切题导航同组件实例复用（/assignments/:aid/problems/:id 仅 :id 段变化）：三段任一
// 变化都要重载（曾只比 route.name——切题前后路由名相同导致不重载，URL 变题目不换）
watch(
  () => [route.name, route.params.id, route.params.aid] as const,
  ([nName, nId, nAid], [oName, oId, oAid]) => {
    if (nName !== oName || nId !== oId || nAid !== oAid) load()
  },
)

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

// 玉解锁题解（v0.33-②）：登录 ∧ 不可免费看 ∧ 未解锁 → 显示「N 玉解锁」按钮
const canUnlockSolutions = computed(
  () =>
    !!userStore.token &&
    !!problem.value &&
    !problem.value.can_view_solutions &&
    !problem.value.solution_unlocked &&
    (problem.value.solution_unlock_price ?? 0) > 0,
)
const unlocking = ref(false)
// AI 助教是否启用（AiTutorPanel 拉状态后回报；决定「答案解析」卡是否为 AI 单独出现）
const aiEnabled = ref(false)
async function unlockSolutions() {
  const p = problem.value
  if (!p) return
  try {
    await ElMessageBox.confirm(
      `花 ${p.solution_unlock_price} 玉解锁本题全部文字题解？解锁即时生效、永久可见（首次通过后本就免费）。`,
      '解锁题解',
      { type: 'warning', confirmButtonText: `解锁（${p.solution_unlock_price} 玉）`, cancelButtonText: '再想想' },
    )
  } catch {
    return
  }
  unlocking.value = true
  try {
    const r = await problemApi.unlockSolutions(p.id)
    userStore.setJade(r.balance, userStore.jadeCurrency || '玉') // 顶栏余额即时刷新
    ElMessage.success('题解已解锁')
    await load() // 重拉详情：can_view_solutions 翻真
  } catch {
    // 拦截器统一提示（余额不足等）
  } finally {
    unlocking.value = false
  }
}

// 客观题（v0.9）：提交走主列作答区，在线编程不适用；函数题（v0.9.1）：模板预填 + 语言锁 C++
const isObjective = computed(() => problem.value?.type === 'objective')
const isFunction = computed(() => problem.value?.type === 'function')
const funcTemplate = computed(() => problem.value?.function?.template_code || '')
const isInteractive = computed(() => problem.value?.type === 'interactive')

// 挑战模式（v0.23）：题目资格（stdio 传统题）+ 有数据 + 非锁题；入口走独立全屏页
// 等级权益闸（v0.40）：Lv.3 解锁挑战（教职工豁免，userStore.canChallenge）
const challengeReady = computed(
  () =>
    !!problem.value?.challenge?.eligible &&
    problem.value?.has_cases !== false &&
    !problem.value?.locked &&
    userStore.canChallenge,
)

function openChallenge() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  if (!challengeReady.value) {
    if (userStore.token && !userStore.canChallenge) {
      ElMessage.warning('Lv.3 解锁挑战模式——先去做题攒经验升级吧')
      return
    }
    ElMessage.warning(
      problem.value?.locked
        ? '该题处于比赛/考试锁定期，暂不可挑战'
        : '本题暂不支持挑战（仅限标准输入输出的传统题）',
    )
    return
  }
  router.push(`/problems/${problem.value!.id}/challenge`)
}

// ---- 副列「提交本题」内嵌编辑器（v3 rail codebox）：语言 + 深色代码框 + 直接提交 ----
const railLang = ref(defaultLang())
const railCode = ref('')
const acOn = computed(() => autocompleteOn())

watch(problem, (p) => {
  if (!p) return
  const lock = isFunction.value ? 'cpp' : isInteractive.value ? 'cpp' : undefined
  if (lock) railLang.value = lock
  // 仅函数题预填题面学生模板；个人代码模板只用于在线编程（2026-10-27），提交弹窗保持全空白
  railCode.value = funcTemplate.value
})

function changeRailLang(l: string) {
  railLang.value = l
}

const railLangOptions = computed(() => {
  if (isFunction.value) return languageOptions.filter((o) => o.value === 'cpp')
  if (isInteractive.value) return languageOptions.filter((o) => ['cpp', 'c'].includes(o.value))
  return languageOptions
})

function railSubmit() {
  if (submitDisabled.value) {
    ElMessage.warning(submitDisabledReason.value || '请先登录')
    if (!userStore.token) router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  if (!railCode.value.trim()) {
    ElMessage.warning('代码不能为空')
    return
  }
  handleSubmit(railLang.value, railCode.value)
}

function openWorkbenchEntry() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  showWorkbench.value = true
}

// 粘贴提交弹窗（2026-10-25 修复⑥）：副列去内嵌编辑器后的入口。
// 打开弹窗时仅在未编辑过（空白 / 仍是函数题模板）的情况下回填函数题模板；
// 个人代码模板只用于在线编程（2026-10-27），此处不再填入。
const showRailSubmit = ref(false)

const railUntouched = computed(() => {
  const code = railCode.value
  return !code.trim() || (!!funcTemplate.value && code === funcTemplate.value)
})

function openRailSubmit() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  if (railUntouched.value) railCode.value = funcTemplate.value
  showRailSubmit.value = true
}

function openSolutions() {
  // 入口在答案解析卡内，卡片本身已按满分∨老师门槛显隐，这里直接开
  showSolutions.value = true
}

function openOthers() {
  // 榜单弹窗（v0.23 Tab 化）：挑战榜登录即可见，用时/内存榜 AC 门槛在弹窗内提示
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
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
    showWorkbench.value = false
    showRailSubmit.value = false
    mineDialogRef.value?.refresh(1)
    loadRecentMine()
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
    mineDialogRef.value?.refresh(1)
    startPolling(r.id)
    await load() // 行卡随 load() 内的 loadRecentMine 一并刷新
  } catch {
    // 错误提示已由 axios 拦截器统一弹出
  } finally {
    submitting.value = false
  }
}

// ---- 副列「我的提交」mini 行卡：近 3 条 + 查看全部 ----
const recentMine = ref<Submission[]>([])

async function loadRecentMine() {
  if (!problem.value || !userStore.token) return
  const pid = problem.value.id
  try {
    const r = await problemApi.mySubmissions(pid, 1, 3)
    // 快速切题竞态守卫：响应落地时已换题则丢弃，旧题提交不残留在新题行卡
    if (problem.value?.id !== pid) return
    recentMine.value = r.list.slice(0, 3)
  } catch {
    if (problem.value?.id === pid) recentMine.value = []
  }
}

// 判题结果轮询（v0.37 迁 usePolling）：后台标签页自动暂停（原手写 setInterval 在
// 切走标签后继续空转）、5 分钟上限——判题队列僵死时不再无限轮询，到限提示后由
// 学生手动刷新兜底。拉取失败即停（拦截器已提示），终态走完成提示与副列刷新。
let pollingSid = 0
const judgePoll = usePolling(
  async () => {
    const id = pollingSid
    if (!id) return false
    try {
      const s = await submissionApi.get(id)
      if (!isQueueStatus(s.status)) {
        pollingSid = 0
        mineDialogRef.value?.refresh()
        loadRecentMine()
        const st = statusStyle(s.status)
        // 游戏感批（v0.41）：AC 开箱（首 AC 带发玉数据）与 WA 吐槽
        if (s.status === 'Accepted' && s.game && (s.game.jade_total || s.game.combo_cur)) {
          feast.feast(s.game, s.score ?? 0, problem.value?.title ?? '')
        } else if (s.status === 'Wrong Answer') {
          ElMessage({ message: pickTaunt(), type: 'warning', duration: 2600, grouping: true })
        }
        ElMessage.success(`#${id} 判题完成：${st.label}${s.score != null ? ` · ${s.score} 分` : ''}`)
        return false
      }
      return true
    } catch {
      pollingSid = 0
      return false
    }
  },
  1200,
  {
    maxDurationMs: 5 * 60_000,
    onExpire: () => {
      const id = pollingSid
      pollingSid = 0
      ElMessage.warning(`#${id} 判题等待超时（5 分钟）——请稍后刷新查看结果；持续异常请联系老师`)
    },
  },
)

function startPolling(id: number) {
  pollingSid = id
  judgePoll.restart()
}

function stopPolling() {
  pollingSid = 0
  judgePoll.stop()
}

// 隐藏开关（仅题面编辑者可见；状态 tag 已删，按钮文案即状态）
async function toggleHidden() {
  if (!problem.value) return
  const target = !problem.value.hidden
  await problemApi.setHidden(problem.value.id, target)
  problem.value.hidden = target
  ElMessage.success(target ? '题目已隐藏，无权限的用户看不到本题' : '已取消隐藏')
}

// 重复标记开关（v0.47，仅题面编辑者可见；设置后按钮置灰，学生端立即不可见）
async function toggleDuplicate() {
  if (!problem.value) return
  const target = !problem.value.duplicate
  await problemApi.setDuplicate(problem.value.id, target)
  problem.value.duplicate = target
  ElMessage.success(target ? '已标记重复题，无权限的用户看不到本题' : '已取消重复标记')
}

onMounted(async () => {
  await load()
})

onUnmounted(stopPolling)
</script>

<template>
  <div v-loading="loading">
    <template v-if="problem">
      <PageShell :hide-aside="stmtFocus">
        <!-- v0.77 作业做题统一头（独立行）：面包屑（班级/作业可点）+ 切题序号，与比赛/
             考试做题页同形态；拉不到作业上下文（直链且非班级成员 403）不渲染，降级普通题页 -->
        <el-card
          v-if="assignmentCtx && assignmentCtx.id && !stmtFocus"
          class="assign-head"
          :body-style="{ padding: 'var(--soj-sp-14) var(--soj-sp-20)' }"
        >
          <PageCrumbs :items="assignCrumbItems" />
          <div class="sw-row">
            <span class="sw-lab">切题</span>
            <button
              v-for="(p, i) in assignmentCtx.problems"
              :key="p.id"
              type="button"
              class="sw-btn"
              :class="{ on: p.id === Number(route.params.id) }"
              @click="goAssignProblem(p.id)"
            >
              {{ i + 1 }}
            </button>
          </div>
        </el-card>
        <div v-else-if="!stmtFocus" class="crumbs">
          <router-link to="/problems">题库</router-link>
          ／ <span class="mono">P{{ problem.id }}</span>
        </div>

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
            <el-button
              v-if="canSetDuplicate"
              size="small"
              :type="problem.duplicate ? 'info' : 'default'"
              @click="toggleDuplicate"
            >
              {{ problem.duplicate ? '已标重复' : '标重复' }}
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
        <RailCard v-if="problem.can_edit_statement" ov="管理" title="题目维护" class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" @click="router.push(`/problems/${problem.id}/edit`)">编辑题目</el-button>
            <el-button :loading="rejudgeAllRunning" @click="rejudgeAll">批量重测本题</el-button>
          </div>
        </RailCard>

        <!-- v3「提交本题」（2026-10-25 修复⑥）：副列不再内嵌编辑器，点「提交」进弹窗粘贴提交；
             客观题换成操作入口卡。按钮竖排全宽五连（2026-10-27 拍板：对齐比赛做题页本题操作卡，
             弃「我的提交/所有代码」两列行与 small 档） -->
        <RailCard ov="评测" :title="isObjective ? '本题操作' : '提交本题'" class="aside-card">
          <template v-if="!isObjective">
            <div class="entry-actions">
              <el-button type="primary" :disabled="submitDisabled" @click="openRailSubmit">提交</el-button>
              <el-button v-if="userStore.canOnline" :disabled="submitDisabled" @click="openWorkbenchEntry">在线编程</el-button>
              <el-button
                v-if="userStore.canChallenge"
                class="btn-challenge"
                :class="{ off: !challengeReady }"
                @click="openChallenge"
              >
                ⚡ 挑战模式
              </el-button>
              <el-button @click="openOthers">榜单</el-button>
            </div>
            <div v-if="submitDisabledReason" class="hint">{{ submitDisabledReason }}</div>
          </template>
          <div v-else class="entry-actions">
            <div v-if="!userStore.token" class="hint center">登录后作答</div>
          </div>
        </RailCard>

        <RailCard v-if="userStore.token" ov="记录" title="我的提交" class="aside-card">
          <template v-if="recentMine.length">
            <div v-for="s in recentMine" :key="s.id" class="soj-mini-li" @click="showMine = true">
              <SubStatus :status="s.status" :score="s.score" />
              <span class="grow mono">#{{ s.id }}</span>
              <!-- 相对时间列（2026-10-25 用户反馈）：N 秒/分钟/小时前；含中文不挂 mono（修复⑫） -->
              <span class="t">{{ fmtRel(s.created_at) }}</span>
              <span class="t mono">{{ s.time_ms ? `${s.time_ms}ms` : fmtShort(s.created_at).split(' ')[1] }}</span>
            </div>
            <p class="hint link" @click="showMine = true">查看全部 →</p>
          </template>
          <p v-else class="hint">还没有提交过本题。</p>
        </RailCard>

        <!-- 答案解析（v0.23 验收调整）：视频讲解默认可看（不设 AC 门槛），文字题解满分∨老师门槛；
             玉解锁（v0.33-②）：未达门槛的学生可花玉解锁文字题解（难度 × 系数定价，解锁永久）。
             四按钮方案（2026-10-30 草案）：AI 启用时卡标题让位——小玉组带自有头行，
             解析组以「答案解析」小节标题分组；四钮统一大胶囊 -->
        <RailCard
          v-if="!problem.locked && (problem.solution_video_url || problem.can_view_solutions || canUnlockSolutions || aiEnabled)"
          ov="解析"
          :class="['aside-card', { 'ai-headless': aiEnabled }]"
        >
          <template #head>
            <div class="rail-ov">解析</div>
            <div v-if="!aiEnabled" class="rail-t">
              <TipLabel
                label="答案解析"
                tip="文字题解满分（或老师）可见；未达标可花玉解锁，首次通过后免费，解锁即时生效、永久可见"
              />
            </div>
          </template>
          <AiTutorPanel v-if="userStore.token" :pid="problem.id" class="ai-in-solution" @enabled="aiEnabled = $event" />
          <template v-if="problem.solution_video_url || problem.can_view_solutions || canUnlockSolutions">
            <div v-if="aiEnabled" class="sol-sec-h">答案解析</div>
            <div class="entry-actions pill-btns">
              <el-button v-if="problem.solution_video_url" @click="showVideo = true">视频讲解</el-button>
              <el-button v-if="problem.can_view_solutions" @click="openSolutions">文字题解</el-button>
              <el-button v-if="canUnlockSolutions" type="primary" plain :loading="unlocking" @click="unlockSolutions">
                花 {{ problem.solution_unlock_price }} 玉解锁文字题解
              </el-button>
            </div>
          </template>
        </RailCard>

        <!-- 讨论入口（v0.24）：题内思路讨论弹窗；赛时整题锁定（服务端闸门，弹窗内提示） -->
        <RailCard v-if="!problem.locked && site.discussProblemEnabled" ov="互助" class="aside-card">
          <template #head>
            <div class="rail-ov">互助</div>
            <div class="rail-t"><TipLabel label="本题讨论" tip="交流思路、求助与纠错；代码块按讨论门槛展示" /></div>
          </template>
          <div class="entry-actions">
            <el-button type="primary" plain @click="showDiscuss = true">讨论本题</el-button>
          </div>
        </RailCard>

        <RailCard ov="说明" title="评测须知" class="aside-card">
          <p class="hint">
            代码在沙箱中编译运行；判分方式以题面为准（默认全文比对，部分题采用宽容比对 / 校验器 / 客观题判分）；提交间隔受全站限速约束，请勿读写约定之外的文件。
          </p>
        </RailCard>
      </template>
    </PageShell>

    <CodeWorkbench
      v-model="showWorkbench"
      :title="`#${problem.id} ${problem.title}`"
      :problem="problem"
      :submitting="submitting"
      :initial-code="isFunction ? funcTemplate : railCode || undefined"
      :lang-only="isFunction ? 'cpp' : isInteractive ? ['cpp', 'c'] : undefined"
      @submit="handleSubmit"
    />

    <SolutionsDialog v-if="showSolutions" v-model="showSolutions" :problem-id="problem.id" />

    <!-- 题内讨论弹窗（v0.24；v-if 配合异步组件实现真延迟） -->
    <DiscussDialog
      v-if="showDiscuss"
      v-model="showDiscuss"
      scope="problem"
      :scope-id="problem.id"
      :title="`P${problem.id} ${problem.title} · 讨论`"
    />

    <!-- 视频讲解弹窗（答案解析卡入口） -->
    <el-dialog v-model="showVideo" title="视频讲解" width="880px" top="6vh" destroy-on-close>
      <AnswerAnalysisCard v-if="problem.solution_video_url" :url="problem.solution_video_url" />
    </el-dialog>

    <MySubmissionsDialog ref="mineDialogRef" v-model="showMine" :problem-id="problem.id" />

    <AllCodesDialog v-model="showOthers" :problem-id="problem.id" :problem-title="problem.title" />

    <!-- 粘贴提交弹窗（2026-10-25 修复⑥）：副列不再内嵌编辑器，点「提交代码」进弹窗粘贴提交；
         2026-10-26 字号/字体跟随用户偏好（与在线编程同一份 prefs 实时联动），弹窗加大 -->
    <el-dialog v-model="showRailSubmit" title="提交代码" width="960px" top="6vh" :close-on-click-modal="false">
      <div class="dlg-lang">
        <span class="dlg-label">语言</span>
        <el-select :model-value="railLang" style="width: 220px" @change="changeRailLang">
          <el-option v-for="o in railLangOptions" :key="o.value" :value="o.value" :label="o.label" />
        </el-select>
      </div>
      <CodeEditor
        v-model="railCode"
        class="dlg-editor"
        :language="railLang"
        :autocomplete="acOn"
        :font-size="userStore.prefs.codeFontSize || 14"
        :font-family="userStore.prefs.codeFont || 'Consolas'"
        height="520px"
        placeholder="在此编写或粘贴你的代码"
      />
      <div v-if="submitDisabledReason" class="hint">{{ submitDisabledReason }}</div>
      <template #footer>
        <el-button @click="showRailSubmit = false">取消</el-button>
        <el-button type="primary" :disabled="submitDisabled" :loading="submitting" @click="railSubmit">
          提交评测
        </el-button>
      </template>
    </el-dialog>
</template>
  </div>
</template>

<style scoped>
/* v0.77 作业做题统一头（与比赛/考试做题页同形态；sw-btn 样式照搬 RoundProblemView） */
.assign-head :deep(.soj-crumbs) {
  margin-bottom: var(--soj-sp-10);
}

.sw-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-6);
  flex-wrap: wrap;
}

.sw-lab {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  margin-right: var(--soj-sp-2);
}

.sw-btn {
  width: 34px;
  height: 32px;
  border-radius: 10px;
  border: 1px solid var(--soj-line-2);
  background: var(--el-fill-color-blank);
  font: inherit;
  font-weight: var(--soj-fw-bold);
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  cursor: pointer;
  transition: all 0.12s;
}

.sw-btn:hover {
  border-color: var(--soj-blue-line);
  color: var(--soj-blue);
}

.sw-btn.on {
  background: var(--soj-blue);
  border-color: var(--soj-blue);
  color: var(--soj-white);
}

/* v3 .crumbs */
.crumbs {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  margin-bottom: var(--soj-sp-14);
}

.crumbs a {
  color: var(--soj-ink-3);
  text-decoration: none;
}

.crumbs a:hover {
  color: var(--soj-blue);
}

/* 粘贴提交弹窗 */
.dlg-lang {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
}

.dlg-label {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

.dlg-editor {
  margin-top: var(--soj-sp-12);
}

.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: var(--soj-sp-10) 0 0;
}

.hint.link {
  cursor: pointer;
  color: var(--soj-blue);
  font-weight: var(--soj-fw-emphasis);
}

/* 挑战模式入口（v0.23）：暖橙渐变在评测卡五连中一眼可辨；无资格态降灰 */
.btn-challenge {
  --el-button-text-color: var(--soj-warning-deep);
  --el-button-border-color: color-mix(in srgb, var(--soj-warning) 45%, var(--soj-white));
  --el-button-bg-color: color-mix(in srgb, var(--soj-warning) 12%, var(--soj-white));
  --el-button-hover-text-color: var(--soj-warning-deep);
  --el-button-hover-border-color: var(--soj-warning);
  --el-button-hover-bg-color: color-mix(in srgb, var(--soj-warning) 22%, var(--soj-white));
  font-weight: var(--soj-fw-emphasis);
}
.btn-challenge.off {
  --el-button-text-color: var(--soj-ink-3);
  --el-button-border-color: var(--soj-line);
  --el-button-bg-color: var(--soj-surface-2);
  --el-button-hover-text-color: var(--soj-ink-3);
  --el-button-hover-border-color: var(--soj-line);
  --el-button-hover-bg-color: var(--soj-surface-2);
  filter: grayscale(1);
  cursor: not-allowed;
}

.center {
  text-align: center;
  margin-top: var(--soj-sp-8);
}

.soj-mini-li {
  cursor: pointer;
}

/* AI 助教并入答案解析卡（v0.33-④ 验收调整）：面板与解析入口之间留间距 */
.ai-in-solution {
  margin-bottom: var(--soj-sp-12);
}

/* AI 让位卡标题时头部只剩 kicker：不再画卡头横线（线由小玉头行承担，2026-10-30 反馈） */
.aside-card.ai-headless :deep(.el-card__header) {
  border-bottom: none;
}

/* 答案解析小节标题（2026-10-30 草案）：横线画法对齐卡头——贴卡缘、EP 卡头同色；
   线紧贴标题（对齐卡头标题距线间距，2026-10-30 反馈压高度） */
.sol-sec-h {
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  margin: 0 -20px var(--soj-sp-12);
  padding: 0 var(--soj-sp-20) var(--soj-sp-4);
  border-bottom: 1px solid var(--el-card-border-color, var(--el-border-color-light));
}

/* 解析组胶囊钮（2026-10-30 草案）：整宽大胶囊，与上方小玉两钮同族 */
.entry-actions.pill-btns {
  flex-direction: column;
  align-items: stretch;
  gap: var(--soj-sp-12);
}

.entry-actions.pill-btns .el-button {
  width: 100%;
  height: 46px;
  margin: 0;
  border-radius: 999px;
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  --el-button-hover-text-color: var(--soj-navy);
  --el-button-hover-bg-color: var(--el-fill-color-blank);
  --el-button-hover-border-color: var(--soj-blue-line);
}
</style>

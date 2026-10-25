<script setup lang="ts">
// 题目详情（v3-full 重制）：面包屑 + 画布题面（ProblemStatement）+ 宽副列 356 五卡
// （题目维护 → 提交本题（内嵌语言选择 + 深色代码框直接提交）→ 题目信息 → 我的提交
// mini 行卡 → 评测须知）；客观题作答区在主列，弹窗链路（题解/我的提交/所有代码/在线编程）保留。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { problemApi, submissionApi } from '@/api'
import { judgeQueueApi } from '@/api/judgeQueue'
import CodeEditor from '@/components/CodeEditor.vue'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import ObjectiveAnswer from '@/components/ObjectiveAnswer.vue'
import ObjectiveSolutionCard from '@/components/ObjectiveSolutionCard.vue'
import SubStatus from '@/components/SubStatus.vue'
import MySubmissionsDialog from '@/components/problem/MySubmissionsDialog.vue'
import AllCodesDialog from '@/components/problem/AllCodesDialog.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import SolutionsDialog from '@/components/problem/SolutionsDialog.vue'
import { useUserStore } from '@/stores/user'
import { autocompleteOn, defaultLang, languageOptions, templateFor } from '@/utils/code'
import { fmtRel, fmtShort, isQueueStatus, statusStyle } from '@/utils/format'
import type { ObjectiveAnswer as ObjAnswer, ProblemDetail, Submission } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
let timer: ReturnType<typeof setInterval> | undefined

// 右列弹窗：在线编程 / 题解 / 我的提交 / 所有代码（提交走副列内嵌编辑器）
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

// 客观题（v0.9）：提交走主列作答区，在线编程不适用；函数题（v0.9.1）：模板预填 + 语言锁 C++
const isObjective = computed(() => problem.value?.type === 'objective')
const isFunction = computed(() => problem.value?.type === 'function')
const funcTemplate = computed(() => problem.value?.function?.template_code || '')
const isInteractive = computed(() => problem.value?.type === 'interactive')

// ---- 副列「提交本题」内嵌编辑器（v3 rail codebox）：语言 + 深色代码框 + 直接提交 ----
const railLang = ref(defaultLang())
const railCode = ref('')
const acOn = computed(() => autocompleteOn())

watch(problem, (p) => {
  if (!p) return
  const lock = isFunction.value ? 'cpp' : isInteractive.value ? 'cpp' : undefined
  if (lock) railLang.value = lock
  railCode.value = funcTemplate.value || templateFor(railLang.value)
})

function changeRailLang(l: string) {
  // 仅在代码还是初始模板（未编辑）时跟随切换，避免覆盖已写的代码
  if (!railCode.value.trim() || railCode.value === templateFor(railLang.value)) railCode.value = templateFor(l)
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

// 粘贴提交弹窗（2026-10-25 修复⑥）：副列去内嵌编辑器后的入口
const showRailSubmit = ref(false)

function openRailSubmit() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  showRailSubmit.value = true
}

function openMine() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  showMine.value = true
}

function openSolutions() {
  if (!problem.value?.can_view_solutions) {
    ElMessage.warning('该题拿到满分后才能查看题解')
    return
  }
  showSolutions.value = true
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
    loadRecentMine()
    startPolling(r.id)
    await load()
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
  try {
    const r = await problemApi.mySubmissions(problem.value.id, 1, 3)
    recentMine.value = r.list.slice(0, 3)
  } catch {
    recentMine.value = []
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
        loadRecentMine()
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

// 隐藏开关（仅题面编辑者可见；状态 tag 已删，按钮文案即状态）
async function toggleHidden() {
  if (!problem.value) return
  const target = !problem.value.hidden
  await problemApi.setHidden(problem.value.id, target)
  problem.value.hidden = target
  ElMessage.success(target ? '题目已隐藏，无权限的用户看不到本题' : '已取消隐藏')
}

onMounted(async () => {
  await load()
  loadRecentMine()
})

onUnmounted(stopPolling)
</script>

<template>
  <div v-loading="loading">
    <template v-if="problem">
      <PageShell rail-wide>
        <!-- v3 面包屑 -->
        <div class="crumbs">
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

        <!-- v3「提交本题」（2026-10-25 修复⑥）：副列不再内嵌编辑器，点「提交代码」进弹窗粘贴提交；
             客观题换成操作入口卡 -->
        <RailCard ov="评测" :title="isObjective ? '本题操作' : '提交本题'" class="aside-card">
          <template v-if="!isObjective">
            <div class="entry-actions">
              <el-button type="primary" :disabled="submitDisabled" @click="openRailSubmit">提交代码</el-button>
              <!-- 次级 CTA：primary plain 浅蓝款，与主 CTA 拉开层次（2026-10-25 用户反馈加色） -->
              <el-button type="primary" plain :disabled="submitDisabled" @click="openWorkbenchEntry">在线编程</el-button>
            </div>
            <div v-if="submitDisabledReason" class="hint">{{ submitDisabledReason }}</div>
            <div class="entry-extras">
              <el-button size="small" @click="openSolutions">题解</el-button>
              <div class="entry-row">
                <el-button size="small" @click="openMine">我的提交</el-button>
                <el-button size="small" @click="openOthers">所有代码</el-button>
              </div>
            </div>
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

        <RailCard ov="说明" title="评测须知" class="aside-card">
          <p class="hint">
            代码在 go-judge 沙箱中编译运行，全文比对输出；提交间隔受全站限速约束。请勿读写约定之外的文件。
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

    <SolutionsDialog v-model="showSolutions" :problem-id="problem.id" />

    <MySubmissionsDialog ref="mineDialogRef" v-model="showMine" :problem-id="problem.id" />

    <AllCodesDialog v-model="showOthers" :problem-id="problem.id" :problem-title="problem.title" />

    <!-- 粘贴提交弹窗（2026-10-25 修复⑥）：副列不再内嵌编辑器，点「提交代码」进弹窗粘贴提交 -->
    <el-dialog v-model="showRailSubmit" title="提交代码" width="720px" :close-on-click-modal="false">
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
        height="380px"
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
/* v3 .crumbs */
.crumbs {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  margin-bottom: 14px;
}

.crumbs a {
  color: var(--soj-ink-3);
  text-decoration: none;
}

.crumbs a:hover {
  color: var(--soj-blue);
}

/* 次要入口区（题解小按钮独占一行 + 我的提交/所有代码两列，2026-10-25 修复⑥）；
   字号 15px 与上方按钮一致——12px 压缩与 small 档自带 13px 都被否（2026-10-25 二次回调） */
.entry-extras {
  margin-top: 8px;
  display: grid;
  gap: 8px;
}

.entry-extras .el-button {
  width: 100%;
  margin-left: 0;
  font-size: 15px;
}

.entry-extras .entry-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

/* 粘贴提交弹窗 */
.dlg-lang {
  display: flex;
  align-items: center;
  gap: 10px;
}

.dlg-label {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

.dlg-editor {
  margin-top: 12px;
}

/* 次要管理动作两列行：压掉 EP 的 el-button 联动 margin，间距走 grid gap */
.entry-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  width: 100%;
}

.entry-row .el-button + .el-button {
  margin-left: 0;
}

.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: 10px 0 0;
}

.hint.link {
  cursor: pointer;
  color: var(--soj-blue);
  font-weight: var(--soj-fw-emphasis);
}

.center {
  text-align: center;
  margin-top: 8px;
}

.soj-mini-li {
  cursor: pointer;
}
</style>

<script setup lang="ts">
// 比赛/考试做题页共享组件（v0.5.4 ① 合并，原 ContestProblemView/ExamProblemView 94→97% 同构）。
// 差异仅三处：文案（比赛/考试）、详情页路由前缀（/contests|/exams）、加载失败回退
// （比赛回赛事页、考试回班级列表）——全部由 kind 派生。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { contestApi, examApi, problemApi, submissionApi, type RoundKind } from '@/api'
import { judgeQueueApi } from '@/api/judgeQueue'
import { usePolling } from '@/composables/usePolling'
import { useStatementFocus } from '@/composables/useStatementFocus'
import CodePane from '@/components/CodePane.vue'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import PageCrumbs from '@/components/PageCrumbs.vue'
import SubStatus from '@/components/SubStatus.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import AllCodesDialog from '@/components/problem/AllCodesDialog.vue'
import AnswerAnalysisCard from '@/components/problem/AnswerAnalysisCard.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import SolutionsDialog from '@/components/problem/SolutionsDialog.vue'
import ObjectiveAnswer from '@/components/ObjectiveAnswer.vue'
import ObjectiveSolutionCard from '@/components/ObjectiveSolutionCard.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import { fmtDate, fmtMem, fmtRel, fmtSec, isQueueStatus } from '@/utils/format'
import { useUserStore } from '@/stores/user'
import type {
  ContestDetail, ContestMySubmission, ObjectiveAnswer as ObjAnswer, ProblemDetail, Submission,
} from '@/types'

const props = defineProps<{ kind: RoundKind }>()

const route = useRoute()
const router = useRouter()
// 题面专注模式（v0.79）：真值时独立头/右栏让位
const { focused: stmtFocus } = useStatementFocus()
const userStore = useUserStore()

const api = computed(() => (props.kind === 'exam' ? examApi : contestApi))
const name = computed(() => (props.kind === 'exam' ? '考试' : '比赛'))
const roundBase = computed(() => `/${props.kind}s`)
// 统一头面包屑段（v0.77）：考试两段（班级可点回班级）/比赛一段（不挂班级）；末段=
// 当前题目名；「返回xx」按钮随旧头退役（拍板②）
const crumbItems = computed(() => {
  const items: { label: string; to?: string }[] = []
  if (props.kind === 'exam' && round.value?.class_id && round.value.class_name) {
    items.push({ label: round.value.class_name, to: `/classes/${round.value.class_id}` })
  }
  if (round.value) items.push({ label: round.value.title, to: `${roundBase.value}/${round.value.id}` })
  if (problem.value) items.push({ label: problem.value.title })
  return items
})

const round = ref<ContestDetail | null>(null)
const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
const mine = ref<ContestMySubmission[]>([])
// 点开的提交：隐藏期 = 列表行（自带 code）；已揭榜 = 详情接口 Submission（列表瘦身）
const mineDetail = ref<ContestMySubmission | Submission | null>(null)
const revealed = ref(false)

const showSubmit = ref(false)
const showWorkbench = ref(false)
const showOthers = ref(false)
const showMine = ref(false)
const submitting = ref(false)
// 我的提交弹窗（对齐题库的弹窗形态）：列表点行 → 代码详情；隐藏期内不出分
const mineView = ref<'list' | 'code'>('list')
// 所有代码入口：题库全量可见人群（admin/root）在赛事做题页也可查看，普通参与者维持无此入口
const canViewOthers = computed(() => userStore.can('problem.view.all'))

const roundId = computed(() => Number(route.params.id))
const pid = computed(() => Number(route.params.pid))
const running = computed(() => round.value?.status === 'running')
const statusText = computed(() => {
  switch (round.value?.status) {
    case 'running': return '进行中'
    case 'scheduled': return '未开始'
    default: return '已结束'
  }
})

// 加载失败回退：比赛回赛事页；考试回班级列表（考试路由不在主导航）
function fallbackRoute(): string {
  return props.kind === 'exam' ? '/classes' : `${roundBase.value}/${roundId.value}`
}

// 赛事优先级（v0.7.3）：考试题被主题库比赛收录（隐藏期覆盖赛前/赛中/赛后）时，
// 一律引导进比赛做题页——行内切题/watch 重载都会经过 load()，统一在这里拦截
function contestSupersede(): string {
  if (props.kind !== 'exam') return ''
  const p = problem.value
  if (p?.locked && p.lock_kind === 'contest' && p.contest_id) {
    return `/contests/${p.contest_id}/problems/${p.id || pid.value}`
  }
  return ''
}

async function load() {
  loading.value = true
  try {
    // v0.65 三连改并行：round/problem 互不依赖（覆盖/结束判断都在两者到位后做），
    // loadMine 只依赖路由参数——三请求并发，高延迟链路整页遮罩省 2×RTT
    const [rd, p] = await Promise.all([
      api.value.detail(roundId.value),
      problemApi.detail(pid.value),
      loadMine(), // 自带 try/catch 不外抛，失败仅清列表
    ])
    round.value = rd
    problem.value = p
    const toContest = contestSupersede()
    if (toContest) {
      router.replace(toContest)
      return
    }
    // 考试彻底结束（已发布，锁已解除）：题目交还题库，学生入口回题库题目页
    // （被比赛覆盖的情形已在上面先进比赛）；组织者留在考试上下文看本场明细
    if (props.kind === 'exam' && round.value?.published && !round.value.is_organizer) {
      router.replace(`/problems/${pid.value}`)
      return
    }
  } catch (err) {
    // 隐藏期 404 也带锁信息（failData）：被比赛覆盖时同样引导进比赛，而不是弹回
    const lock = (err as Error & { data?: { locked?: boolean; lock_kind?: string; contest_id?: number } })?.data
    if (props.kind === 'exam' && lock?.locked && lock.lock_kind === 'contest' && lock.contest_id) {
      router.replace(`/contests/${lock.contest_id}/problems/${pid.value}`)
      return
    }
    router.replace(fallbackRoute())
  } finally {
    loading.value = false
  }
}

async function loadMine() {
  try {
    const r = await api.value.mySubmissions(roundId.value)
    mine.value = r.list.filter((s) => s.pid === pid.value)
    revealed.value = r.revealed
  } catch {
    mine.value = []
  }
}

function goProblem(targetPid: number) {
  if (targetPid === pid.value) return
  router.push(`${roundBase.value}/${roundId.value}/problems/${targetPid}`)
}

// 递交入口（提交/在线编程）只属于进行中的比赛；参赛者在未开始/已结束的场次
// 不再看到无效按钮（admin/root 保留，与题库详情页布局一致）
const showSubmitWays = computed(() => running.value || canViewOthers.value)

// 客观题（v0.9）：提交走作答弹窗，在线编程不适用，不受 has_cases 约束
const isObjective = computed(() => problem.value?.type === 'objective')
const isFunction = computed(() => problem.value?.type === 'function')
const funcTemplate = computed(() => problem.value?.function?.template_code || '')
const isInteractive = computed(() => problem.value?.type === 'interactive')
const noCases = computed(() => !isObjective.value && problem.value?.has_cases === false)

async function openMineDetail(s: ContestMySubmission) {
  if (!s.id) return
  // v0.64 拆形：已揭榜的列表行瘦身不带 code，点开按需拉详情；隐藏期行自带
  // code（考时回看唯一通道），直接展示
  if (revealed.value && !s.code) {
    try {
      mineDetail.value = await submissionApi.get(s.id)
    } catch {
      return // 拦截器已提示
    }
  } else {
    mineDetail.value = s
  }
  mineView.value = 'code'
}

// 批量重测本题（题库详情页同款，管理卡）：本题全部已出结果的提交低优先级重排队
const rejudgeRunning = ref(false)
async function rejudgeAll() {
  if (!problem.value || rejudgeRunning.value) return
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
    const r = await judgeQueueApi.rejudgeProblem(problem.value.id)
    ElMessage.success(r.count > 0 ? `已加入重测队列：${r.count} 条` : '本题没有可重测的提交')
  } finally {
    rejudgeRunning.value = false
  }
}

// 题解弹窗（揭榜后的答案解析卡内入口，卡片已按 revealed + can_view_solutions 显隐）
const showSolutions = ref(false)
const showVideo = ref(false)

// 客观题提交（v0.9）
async function handleObjectiveSubmit(answers: ObjAnswer[]) {
  submitting.value = true
  try {
    const r = await api.value.submitObjective(roundId.value, pid.value, answers)
    ElMessage.success('已提交')
    showSubmit.value = false
    await loadMine()
    if (revealed.value) pollRevealed(r.id)
  } finally {
    submitting.value = false
  }
}

async function handleSubmit(lang: string, code: string) {
  submitting.value = true
  try {
    const r = await api.value.submit(roundId.value, pid.value, lang, code)
    ElMessage.success('已提交')
    showSubmit.value = false
    showWorkbench.value = false
    await loadMine()
    // 赛时不轮询结果（不出分）；已公开时轮询刷新
    if (revealed.value) pollRevealed(r.id)
  } finally {
    submitting.value = false
  }
}

// 赛后已揭榜提交轮询（v0.37 迁 usePolling）：原手写 setInterval 只在「找到该 sid
// 且终态」才停——loadMine 失败或 sid 出页窗时 1.5s 死循环 + 拦截器每拍 toast 刷屏。
// 现在：拉取失败或连续 5 拍未找到该 sid 即停（列表里已经看不到它，无需再刷）；
// 后台标签自动暂停；10 分钟硬上限兜底。
let revealedSid = 0
let pollMisses = 0
const revealedPoll = usePolling(
  async () => {
    if (!revealedSid) return false
    let loaded = false
    try {
      await loadMine()
      loaded = true
    } catch {
      /* 拦截器已提示；连续失败走 miss 计数停轮 */
    }
    const s = loaded ? mine.value.find((x) => x.id === revealedSid) : undefined
    if (s && s.status && !isQueueStatus(s.status)) return false
    if (!s) {
      if (++pollMisses >= 5) return false
    } else {
      pollMisses = 0
    }
    return true
  },
  1500,
  { maxDurationMs: 10 * 60_000 },
)

function pollRevealed(sid: number) {
  revealedSid = sid
  pollMisses = 0
  revealedPoll.restart()
}

onMounted(load)

// 序号切题：同路由复用组件实例，pid 变化需重载数据
watch(
  () => route.params.pid,
  (nv, ov) => {
    if (nv && nv !== ov) load()
  },
)
// 轮询生命周期由 usePolling 的 onScopeDispose 接管（v0.37 迁移后不再手工清理）
</script>

<template>
  <div v-loading="loading">
    <!-- v0.79 题面专注模式：stmtFocus 时独立头与右栏让位，题面铺满。
         v0.80 用户拍板：统一头卡收进主列（不再 standalone 横跨全宽），副列留给功能卡 -->
    <PageShell v-if="round && problem" :hide-aside="stmtFocus">
      <el-card v-if="!stmtFocus" class="standalone-card round-head" :body-style="{ padding: 'var(--soj-sp-14) var(--soj-sp-20)' }">
          <!-- v0.77 统一头（拍板②全套统一）：面包屑（考试两段/比赛一段，前段可点直达）
               + 切题序号一体；「返回xx」按钮与旧「赛事名 / T{序号}」面包屑行退役 -->
          <PageCrumbs :items="crumbItems" />
          <div class="sa-head">
            <div class="sa-time">
              <span class="sub-status" :class="running ? 'blue' : 'gray'"><span class="dot" :class="running ? 'blue' : 'gray'"></span>{{ statusText }}</span>
              <span class="rtime">{{ fmtDate(round.start) }} ~ {{ fmtDate(round.end) }}</span>
              <span v-if="!revealed" class="rtime">OI 赛制：赛时只记录提交次数，不出分</span>
              <span class="sw-btns">
                <span class="sw-lab">切题</span>
                <button
                  v-for="(p, i) in round.problems"
                  :key="p.id"
                  type="button"
                  class="sw-btn"
                  :class="{ on: p.id === pid }"
                  @click="goProblem(p.id)"
                >
                  {{ i + 1 }}
                </button>
              </span>
            </div>
          </div>
      </el-card>

      <ProblemStatement :problem="problem" :file-io="round.file_io" />

      <!-- 客观题（v0.9.3）：作答区内联在题面下方；仅在赛中进行中可交 -->
      <ObjectiveAnswer
        v-if="isObjective && problem.questions?.length"
        :questions="problem.questions"
        :disabled="!running"
        :disabled-reason="running ? '' : `${name}未在进行中`"
        :submitting="submitting"
        @submit="handleObjectiveSubmit"
      />

      <ObjectiveSolutionCard
        v-if="isObjective && problem.questions_full?.length"
        :questions="problem.questions_full"
      />

      <template #aside>
        <!-- 管理卡（题库详情页同款）：编辑直达，免去回题库找原题 -->
        <RailCard v-if="problem.can_edit_statement" ov="管理" title="题目维护" class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="router.push(`/problems/${pid}/edit`)">编辑题目</el-button>
            <el-button style="width: 100%" :loading="rejudgeRunning" @click="rejudgeAll">批量重测本题</el-button>
          </div>
        </RailCard>

        <RailCard ov="提交" class="aside-card">
          <template #head>
            <div class="rail-ov">提交</div>
            <div class="rail-t">
              <TipLabel
                label="本题操作"
                :tip="!revealed
                  ? 'OI 赛制：赛时只记录提交次数不出分；「我的提交」仅显示已记录，可点开查看自己的代码'
                  : undefined"
              />
            </div>
          </template>
          <div class="entry-actions">
            <template v-if="showSubmitWays && !isObjective">
              <el-tooltip
                :disabled="!noCases"
                content="本题暂无数据，无法评测。请联系管理员上传数据"
                placement="top"
              >
                <el-button type="primary" style="width: 100%" :disabled="!running || noCases" @click="showSubmit = true">
                  提交
                </el-button>
              </el-tooltip>
              <el-tooltip
                v-if="!isObjective"
                :disabled="!noCases"
                content="本题暂无数据，无法评测。请联系管理员上传数据"
                placement="top"
              >
                <el-button style="width: 100%" :disabled="!running || noCases" @click="showWorkbench = true">
                  在线编程
                </el-button>
              </el-tooltip>
            </template>
            <el-button style="width: 100%" @click="showMine = true">我的提交</el-button>
            <el-button v-if="canViewOthers" style="width: 100%" @click="showOthers = true">所有代码</el-button>
            <div v-if="!running" class="hint center">{{ name }}未在进行中</div>
          </div>
        </RailCard>

        <!-- 答案解析（v0.23）：视频讲解默认可看，文字题解满分∨老师门槛，两钮同款白底黑字；
             比赛/考试隐藏期（未揭榜）整卡不显示 -->
        <RailCard
          v-if="revealed && (problem.solution_video_url || problem.can_view_solutions)"
          ov="解析"
          title="答案解析"
          class="aside-card"
        >
          <div class="entry-actions">
            <el-button v-if="problem.solution_video_url" style="width: 100%" @click="showVideo = true">
              视频讲解
            </el-button>
            <el-button v-if="problem.can_view_solutions" style="width: 100%" @click="showSolutions = true">
              文字题解
            </el-button>
          </div>
        </RailCard>
      </template>
    </PageShell>

    <template v-if="round && problem">
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
        :file-io="round.file_io"
        :submitting="submitting"
        :initial-code="isFunction ? funcTemplate : undefined"
        :lang-only="isFunction ? 'cpp' : isInteractive ? ['cpp', 'c'] : undefined"
        @submit="handleSubmit"
      />

      <SolutionsDialog v-model="showSolutions" :problem-id="pid" />

      <!-- 视频讲解弹窗（答案解析卡入口） -->
      <el-dialog v-model="showVideo" title="视频讲解" width="880px" top="6vh" destroy-on-close>
        <AnswerAnalysisCard v-if="problem.solution_video_url" :url="problem.solution_video_url" />
      </el-dialog>
      <AllCodesDialog v-if="problem" v-model="showOthers" :problem-id="problem.id" :problem-title="problem.title" />

      <!-- 我的提交（题库弹窗同形态）：隐藏期内不出分（状态列显示「已记录」），
           点击行查看自己的代码；不提供判题明细入口 -->
      <el-dialog v-model="showMine" title="我的提交" width="1020" top="4vh" @closed="mineView = 'list'">
        <template v-if="mineView === 'list'">
          <el-table stripe :data="mine" highlight-current-row @row-click="openMineDetail">
            <el-table-column label="状态" width="140">
              <template #default="{ row }">
                <SubStatus v-if="revealed && row.status" :status="row.status" :score="row.score" />
                <span v-else class="dim">已记录</span>
              </template>
            </el-table-column>
            <el-table-column prop="language" label="语言" width="100" />
            <el-table-column label="总耗时" width="100">
              <template #default="{ row }">
                {{ revealed ? fmtSec(row.time_ms ?? 0) : '—' }}
              </template>
            </el-table-column>
            <el-table-column label="总内存" width="110">
              <template #default="{ row }">
                {{ revealed && row.memory_kb ? fmtMem(row.memory_kb) : '—' }}
              </template>
            </el-table-column>
            <el-table-column label="提交时间">
              <template #default="{ row }">{{ fmtRel(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!mine.length" description="还没有提交" :image-size="48" />
        </template>
        <template v-else-if="mineDetail">
          <el-button class="mb8" @click="mineView = 'list'">← 返回列表</el-button>
          <el-descriptions :column="3" border class="detail-desc">
            <el-descriptions-item label="语言">{{ mineDetail.language }}</el-descriptions-item>
            <el-descriptions-item label="提交时间">{{ fmtDate(mineDetail.created_at) }}</el-descriptions-item>
          </el-descriptions>
          <div class="detail-sub">代码</div>
          <CodePane :code="mineDetail.code" :language="mineDetail.language" />
        </template>
      </el-dialog>
    </template>
  </div>
</template>

<style scoped>
/* v0.80 统一头收进主列：下距对齐原 standalone 行距（16px），与题面衔接 */
.round-head {
  margin-bottom: var(--soj-sp-16);
}

/* v0.77 统一头：面包屑行与切题行间距（Crumbs 自带样式，旧 .crumbs/.sa-title 退役） */
.round-head :deep(.soj-crumbs) {
  margin-bottom: var(--soj-sp-10);
}

/* v3 .rtime：--soj-fs-12 弱化说明 */
.rtime {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* v3 .sw-btns：34x32 圆角 10 切题方块钮（并入状态行右侧，v0.77 统一头单行化） */
.sw-btns {
  display: inline-flex;
  gap: var(--soj-sp-6);
  margin-left: var(--soj-sp-12);
  align-items: center;
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

.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: var(--soj-sp-10) 0 0;
}

.sa-time {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  flex-wrap: wrap;
}

.sa-switch {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-top: var(--soj-sp-8);
}

.switch-btn {
  font-weight: var(--soj-fw-emphasis);
}

.center {
  text-align: center;
}
</style>

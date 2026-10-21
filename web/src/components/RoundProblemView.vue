<script setup lang="ts">
// 比赛/考试做题页共享组件（v0.5.4 ① 合并，原 ContestProblemView/ExamProblemView 94→97% 同构）。
// 差异仅三处：文案（比赛/考试）、详情页路由前缀（/contests|/exams）、加载失败回退
// （比赛回赛事页、考试回班级列表）——全部由 kind 派生。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'

import { contestApi, examApi, problemApi, type RoundKind } from '@/api'
import { judgeQueueApi } from '@/api/judgeQueue'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import SubStatus from '@/components/SubStatus.vue'
import PageShell from '@/components/PageShell.vue'
import AllCodesDialog from '@/components/problem/AllCodesDialog.vue'
import ProblemInfoCard from '@/components/problem/ProblemInfoCard.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import SolutionsDialog from '@/components/problem/SolutionsDialog.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import { fmtDate, fmtMem, isQueueStatus } from '@/utils/format'
import { useUserStore } from '@/stores/user'
import type { ContestDetail, ContestMySubmission, ProblemDetail } from '@/types'

const props = defineProps<{ kind: RoundKind }>()

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const api = computed(() => (props.kind === 'exam' ? examApi : contestApi))
const name = computed(() => (props.kind === 'exam' ? '考试' : '比赛'))
const roundBase = computed(() => `/${props.kind}s`)

const round = ref<ContestDetail | null>(null)
const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
const mine = ref<ContestMySubmission[]>([])
const revealed = ref(false)

const showSubmit = ref(false)
const showWorkbench = ref(false)
const showOthers = ref(false)
const showMine = ref(false)
const submitting = ref(false)
// 我的提交弹窗（对齐题库的弹窗形态）：列表点行 → 代码详情；隐藏期内不出分
const mineView = ref<'list' | 'code'>('list')
const mineDetail = ref<ContestMySubmission | null>(null)
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
    round.value = await api.value.detail(roundId.value)
    problem.value = await problemApi.detail(pid.value)
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
    await loadMine()
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

function openMineDetail(s: ContestMySubmission) {
  if (!s.id) return
  mineDetail.value = s
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

// 题解弹窗（揭榜后显示入口）：未满分时后端会拒绝，弹窗内为空并提示——
// 入口处直接用详情接口的 can_view_solutions 给出提示，不打无意义的请求
const showSolutions = ref(false)

function openSolutions() {
  if (!problem.value?.can_view_solutions) {
    ElMessage.warning('该题拿到满分后才能查看题解')
    return
  }
  showSolutions.value = true
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

let pollTimer: ReturnType<typeof setInterval> | undefined
function pollRevealed(sid: number) {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = setInterval(async () => {
    await loadMine()
    const s = mine.value.find((x) => x.id === sid)
    if (s && s.status && !isQueueStatus(s.status)) {
      if (pollTimer) clearInterval(pollTimer)
    }
  }, 1500)
}

onMounted(load)

// 序号切题：同路由复用组件实例，pid 变化需重载数据
watch(
  () => route.params.pid,
  (nv, ov) => {
    if (nv && nv !== ov) load()
  },
)

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<template>
  <div v-loading="loading">
    <PageShell v-if="round && problem">
      <template #standalone>
        <el-card class="standalone-card" :body-style="{ padding: '10px 20px' }">
          <div class="sa-head">
            <div class="sa-time">
              <span class="sa-title">{{ round.title }}</span>
              <el-tag size="small" :type="running ? 'success' : 'info'">{{ statusText }}</el-tag>
              <span class="dim">{{ name }}时间：{{ fmtDate(round.start) }} ~ {{ fmtDate(round.end) }}</span>
              <span v-if="!revealed" class="dim">OI 赛制：赛时只记录提交次数，不出分</span>
            </div>
            <el-button class="sa-back" @click="router.push(`${roundBase}/${round.id}`)">
              <el-icon><ArrowLeft /></el-icon>
              返回{{ name }}
            </el-button>
          </div>
          <div class="sa-switch">
            <el-button
              v-for="(p, i) in round.problems"
              :key="p.id"
              :type="p.id === pid ? 'primary' : 'default'"
              size="small"
              class="switch-btn"
              @click="goProblem(p.id)"
            >
              {{ i + 1 }}
            </el-button>
          </div>
        </el-card>
      </template>

      <ProblemStatement :problem="problem" :file-io="round.file_io" />
      <template #aside>
        <!-- 管理卡（题库详情页同款）：编辑直达，免去回题库找原题 -->
        <el-card v-if="problem.can_edit_statement" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="router.push(`/problems/${pid}/edit`)">编辑题目</el-button>
            <el-button plain style="width: 100%" :loading="rejudgeRunning" @click="rejudgeAll">批量重测本题</el-button>
          </div>
        </el-card>

        <el-card class="aside-card">
          <div class="entry-actions">
            <template v-if="showSubmitWays">
              <el-tooltip
                :disabled="problem?.has_cases !== false"
                content="本题暂无数据，无法评测。请联系管理员上传数据"
                placement="top"
              >
                <el-button type="primary" plain style="width: 100%" :disabled="!running || problem?.has_cases === false" @click="showSubmit = true">
                  提交
                </el-button>
              </el-tooltip>
              <el-tooltip
                :disabled="problem?.has_cases !== false"
                content="本题暂无数据，无法评测。请联系管理员上传数据"
                placement="top"
              >
                <el-button type="success" plain style="width: 100%" :disabled="!running || problem?.has_cases === false" @click="showWorkbench = true">
                  在线编程
                </el-button>
              </el-tooltip>
            </template>
            <el-button type="warning" plain style="width: 100%" @click="openSolutions">题解</el-button>
            <el-button style="width: 100%" @click="showMine = true">我的提交</el-button>
            <el-button v-if="canViewOthers" style="width: 100%" @click="showOthers = true">所有代码</el-button>
            <div v-if="!running" class="dim center">{{ name }}未在进行中</div>
          </div>
        </el-card>

        <ProblemInfoCard v-if="revealed || canViewOthers" :problem="problem" />
      </template>
    </PageShell>

    <template v-if="round && problem">
      <SubmitDialog
        v-model="showSubmit"
        :title="`#${problem.id} ${problem.title}`"
        :submitting="submitting"
        @submit="handleSubmit"
      />

      <CodeWorkbench
        v-model="showWorkbench"
        :title="`#${problem.id} ${problem.title}`"
        :problem="problem"
        :file-io="round.file_io"
        :submitting="submitting"
        @submit="handleSubmit"
      />

      <SolutionsDialog v-model="showSolutions" :problem-id="pid" />
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
                {{ revealed ? `${row.time_ms ?? 0} ms` : '—' }}
              </template>
            </el-table-column>
            <el-table-column label="总内存" width="110">
              <template #default="{ row }">
                {{ revealed && row.memory_kb ? fmtMem(row.memory_kb) : '—' }}
              </template>
            </el-table-column>
            <el-table-column label="提交时间">
              <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!mine.length" description="还没有提交" :image-size="48" />
          <div v-if="mine.length && !revealed" class="dim center mine-hint">
            隐藏期内不出分，点击行可查看自己的代码
          </div>
        </template>
        <template v-else-if="mineDetail">
          <el-button class="mb8" @click="mineView = 'list'">← 返回列表</el-button>
          <el-descriptions :column="3" border class="detail-desc">
            <el-descriptions-item label="语言">{{ mineDetail.language }}</el-descriptions-item>
            <el-descriptions-item label="提交时间">{{ fmtDate(mineDetail.created_at) }}</el-descriptions-item>
          </el-descriptions>
          <div class="detail-sub">代码</div>
          <pre class="mono sub-code">{{ mineDetail.code }}</pre>
        </template>
      </el-dialog>
    </template>
  </div>
</template>

<style scoped>
.standalone-card {
  margin-bottom: 0;
}

.sa-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.sa-title {
  font-size: var(--soj-fs-page-title);
  font-weight: var(--soj-fw-emphasis);
}

.sa-time {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.sa-back {
  flex: none;
}

.sa-switch {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}

.switch-btn {
  font-weight: var(--soj-fw-emphasis);
}

.center {
  text-align: center;
}

/* 我的提交弹窗脚注 */
.mine-hint {
  margin-top: 8px;
}
</style>

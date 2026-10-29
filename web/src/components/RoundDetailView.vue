<script setup lang="ts">
// 比赛/考试详情页共享组件（v0.5.4 合并；v0.7 按 design.md 重排；v0.7.2 副列重排；v0.7.4 管理卡精简）。
// 主列：名称卡（20px + 说明）→ 题目表（T1/状态/题目/标签/通过率）。
// 副列：管理卡（编辑/调整题目/重测/删除，说明与场景开关在编辑弹窗内）→ 时间卡 → 成绩表。
import { computed, onMounted, ref } from 'vue'
import { usePolling } from '@/composables/usePolling'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { authApi, contestApi, examApi, type RoundKind } from '@/api'
import { downloadFile } from '@/api/download'
import { judgeQueueApi } from '@/api/judgeQueue'
import EditRoundDialog from '@/components/EditRoundDialog.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import FitTags from '@/components/problem/FitTags.vue'
import SubStatus from '@/components/SubStatus.vue'
import TierBadge from '@/components/TierBadge.vue'
import { buildRoundBoard } from '@/components/score/adapters'
import ScoreBoardDialog from '@/components/score/ScoreBoardDialog.vue'
import ScoreSummaryCard from '@/components/score/ScoreSummaryCard.vue'
import type { ScoreBoardView } from '@/components/score/types'
import UserSubmissionsDialog from '@/components/score/UserSubmissionsDialog.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { useSiteStore } from '@/stores/site'
import { useUserStore } from '@/stores/user'
import { fmtDate } from '@/utils/format'
import type { ContestDetail, LeaderRow, Submission } from '@/types'
import { defineAsyncComponent } from 'vue'
// 讨论弹窗按需加载（加载优化批）：不点开不下载讨论 chunk
const DiscussDialog = defineAsyncComponent(() => import('@/components/discuss/DiscussDialog.vue'))

const props = defineProps<{ kind: RoundKind }>()

const route = useRoute()
const router = useRouter()
const site = useSiteStore()
const userStore = useUserStore()
const tagStore = useTagTreeStore()

const api = computed(() => (props.kind === 'exam' ? examApi : contestApi))
const name = computed(() => (props.kind === 'exam' ? '考试' : '比赛'))
const roundBase = computed(() => `/${props.kind}s`)

const round = ref<ContestDetail | null>(null)
const loading = ref(true)
const leaders = ref<LeaderRow[]>([])
const leadersLoading = ref(false)
const myUid = ref<number | null>(null)
const showScoreBoard = ref(false)
const showDiscuss = ref(false)

// 榜单单元格点看用户提交明细：组织者本人 ∨「查看用户提交明细」∨「查看比赛/考试成绩表详情」
const canInspectSubs = computed(
  () =>
    !!round.value?.is_organizer ||
    userStore.can('submission.view.user') ||
    userStore.can('contest.score.detail'),
)
const scoreBoard = computed<ScoreBoardView | null>(() => {
  if (!round.value) return null
  return buildRoundBoard(props.kind, round.value, leaders.value, canInspectSubs.value)
})
const scoreTitle = computed(() => (round.value ? `成绩表 · ${round.value.title}` : '成绩表'))
const scoreEmptyText = computed(() => {
  if (!userStore.token) return '登录后查看成绩'
  if (round.value?.status === 'scheduled') return `${name.value}开始前不显示成绩`
  if (round.value?.status === 'running') return `OI 赛制：${name.value}进行中不显示成绩`
  if (round.value && !round.value.published) return '结果尚未公开'
  return '暂无成绩'
})
const showUserSubs = ref(false)
const userSubsLoading = ref(false)
const userSubs = ref<Submission[]>([])
const userSubsTitle = ref('')

async function openUserSubs(uid: number, uname: string, pid: number, ptitle: string) {
  showUserSubs.value = true
  userSubsLoading.value = true
  userSubsTitle.value = `${uname} · ${ptitle}`
  try {
    const r = await api.value.userSubmissions(Number(route.params.id), pid, uid)
    userSubs.value = r.list
  } finally {
    userSubsLoading.value = false
  }
}

function inspectCell(payload: { uid: number; name: string; pid: number; title: string }) {
  openUserSubs(payload.uid, payload.name, payload.pid, payload.title)
}

const showEditRound = ref(false)
const editMode = ref<'full' | 'problems'>('full')

function openEdit(mode: 'full' | 'problems') {
  editMode.value = mode
  showEditRound.value = true
}

const clock = usePolling(() => {
  now.value = Date.now()
}, 1000)

const now = ref(Date.now())

const statusLabel = computed(() => {
  const map: Record<string, string> = { running: '进行中', scheduled: '未开始', ended: '已结束' }
  return round.value ? map[round.value.status] : ''
})

// v3 countdown 格式：X 天 HH : MM : SS（不足一天只出时分秒）
function fmtClock(ms: number): string {
  const sec = Math.max(0, Math.floor(ms / 1000))
  const days = Math.floor(sec / 86400)
  const hh = String(Math.floor((sec % 86400) / 3600)).padStart(2, '0')
  const mm = String(Math.floor((sec % 3600) / 60)).padStart(2, '0')
  const ss = String(sec % 60).padStart(2, '0')
  return days > 0 ? `${days} 天 ${hh} : ${mm} : ${ss}` : `${hh} : ${mm} : ${ss}`
}

const countdownLabel = computed(() => {
  if (!round.value) return ''
  if (round.value.status === 'scheduled') return fmtClock(new Date(round.value.start).getTime() - now.value)
  if (round.value.status === 'running') return fmtClock(new Date(round.value.end).getTime() - now.value)
  return '已结束'
})

// 赛程进度条：未开始 0%，进行中按时间比例，已结束 100%
const progressPct = computed(() => {
  const c = round.value
  if (!c) return 0
  const start = new Date(c.start).getTime()
  const end = new Date(c.end).getTime()
  if (c.status === 'scheduled') return 0
  if (c.status === 'ended') return 100
  const total = end - start
  if (total <= 0) return 100
  return Math.min(100, Math.max(0, Math.round(((now.value - start) / total) * 100)))
})

const problemTotal = computed(() => round.value?.problems?.length ?? 0)

// v3 起止行尾注：进行中 = 已进行 N%；未开始/已结束给状态短语
const progressHint = computed(() => {
  const c = round.value
  if (!c) return ''
  if (c.status === 'scheduled') return '未开始'
  if (c.status === 'ended') return '已结束'
  return `已进行 ${progressPct.value}%`
})

// 成绩表可见：组织者恒可见；他人需「查看比赛/考试成绩表」权限位（v0.11，root/admin 默认
// 持有、default 无），且需开启成绩表且结果已发布（关隐藏时赛中实时可见）——与服务端同规则
// 题目行跳转（v0.7.3 优先级）：
//   考试题被主题库比赛收录（隐藏期）→ 引导进比赛做题页；
//   考试彻底结束（已发布，锁已解除）→ 学生入口回题库题目页，组织者留在考试上下文看本场明细；
//   其余（考试进行中/隐藏期内）→ 考试做题页
function problemRoute(row: { id?: number; contest_id?: number }) {
  const pid = row?.id
  if (!pid || !round.value) return
  if (props.kind === 'exam' && row.contest_id) {
    router.push(`/contests/${row.contest_id}/problems/${pid}`)
    return
  }
  if (props.kind === 'exam' && round.value.published && !round.value.is_organizer) {
    router.push(`/problems/${pid}`)
    return
  }
  router.push(`${roundBase.value}/${round.value.id}/problems/${pid}`)
}
const boardVisible = computed(() => {
  const c = round.value
  if (!c || !userStore.token) return false
  if (c.is_organizer) return true
  if (!userStore.can('contest.score.view')) return false
  if (!c.scoreboard) return false
  return c.published || (c.status === 'running' && !c.hide_during)
})

// 通过率 = AC 提交 / 总提交（与题库同口径）
function acceptRate(row: { submit_count?: number; accepted_count?: number }): string {
  if (!row.submit_count) return '—'
  const pct = ((row.accepted_count ?? 0) / row.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
}

async function load() {
  try {
    round.value = await api.value.detail(Number(route.params.id))
    if (boardVisible.value) loadLeaders()
  } catch {
    // 加载失败回退：比赛回赛事列表；考试回班级列表（考试路由不在主导航）
    router.replace(props.kind === 'exam' ? '/classes' : '/contests')
  } finally {
    loading.value = false
  }
}

async function loadLeaders() {
  if (!userStore.token) {
    leaders.value = []
    return
  }
  leadersLoading.value = true
  try {
    leaders.value = await api.value.leaderboard(Number(route.params.id))
  } catch {
    leaders.value = []
  } finally {
    leadersLoading.value = false
  }
}

// 成绩导出 CSV（v0.5.6）：仅组织者可见；服务端现算榜单，列含排名/昵称/各题得分/总分
const exporting = ref(false)
async function exportCsv() {
  if (!round.value) return
  exporting.value = true
  try {
    await downloadFile(`/${props.kind}s/${round.value.id}/export`, `${round.value.title}.csv`)
  } finally {
    exporting.value = false
  }
}

async function remove() {
  if (!round.value) return
  try {
    await ElMessageBox.confirm(
      `确定删除${name.value}「${round.value.title}」吗？删除后不可恢复！`,
      '⚠️ 危险操作：确认删除',
      { type: 'error', confirmButtonText: '确认删除', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  await api.value.remove(round.value.id)
  ElMessage.success('已删除')
  // 删除后回退：比赛回赛事列表；考试回所属班级
  router.push(props.kind === 'exam' ? `/classes/${round.value.class_id}` : '/contests')
}

// 批量重测（v0.7.1）：本场全部已出结果的提交低优先级重排队，不挤占学生新提交
const rejudgeAllRunning = ref(false)
async function rejudgeAll() {
  const r = round.value
  if (!r || rejudgeAllRunning.value) return
  try {
    await ElMessageBox.confirm(
      `将把本场${name.value}所有已出结果的提交重新评测（低优先级，不影响学生新提交的评测顺序），确定？`,
      '批量重测',
      { type: 'warning', confirmButtonText: '批量重测', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  rejudgeAllRunning.value = true
  try {
    const res = await judgeQueueApi.rejudgeRound(props.kind, r.id)
    ElMessage.success(res.count > 0 ? `已加入重测队列：${res.count} 条` : '本场没有可重测的提交')
  } finally {
    rejudgeAllRunning.value = false
  }
}

onMounted(() => {
  tagStore.ensure() // 标签名映射异步就绪，题目表标签列响应式跟随
  if (userStore.token) {
    authApi.me().then((me) => {
      myUid.value = me.id
      if (boardVisible.value && !leaders.value.length) loadLeaders()
    }).catch(() => {})
  }
  load()
  clock.start() // 每秒时钟（usePolling：后台标签暂停、卸载自清）
})
</script>

<template>
  <div v-loading="loading">
    <PageShell v-if="round">
      <!-- 名称卡：名称 20px + 说明（说明不再单独成卡） -->
      <el-card :body-style="{ padding: '22px 26px' }">
        <div class="round-body">
          <div class="title-row">
            <span class="big-title">{{ round.title }}</span>
            <span class="sub-status" :class="round.status === 'running' ? 'blue' : round.status === 'scheduled' ? 'yellow' : 'gray'">
              <span class="dot" :class="round.status === 'running' ? 'blue' : round.status === 'scheduled' ? 'yellow' : 'gray'"></span>{{ statusLabel }}
            </span>
            <el-tag v-if="kind === 'contest'" size="small">主题库 · 全员</el-tag>
          </div>
          <el-alert
            v-if="kind === 'exam' && !round.participant && !userStore.token"
            title="登录并加入对应班级后才能参加本场考试"
            type="info"
            :closable="false"
            class="mt8"
          />
          <div v-if="round.intro" class="intro"><MdView :content="round.intro" :html="round.intro_html" /></div>
          <!-- 起止时间在右栏「时间」卡展示，名称卡只留赛后隐藏期提示（v0.7.2 验收反馈） -->
          <div v-if="round.hidden_until" class="meta">
            <el-tag size="small" type="info">
              赛后隐藏至 {{ fmtDate(round.hidden_until) }}
            </el-tag>
          </div>
        </div>
      </el-card>

      <!-- 题目表：T1 / 状态 / 题目 / 难度 / 知识点 / 通过率（难度与知识点按场次 hide_meta
           开关服务端下发：隐藏时字段缺席渲染横杠，2026-10-27） -->
      <el-card class="block">
        <template #header><span>题目（{{ problemTotal }}）</span></template>
        <el-table v-if="round.problems?.length" stripe :data="round.problems">
          <el-table-column label="题号" width="80">
            <template #default="{ $index }">T{{ $index + 1 }}</template>
          </el-table-column>
          <el-table-column v-if="userStore.token" label="状态" width="150">
            <template #default="{ row }">
            <template v-if="row.result_hidden">
              <el-tag v-if="row.submitted" size="small" type="info">已提交</el-tag>
              <span v-else class="dim">—</span>
            </template>
            <SubStatus v-else-if="row.status" :status="row.status" :score="row.my_score" />
            <span v-else class="dim">未做</span>
            </template>
          </el-table-column>
          <el-table-column label="题目" min-width="260">
            <template #default="{ row }">
              <span class="tlink" @click="problemRoute(row)">{{ row.title }}</span>
            </template>
          </el-table-column>
          <el-table-column label="难度" width="104">
            <template #default="{ row }">
              <TierBadge v-if="row.difficulty != null" :tier="row.difficulty" kind="difficulty" />
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="知识点" min-width="170">
            <template #default="{ row }">
              <FitTags v-if="row.tags?.length" :ids="row.tags" />
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="通过率" width="110">
            <template #default="{ row }">
              <el-tooltip
                v-if="!row.result_hidden && row.submit_count"
                :content="`AC ${row.accepted_count ?? 0} / 提交 ${row.submit_count}`"
                placement="top"
              >
                <span>{{ acceptRate(row) }}</span>
              </el-tooltip>
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
        </el-table>
        <el-alert
          v-else
          :title="round.status === 'scheduled' ? `${name}开始前题目隐藏，开始后可见` : '仅本班成员可见题目'"
          type="info"
          :closable="false"
        />
      </el-card>

      <template #aside>
        <!-- 管理卡（v0.7.4 精简）：编辑说明与三个场景开关并入编辑弹窗，仅剩动作按钮 -->
        <RailCard v-if="round.is_organizer" ov="管理" :title="`${name}管理`" class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="openEdit('full')">编辑{{ name }}</el-button>
            <el-button style="width: 100%" @click="openEdit('problems')">调整题目</el-button>
            <el-button style="width: 100%" :loading="rejudgeAllRunning" @click="rejudgeAll">重测本场提交</el-button>
            <el-button v-if="round.deletable" type="danger" style="width: 100%" @click="remove">删除{{ name }}</el-button>
          </div>
        </RailCard>

        <!-- 时间卡（v3：countdown 浅底虚线框 + 渐变进度条 + 起止行） -->
        <RailCard ov="时间" title="剩余时间" class="aside-card">
          <div class="soj-countdown">{{ countdownLabel }}</div>
          <div class="soj-pbar" style="margin-top: 10px"><i :style="{ width: `${progressPct}%` }" /></div>
          <div class="range dim">{{ fmtDate(round.start) }} ~ {{ fmtDate(round.end) }} · {{ progressHint }}</div>
        </RailCard>

        <!-- 讨论入口（v0.24）：仅主题库比赛配讨论区（考试不配，考试题走题内讨论赛时锁定） -->
        <RailCard v-if="kind === 'contest' && site.discussContestEnabled" ov="互动" title="比赛讨论" class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" plain style="width: 100%" @click="showDiscuss = true">
              进入讨论区
            </el-button>
          </div>
          <p class="range dim">赛时提问仅组织者可见，可被升格公开答复；赛后转公开复盘。</p>
        </RailCard>

        <ScoreSummaryCard
          v-if="boardVisible"
          class="aside-card"
          :board="scoreBoard"
          :loading="leadersLoading"
          :visible="true"
          :empty-text="scoreEmptyText"
          :my-uid="myUid"
          :can-refresh="true"
          @open="showScoreBoard = true"
          @refresh="loadLeaders"
        />
      </template>
    </PageShell>

    <!-- 比赛讨论弹窗（v0.24；showDiscuss 条件配合异步组件实现真延迟） -->
    <DiscussDialog
      v-if="showDiscuss && round && kind === 'contest'"
      v-model="showDiscuss"
      scope="contest"
      :scope-id="round.id"
      :title="`${round.title} · 讨论`"
      :contest-running="round.status === 'running'"
    />

    <EditRoundDialog
      v-if="round"
      v-model="showEditRound"
      :kind="kind"
      :round="round"
      :mode="editMode"
      @saved="load"
    />

    <ScoreBoardDialog
      v-model="showScoreBoard"
      :board="scoreBoard"
      :title="scoreTitle"
      :loading="leadersLoading"
      :can-export="!!round?.is_organizer"
      :exporting="exporting"
      :my-uid="myUid"
      @refresh="loadLeaders"
      @export="exportCsv"
      @inspect="inspectCell"
    />

    <UserSubmissionsDialog
      v-model="showUserSubs"
      :title="`提交明细 · ${userSubsTitle}`"
      :loading="userSubsLoading"
      :list="userSubs"
    />
  </div>
</template>

<style scoped>
.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* 名称卡与题面同构：随主列撑满卡片，与下方满宽卡片左右缘对齐 */
.round-body {
  margin: 0;
}

/* v3-full 实体大标题：藏蓝 28/850（本地 800，比赛/考试名） */
.big-title {
  font-size: var(--soj-fs-entity-detail);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -0.5px;
  color: var(--soj-navy);
}

.intro {
  margin: 12px 0 4px;
  color: var(--el-text-color-regular);
}

.meta {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 10px;
}

.block {
  margin-top: 16px;
}

.mt8 {
  margin-top: 8px;
}

/* v3 .tc-range：起止时间行 */
.range {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  text-align: center;
  margin-top: 8px;
}
</style>

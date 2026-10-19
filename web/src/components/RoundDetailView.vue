<script setup lang="ts">
// 比赛/考试详情页共享组件（v0.5.4 合并；v0.7 按 design.md 重排）。
// 主列：名称卡（20px + 说明）→ 时间/提交进度卡 → 题目表（T1/状态/题目/标签/通过率）。
// 副列：管理卡（编辑比赛/考试、调整题目、文件 IO/成绩表/赛期内不可见开关）→ 成绩表。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { authApi, contestApi, examApi, type RoundKind } from '@/api'
import { downloadCsv } from '@/api/download'
import EditRoundDialog from '@/components/EditRoundDialog.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import FitTags from '@/components/problem/FitTags.vue'
import SubStatus from '@/components/SubStatus.vue'
import { buildRoundBoard } from '@/components/score/adapters'
import ScoreBoardDialog from '@/components/score/ScoreBoardDialog.vue'
import ScoreSummaryCard from '@/components/score/ScoreSummaryCard.vue'
import type { ScoreBoardView } from '@/components/score/types'
import UserSubmissionsDialog from '@/components/score/UserSubmissionsDialog.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { useUserStore } from '@/stores/user'
import { fmtDate } from '@/utils/format'
import type { ContestDetail, LeaderRow, Submission } from '@/types'

const props = defineProps<{ kind: RoundKind }>()

const route = useRoute()
const router = useRouter()
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

// 榜单单元格点看用户提交明细：组织者本人 ∨「查看用户提交明细」权限位
const canInspectSubs = computed(
  () => !!round.value?.is_organizer || userStore.can('submission.view.user'),
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

const fileIOSaving = ref(false)
const flagSaving = ref(false)

async function toggleFileIO(v: boolean) {
  fileIOSaving.value = true
  try {
    await api.value.setFileIO(Number(route.params.id), v)
    round.value!.file_io = v
    ElMessage.success(v ? '本场已启用文件 IO，之后的提交按文件读写判题' : '本场已关闭文件 IO，之后的提交按标准输入输出判题')
  } finally {
    fileIOSaving.value = false
  }
}

// v0.7 两个场景开关：保存后重载详情（题目表可见性/成绩表入口随之变化）
async function saveFlag(patch: { scoreboard?: boolean; hide_during?: boolean }) {
  if (!round.value) return
  flagSaving.value = true
  try {
    await api.value.update(round.value.id, patch)
    await load()
    ElMessage.success('设置已保存')
  } finally {
    flagSaving.value = false
  }
}

const showEditRound = ref(false)
const editMode = ref<'full' | 'problems'>('full')

function openEdit(mode: 'full' | 'problems') {
  editMode.value = mode
  showEditRound.value = true
}

let timer: ReturnType<typeof setInterval> | undefined

const now = ref(Date.now())

const statusLabel = computed(() => {
  const map: Record<string, string> = { running: '进行中', scheduled: '未开始', ended: '已结束' }
  return round.value ? map[round.value.status] : ''
})

const countdownLabel = computed(() => {
  if (!round.value) return ''
  if (round.value.status === 'scheduled') return fmtDur(new Date(round.value.start).getTime() - now.value)
  if (round.value.status === 'running') return fmtDur(new Date(round.value.end).getTime() - now.value)
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

const submittedCount = computed(
  () => (round.value?.problems ?? []).filter((p) => p.submitted).length,
)
const problemTotal = computed(() => round.value?.problems?.length ?? 0)
const submitProgressPct = computed(() => {
  const total = problemTotal.value
  if (total <= 0) return 0
  return Math.round((submittedCount.value / total) * 100)
})

// 成绩表可见：组织者恒可见；参与者需开启成绩表且结果已发布（关隐藏时赛中实时可见）
const boardVisible = computed(() => {
  const c = round.value
  if (!c || !userStore.token) return false
  if (c.is_organizer) return true
  if (!c.scoreboard) return false
  return c.published || (c.status === 'running' && !c.hide_during)
})

function fmtDur(ms: number): string {
  if (ms <= 0) return '0'
  const s = Math.floor(ms / 1000)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  return (d > 0 ? `${d}天` : '') + `${h}小时${m}分${sec}秒`
}

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
    await downloadCsv(`/${props.kind}s/${round.value.id}/export`, `${round.value.title}.csv`)
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

onMounted(() => {
  tagStore.ensure() // 标签名映射异步就绪，题目表标签列响应式跟随
  if (userStore.token) {
    authApi.me().then((me) => {
      myUid.value = me.id
      if (boardVisible.value && !leaders.value.length) loadLeaders()
    }).catch(() => {})
  }
  load()
  timer = setInterval(() => {
    now.value = Date.now()
  }, 1000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div v-loading="loading">
    <PageShell v-if="round">
      <!-- 名称卡：名称 20px + 说明（说明不再单独成卡） -->
      <el-card>
        <div class="round-body">
          <div class="title-row">
            <span class="big-title">{{ round.title }}</span>
            <el-tag :type="round.status === 'running' ? 'success' : round.status === 'scheduled' ? 'warning' : 'info'">
              {{ statusLabel }}
            </el-tag>
            <el-tag v-if="kind === 'contest'" size="small" type="info">主题库 · 全员</el-tag>
          </div>
          <el-alert
            v-if="kind === 'exam' && !round.participant && !userStore.token"
            title="登录并加入对应班级后才能参加本场考试"
            type="info"
            :closable="false"
            class="mt8"
          />
          <div v-if="round.intro_html || round.intro" class="intro"><MdView :content="round.intro_html || round.intro" /></div>
          <div class="meta">
            <el-tag size="small" type="info">{{ fmtDate(round.start) }} ~ {{ fmtDate(round.end) }}</el-tag>
            <el-tag v-if="round.hidden_until" size="small" type="info">
              赛后隐藏至 {{ fmtDate(round.hidden_until) }}
            </el-tag>
          </div>
        </div>
      </el-card>

      <!-- 时间进度 + 提交进度 -->
      <el-card class="block">
        <div class="progress-head">
          <span class="progress-title">{{ round.status === 'scheduled' ? '开始倒计时' : round.status === 'running' ? '剩余时间' : '赛程' }}</span>
          <span class="countdown">{{ countdownLabel }}</span>
        </div>
        <el-progress
          :percentage="progressPct"
          :status="round.status === 'ended' ? 'success' : undefined"
          :stroke-width="10"
        />
        <div class="dim center">{{ fmtDate(round.start) }} ~ {{ fmtDate(round.end) }}</div>
        <template v-if="userStore.token && round.participant && problemTotal > 0">
          <div class="submit-head">
            <span>提交进度</span>
            <span>{{ submittedCount }} / {{ problemTotal }}</span>
          </div>
          <el-progress :percentage="submitProgressPct" :stroke-width="8" />
        </template>
      </el-card>

      <!-- 题目表：T1 / 状态 / 题目 / 标签 / 通过率 -->
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
              <el-link type="primary" @click="router.push(`${roundBase}/${round!.id}/problems/${row.id}`)">
                {{ row.title }}
              </el-link>
            </template>
          </el-table-column>
          <el-table-column label="标签" min-width="170">
            <template #default="{ row }">
              <FitTags v-if="!row.result_hidden && row.tags?.length" :ids="row.tags" />
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
        <!-- 管理卡：编辑比赛/考试（名称/时间/说明）、调整题目 → 文件 IO / 成绩表 / 赛期内不可见 -->
        <el-card v-if="round.is_organizer" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="openEdit('full')">编辑{{ name }}</el-button>
            <el-button type="success" style="width: 100%" @click="openEdit('problems')">调整题目</el-button>
            <el-button v-if="round.deletable" type="danger" style="width: 100%" @click="remove">删除{{ name }}</el-button>
          </div>
          <div class="scene-row">
            <span>启用文件 IO</span>
            <el-switch
              :model-value="round.file_io"
              :loading="fileIOSaving"
              @change="(v: string | number | boolean) => toggleFileIO(!!v)"
            />
          </div>
          <div class="scene-row">
            <span>开启成绩表</span>
            <el-switch
              :model-value="round.scoreboard"
              :disabled="flagSaving"
              @change="(v: string | number | boolean) => saveFlag({ scoreboard: !!v })"
            />
          </div>
          <div class="scene-row">
            <span>赛期内不可见</span>
            <el-switch
              :model-value="round.hide_during"
              :disabled="flagSaving"
              @change="(v: string | number | boolean) => saveFlag({ hide_during: !!v })"
            />
          </div>
          <div class="dim scene-note">
            文件 IO 改动只影响之后的提交。关闭「成绩表」后参与者不再看到成绩表，组织者仍可见；
            关闭「赛期内不可见」后参赛者在赛中即可看到状态、标签与通过率（成绩表按开关与发布态显示）。
          </div>
        </el-card>

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

/* 名称卡与题面同列 860 居中，标题与说明左缘对齐 */
.round-body {
  max-width: 860px;
  margin: 0 auto;
}

/* 页面名称 20px（v0.7 design.md；token 在 v0.7 统一调整） */
.big-title {
  font-size: var(--soj-fs-page-title);
  font-weight: var(--soj-fw-emphasis);
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

.progress-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.progress-title {
  font-weight: var(--soj-fw-emphasis);
}

/* 倒计时大字（22/600，强调档） */
.countdown {
  font-size: var(--soj-fs-page-title);
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-color-primary);
}

.center {
  text-align: center;
  margin-top: 6px;
}

.submit-head {
  display: flex;
  justify-content: space-between;
  margin: 14px 0 6px;
  color: var(--el-text-color-regular);
}

.scene-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 10px;
}

.scene-note {
  margin-top: 10px;
  line-height: 1.6;
}
</style>

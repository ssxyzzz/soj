<script setup lang="ts">
// 比赛/考试详情页共享组件（v0.5.4 ① 合并，原 ContestDetailView/ExamDetailView 94% 同构）。
// 差异收敛为：文案（比赛/考试）、路由前缀、加载失败与删除后的回退路由
// （比赛 → /contests；考试 → 班级列表/班级详情）、两个条件块
// （比赛头部的「主题库 · 全员」tag；考试未登录时的参赛建议 alert）。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { authApi, contestApi, examApi, type RoundKind } from '@/api'
import { downloadCsv } from '@/api/download'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import { buildRoundBoard } from '@/components/score/adapters'
import ScoreBoardDialog from '@/components/score/ScoreBoardDialog.vue'
import ScoreSummaryCard from '@/components/score/ScoreSummaryCard.vue'
import type { ScoreBoardView } from '@/components/score/types'
import UserSubmissionsDialog from '@/components/score/UserSubmissionsDialog.vue'
import { useUserStore } from '@/stores/user'
import { fmtDate } from '@/utils/format'
import { MAX_RICHTEXT_BYTES, richtextBytes } from '@/utils/richtext'
import type { ContestDetail, LeaderRow, Submission } from '@/types'

const props = defineProps<{ kind: RoundKind }>()

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

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

const showEditIntro = ref(false)
const editIntro = ref('')
const savingIntro = ref(false)
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

const boardVisible = computed(() => !!round.value && (round.value.is_organizer || round.value.status === 'ended'))

function fmtDur(ms: number): string {
  if (ms <= 0) return '0'
  const s = Math.floor(ms / 1000)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  return (d > 0 ? `${d}天` : '') + `${h}小时${m}分${sec}秒`
}

async function load() {
  try {
    round.value = await api.value.detail(Number(route.params.id))
    if (boardVisible.value && userStore.token) loadLeaders()
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

// 成绩导出 CSV（v0.5.6）：仅组织者可见；服务端现算榜单，列含排名/昵称/各题得分/总分，
// 文件名以服务端 Content-Disposition 为准
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

// 右列成绩摘要已迁到 ScoreSummaryCard（统一成绩表 §2.4）
function openEditIntro() {
  editIntro.value = round.value?.intro_html ?? round.value?.intro ?? ''
  showEditIntro.value = true
}

async function saveIntro() {
  if (!round.value) return
  if (!editIntro.value.trim()) {
    ElMessage.warning(`${name.value}说明不能为空`)
    return
  }
  if (richtextBytes(editIntro.value) > MAX_RICHTEXT_BYTES) {
    ElMessage.error(`${name.value}说明过长（上限 256KB）`)
    return
  }
  savingIntro.value = true
  try {
    await api.value.setIntro(round.value.id, editIntro.value)
    round.value.intro_html = editIntro.value
    ElMessage.success(`${name.value}说明已保存`)
    showEditIntro.value = false
  } finally {
    savingIntro.value = false
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
        <el-card>
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
        </el-card>

        <el-card class="block">
          <el-table stripe v-if="round.problems" :data="round.problems">
            <el-table-column prop="id" label="ID" width="100" />
            <el-table-column label="题目">
              <template #default="{ row }">
                <el-link
                  type="primary"
                  @click="router.push(`${roundBase}/${round!.id}/problems/${row.id}`)"
                >
                  {{ row.title }}
                </el-link>
              </template>
            </el-table-column>
            <el-table-column prop="score" label="满分" width="100" />
          </el-table>
          <el-alert
            v-else
            :title="round.status === 'scheduled' ? `${name}开始前题目隐藏，开始后可见` : '仅本班成员可见题目'"
            type="info"
            :closable="false"
          />
        </el-card>

      <template #aside>
        <el-card v-if="round.is_organizer" class="aside-card">
          <template #header><span>本场场景</span></template>
          <div class="scene-row">
            <span>启用文件 IO</span>
            <el-switch
              :model-value="round.file_io"
              :loading="fileIOSaving"
              @change="(v: string | number | boolean) => toggleFileIO(!!v)"
            />
          </div>
          <div class="dim" style="margin-top: 8px; line-height: 1.6">
            启用后本场所有题按各题文件名对以文件读写方式判题，题面下方展示文件 IO 说明；可随时开关，之后的提交按新设置判。
          </div>
        </el-card>

        <el-card class="aside-card">
          <template #header>
            <span>{{ round.status === 'scheduled' ? '开始倒计时' : round.status === 'running' ? '剩余时间' : '赛程' }}</span>
          </template>
          <div class="countdown">{{ countdownLabel }}</div>
          <el-progress
            :percentage="progressPct"
            :status="round.status === 'ended' ? 'success' : undefined"
            :stroke-width="10"
          />
          <div class="dim center">{{ fmtDate(round.start) }} ~ {{ fmtDate(round.end) }}</div>
        </el-card>

        <ScoreSummaryCard
          class="aside-card"
          :board="scoreBoard"
          :loading="leadersLoading"
          :visible="boardVisible && !!userStore.token"
          :empty-text="scoreEmptyText"
          :my-uid="myUid"
          :can-refresh="boardVisible && !!userStore.token"
          @open="showScoreBoard = true"
          @refresh="loadLeaders"
        />

        <el-card v-if="round.is_organizer || round.deletable" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button v-if="round.is_organizer" type="primary" style="width: 100%" @click="openEditIntro">
              编辑说明
            </el-button>
            <el-button v-if="round.deletable" type="danger" style="width: 100%" @click="remove">
              删除{{ name }}
            </el-button>
          </div>
        </el-card>
      </template>
    </PageShell>

    <el-dialog v-model="showEditIntro" :title="`编辑${name}说明（Markdown / 所见即所得）`" width="1020" top="4vh">
      <MdEditor v-model="editIntro" height="380px" />
      <template #footer>
        <el-button @click="showEditIntro = false">取消</el-button>
        <el-button type="primary" :loading="savingIntro" @click="saveIntro">保存</el-button>
      </template>
    </el-dialog>

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

/* 页头大标题标准档（22/600）：全站层级顶点 */
.big-title {
  font-size: 22px;
  font-weight: 600;
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

/* 倒计时大字（22/600，强调档） */
.countdown {
  font-size: 22px;
  font-weight: 600;
  color: var(--el-color-primary);
  margin-bottom: 10px;
  text-align: center;
}

.center {
  text-align: center;
  margin-top: 6px;
}

.scene-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
</style>

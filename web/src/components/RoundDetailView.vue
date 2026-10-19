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
import SubStatus from '@/components/SubStatus.vue'
import { useUserStore } from '@/stores/user'
import { fmtDate, fmtMem } from '@/utils/format'
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
const tab = ref('problems')
const leaders = ref<LeaderRow[]>([])
const leadersLoading = ref(false)
const myUid = ref<number | null>(null)

// 榜单单元格点看用户提交明细：组织者本人 ∨「查看用户提交明细」权限位
const canInspectSubs = computed(
  () => !!round.value?.is_organizer || userStore.can('submission.view.user'),
)
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
    if (boardVisible.value) loadLeaders()
  } catch {
    // 加载失败回退：比赛回赛事列表；考试回班级列表（考试路由不在主导航）
    router.replace(props.kind === 'exam' ? '/classes' : '/contests')
  } finally {
    loading.value = false
  }
}

async function loadLeaders() {
  leadersLoading.value = true
  try {
    leaders.value = await api.value.leaderboard(Number(route.params.id))
  } catch {
    leaders.value = []
  } finally {
    leadersLoading.value = false
  }
}

// 成绩导出 CSV（v0.5.6）：仅组织者可见；服务端现算榜单，列含排名/用户名/各题得分/总分
//（真实姓名列由 user.realname.view 权限位决定），文件名以服务端 Content-Disposition 为准
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

// 右列成绩摘要：榜单可见时取自己那一行
const mySummary = computed(() => {
  if (myUid.value == null) return null
  return leaders.value.find((r) => r.uid === myUid.value) ?? null
})

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
          <el-tabs v-model="tab">
            <el-tab-pane label="题目" name="problems">
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
            </el-tab-pane>
            <el-tab-pane label="榜单" name="board">
              <el-button v-if="boardVisible" size="small" @click="loadLeaders">刷新榜单</el-button>
              <el-button v-if="round.is_organizer" size="small" :loading="exporting" @click="exportCsv">
                导出 CSV
              </el-button>
              <el-alert
                v-if="round.status === 'running' && !round.is_organizer"
                :title="`OI 赛制：${name}进行中不显示榜单与判题结果`"
                type="info"
                :closable="false"
                class="mt8"
              />
              <el-table stripe v-if="leaders.length" :data="leaders" class="mt8">
                <el-table-column prop="rank" label="排名" width="70" fixed />
                <el-table-column prop="username" label="用户名" min-width="130" fixed />
                <el-table-column prop="nickname" label="昵称" min-width="110" />
                <el-table-column
                  v-for="cp in round.problems"
                  :key="cp.id"
                  min-width="150"
                  align="center"
                >
                  <template #header>
                    <el-link type="primary" @click="router.push(`${roundBase}/${round!.id}/problems/${cp.id}`)">
                      {{ cp.title }}
                    </el-link>
                  </template>
                  <template #default="{ row }">
                    <template v-for="cell in row.problems" :key="cell.pid">
                      <span v-if="cell.pid === cp.id">
                        {{ cell.score }} 分
                        <el-link
                          v-if="cell.tries && canInspectSubs"
                          type="primary"
                          class="cell-tries"
                          @click="openUserSubs(row.uid, row.username, cp.id, cp.title)"
                        >
                          / {{ cell.tries }} 次
                        </el-link>
                        <span v-else-if="cell.tries"> / {{ cell.tries }} 次</span>
                      </span>
                    </template>
                  </template>
                </el-table-column>
                <el-table-column prop="total" label="总分" width="90" fixed />
                <el-table-column label="最后得分提交" width="190">
                  <template #default="{ row }">
                    {{ row.last_scoring_at ? new Date(row.last_scoring_at).toLocaleString('zh-CN', { hour12: false }) : '-' }}
                  </template>
                </el-table-column>
              </el-table>
              <el-empty v-else-if="!leadersLoading" description="暂无榜单数据" :image-size="60" />
            </el-tab-pane>
          </el-tabs>
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

        <el-card v-if="mySummary" class="aside-card">
          <template #header><span>我的成绩</span></template>
          <div class="stat-line"><span>总分</span><b>{{ mySummary.total }}</b></div>
          <div class="stat-line"><span>排名</span><b>第 {{ mySummary.rank }} 名</b></div>
        </el-card>

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

    <!-- 榜单单元格点看的用户提交明细（含代码展开） -->
    <el-dialog v-model="showUserSubs" :title="`提交明细 · ${userSubsTitle}`" width="1020" top="4vh">
      <el-table stripe v-loading="userSubsLoading" :data="userSubs" border>
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="user-subs-detail">
              <pre v-if="row.code" class="mono sub-code">{{ row.code }}</pre>
              <el-table stripe v-if="row.detail?.length || row.error_info" :data="[...(row.detail ?? []), ...(row.error_info ? [{ msg: row.error_info }] : [])]" size="small" border>
                <el-table-column prop="index" label="测试点" width="90" />
                <el-table-column label="结果">
                  <template #default="{ row: c }">
                    <SubStatus v-if="c.status" :status="c.status" />
                  </template>
                </el-table-column>
                <el-table-column label="明细" min-width="150">
                  <template #default="{ row: c }">
                    <span v-if="c.msg" class="case-err">{{ c.msg }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="耗时" width="120">
                  <template #default="{ row: c }">{{ c.time_ms ? `${c.time_ms} ms` : '' }}</template>
                </el-table-column>
                <el-table-column label="内存" width="120">
                  <template #default="{ row: c }">{{ c.memory_kb ? fmtMem(c.memory_kb) : '' }}</template>
                </el-table-column>
              </el-table>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="140">
          <template #default="{ row }">
            <SubStatus :status="row.status" :score="row.score" />
          </template>
        </el-table-column>
        <el-table-column prop="language" label="语言" width="100" />
        <el-table-column label="总耗时" width="100">
          <template #default="{ row }">{{ row.time_ms }} ms</template>
        </el-table-column>
        <el-table-column label="总内存" width="110">
          <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
        </el-table-column>
        <el-table-column label="提交时间">
          <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!userSubsLoading && !userSubs.length" description="该用户在此题没有提交" :image-size="48" />
    </el-dialog>
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

.stat-line {
  display: flex;
  justify-content: space-between;
  padding: 4px 0;
}

.cell-tries {
  vertical-align: baseline;
}

.user-subs-detail {
  padding: 8px 20px 16px;
}

.user-subs-detail .sub-code {
  margin-bottom: 12px;
}

.scene-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
</style>

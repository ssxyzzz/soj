<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { authApi, examApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import { useUserStore } from '@/stores/user'
import { fmtDate } from '@/utils/format'
import { MAX_MARKDOWN_BYTES, markdownBytes } from '@/utils/vditor'
import type { ExamDetail, LeaderRow } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const exam = ref<ExamDetail | null>(null)
const showEditIntro = ref(false)
const editIntro = ref('')
const savingIntro = ref(false)
const loading = ref(true)
const tab = ref('problems')
const leaders = ref<LeaderRow[]>([])
const leadersLoading = ref(false)
const myUid = ref<number | null>(null)
let timer: ReturnType<typeof setInterval> | undefined

const now = ref(Date.now())

const statusLabel = computed(() => {
  const map: Record<string, string> = { running: '进行中', scheduled: '未开始', ended: '已结束' }
  return exam.value ? map[exam.value.status] : ''
})

const countdownLabel = computed(() => {
  if (!exam.value) return ''
  if (exam.value.status === 'scheduled') return fmtDur(new Date(exam.value.start).getTime() - now.value)
  if (exam.value.status === 'running') return fmtDur(new Date(exam.value.end).getTime() - now.value)
  return '已结束'
})

const progressPct = computed(() => {
  const e = exam.value
  if (!e) return 0
  const start = new Date(e.start).getTime()
  const end = new Date(e.end).getTime()
  if (e.status === 'scheduled') return 0
  if (e.status === 'ended') return 100
  const total = end - start
  if (total <= 0) return 100
  return Math.min(100, Math.max(0, Math.round(((now.value - start) / total) * 100)))
})

const boardVisible = computed(() => !!exam.value && (exam.value.is_organizer || exam.value.status === 'ended'))

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
    exam.value = await examApi.detail(Number(route.params.id))
    if (boardVisible.value) loadLeaders()
  } catch {
    router.replace('/classes')
  } finally {
    loading.value = false
  }
}

async function loadLeaders() {
  leadersLoading.value = true
  try {
    leaders.value = await examApi.leaderboard(Number(route.params.id))
  } catch {
    leaders.value = []
  } finally {
    leadersLoading.value = false
  }
}

const mySummary = computed(() => {
  if (myUid.value == null) return null
  return leaders.value.find((r) => r.uid === myUid.value) ?? null
})

function openEditIntro() {
  editIntro.value = exam.value?.intro ?? ''
  showEditIntro.value = true
}

async function saveIntro() {
  if (!exam.value) return
  if (!editIntro.value.trim()) {
    ElMessage.warning('考试说明不能为空')
    return
  }
  if (markdownBytes(editIntro.value) > MAX_MARKDOWN_BYTES) {
    ElMessage.error('考试说明过长（上限 256KB）')
    return
  }
  savingIntro.value = true
  try {
    await examApi.setIntro(exam.value.id, editIntro.value)
    exam.value.intro = editIntro.value
    ElMessage.success('考试说明已保存')
    showEditIntro.value = false
  } finally {
    savingIntro.value = false
  }
}

async function remove() {
  if (!exam.value) return
  try {
    await ElMessageBox.confirm(
      `确定删除考试「${exam.value.title}」吗？删除后不可恢复！`,
      '⚠️ 危险操作：确认删除',
      { type: 'error', confirmButtonText: '确认删除', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  await examApi.remove(exam.value.id)
  ElMessage.success('已删除')
  router.push(`/classes/${exam.value.class_id}`)
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
    <PageShell v-if="exam">
      <div class="page">
        <el-card>
          <div class="title-row">
            <span class="big-title">{{ exam.title }}</span>
            <el-tag :type="exam.status === 'running' ? 'success' : exam.status === 'scheduled' ? 'warning' : 'info'">
              {{ statusLabel }}
            </el-tag>
          </div>
          <div v-if="exam.intro" class="intro"><MdView :content="exam.intro" /></div>
          <div class="meta">
            <el-tag size="small" type="info">{{ fmtDate(exam.start) }} ~ {{ fmtDate(exam.end) }}</el-tag>
            <el-tag v-if="exam.hidden_until" size="small" type="info">
              赛后隐藏至 {{ fmtDate(exam.hidden_until) }}
            </el-tag>
          </div>
          <el-alert
            v-if="!exam.participant && !userStore.token"
            title="登录并加入对应班级后才能参加本场考试"
            type="info"
            :closable="false"
            class="mt8"
          />
        </el-card>

        <el-card class="block">
          <el-tabs v-model="tab">
            <el-tab-pane label="题目" name="problems">
              <el-table v-if="exam.problems" :data="exam.problems">
                <el-table-column prop="id" label="ID" width="100" />
                <el-table-column label="题目">
                  <template #default="{ row }">
                    <el-link
                      type="primary"
                      @click="router.push(`/exams/${exam!.id}/problems/${row.id}`)"
                    >
                      {{ row.title }}
                    </el-link>
                  </template>
                </el-table-column>
                <el-table-column prop="score" label="满分" width="100" />
              </el-table>
              <el-alert
                v-else
                :title="exam.status === 'scheduled' ? '考试开始前题目隐藏，开始后可见' : '仅本班成员可见题目'"
                type="info"
                :closable="false"
              />
            </el-tab-pane>
            <el-tab-pane label="榜单" name="board">
              <el-button v-if="boardVisible" size="small" @click="loadLeaders">刷新榜单</el-button>
              <el-alert
                v-if="exam.status === 'running' && !exam.is_organizer"
                title="考试进行中不显示榜单与判题结果"
                type="info"
                :closable="false"
                class="mt8"
              />
              <el-table v-if="leaders.length" :data="leaders" class="mt8">
                <el-table-column prop="rank" label="排名" width="70" fixed />
                <el-table-column prop="username" label="用户名" min-width="130" fixed />
                <el-table-column prop="nickname" label="昵称" min-width="110" />
                <el-table-column
                  v-for="cp in exam.problems"
                  :key="cp.id"
                  :label="'P' + cp.id"
                  width="100"
                  align="center"
                >
                  <template #default="{ row }">
                    <template v-for="cell in row.problems" :key="cell.pid">
                      <span v-if="cell.pid === cp.id">
                        {{ cell.score }} 分<span v-if="cell.tries"> / {{ cell.tries }} 次</span>
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
      </div>

      <template #aside>
        <el-card class="aside-card">
          <template #header>
            <span>{{ exam.status === 'scheduled' ? '开始倒计时' : exam.status === 'running' ? '剩余时间' : '赛程' }}</span>
          </template>
          <div class="countdown">{{ countdownLabel }}</div>
          <el-progress
            :percentage="progressPct"
            :status="exam.status === 'ended' ? 'success' : undefined"
            :stroke-width="10"
          />
          <div class="dim center">{{ fmtDate(exam.start) }} ~ {{ fmtDate(exam.end) }}</div>
        </el-card>

        <el-card v-if="mySummary" class="aside-card">
          <template #header><span>我的成绩</span></template>
          <div class="stat-line"><span>总分</span><b>{{ mySummary.total }}</b></div>
          <div class="stat-line"><span>排名</span><b>第 {{ mySummary.rank }} 名</b></div>
        </el-card>

        <el-card v-if="exam.is_organizer || exam.deletable" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button v-if="exam.is_organizer" type="primary" plain style="width: 100%" @click="openEditIntro">
              编辑说明
            </el-button>
            <el-button v-if="exam.deletable" type="danger" plain style="width: 100%" @click="remove">
              删除考试
            </el-button>
          </div>
        </el-card>
      </template>
    </PageShell>

    <el-dialog v-model="showEditIntro" title="编辑考试说明（Markdown / 所见即所得）" width="860" top="4vh">
      <MdEditor v-model="editIntro" height="380px" />
      <template #footer>
        <el-button @click="showEditIntro = false">取消</el-button>
        <el-button type="primary" :loading="savingIntro" @click="saveIntro">保存</el-button>
      </template>
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

.big-title {
  font-size: 22px;
  font-weight: 700;
}

.intro {
  margin: 12px 0 4px;
  color: #606266;
}

.meta {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 10px;
}

.mt8 {
  margin-top: 8px;
}

.block {
  margin-top: 16px;
}

.aside-card + .aside-card {
  margin-top: 16px;
}

.countdown {
  font-size: 22px;
  font-weight: 700;
  color: #409eff;
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
  font-size: 14px;
}

.entry-actions {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.dim {
  color: #909399;
  font-size: 13px;
}
</style>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { examApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import { useUserStore } from '@/stores/user'
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
const leadersLoaded = false
let timer: ReturnType<typeof setInterval> | undefined

const now = ref(Date.now())
const countdown = computed(() => {
  if (!exam.value) return ''
  const start = new Date(exam.value.start).getTime()
  const end = new Date(exam.value.end).getTime()
  if (exam.value.status === 'scheduled') return fmtDur(start - now.value)
  if (exam.value.status === 'running') return fmtDur(end - now.value)
  return '已结束'
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

const statusLabel = computed(() => {
  const map: Record<string, string> = { running: '进行中', scheduled: '未开始', ended: '已结束' }
  return exam.value ? map[exam.value.status] : ''
})

async function load() {
  try {
    exam.value = await examApi.detail(Number(route.params.id))
    if (exam.value.status === 'ended' || exam.value.is_organizer) {
      loadLeaders()
    }
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
  await examApi.remove(exam.value.id)
  ElMessage.success('已删除')
  router.push(`/classes/${exam.value.class_id}`)
}

onMounted(() => {
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
  <div v-loading="loading" class="page">
    <template v-if="exam">
      <el-card>
        <template #header>
          <div class="head">
            <div>
              <span class="title">{{ exam.title }}</span>
              <el-tag :type="exam.status === 'running' ? 'success' : exam.status === 'scheduled' ? 'warning' : 'info'" class="ml8">
                {{ statusLabel }}
              </el-tag>
              
            </div>
            <el-button v-if="exam.deletable" size="small" type="danger" plain @click="remove">
              删除比赛
            </el-button>
          </div>
        </template>
        <div v-if="exam.intro" class="intro"><MdView :content="exam.intro" /></div>
        <div class="meta">
          <span>
            {{ exam.status === 'scheduled' ? '开始倒计时：' : exam.status === 'running' ? '剩余时间：' : '' }}
            <b>{{ countdown }}</b>
          </span>
          <span class="dim">
            {{ new Date(exam.start).toLocaleString('zh-CN', { hour12: false }) }} ~
            {{ new Date(exam.end).toLocaleString('zh-CN', { hour12: false }) }}
          </span>
          <span v-if="exam.hidden_until" class="dim">
            （赛后隐藏至 {{ new Date(exam.hidden_until).toLocaleString('zh-CN', { hour12: false }) }}）
          </span>
        </div>
        <el-alert
          v-if="!exam.participant && !userStore.token"
          title="登录并加入对应班级后才能参加本场比赛"
          type="info"
          :closable="false"
          class="ml8 mt8"
        />
      </el-card>

      <el-dialog v-model="showEditIntro" title="编辑考试说明（Markdown / 所见即所得）" width="860" top="4vh">
        <MdEditor v-model="editIntro" height="380px" />
        <template #footer>
          <el-button @click="showEditIntro = false">取消</el-button>
          <el-button type="primary" :loading="savingIntro" @click="saveIntro">保存</el-button>
        </template>
      </el-dialog>

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
            <el-button v-if="exam.is_organizer || exam.status === 'ended'" size="small" @click="loadLeaders">
              刷新榜单
            </el-button>
            <el-alert
              v-if="exam.status === 'running' && !exam.is_organizer"
              title="考试进行中不显示榜单与判题结果"
              type="info"
              :closable="false"
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
    </template>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.title {
  font-weight: 600;
  font-size: 17px;
}

.ml8 {
  margin-left: 8px;
}

.mt8 {
  margin-top: 8px;
}

.intro {
  margin: 0 0 8px;
  color: #606266;
}

.meta {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  align-items: center;
}

.dim {
  color: #909399;
  font-size: 13px;
}

.block {
  margin-top: 16px;
}

.cell {
  margin-right: 12px;
  font-size: 13px;
}
</style>

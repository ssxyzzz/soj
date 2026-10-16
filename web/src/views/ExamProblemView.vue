<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { examApi, problemApi } from '@/api'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import { fmtDate, fmtMem, statusStyle } from '@/utils/format'
import type { ContestMySubmission, ExamDetail, ProblemDetail } from '@/types'

const route = useRoute()
const router = useRouter()

const exam = ref<ExamDetail | null>(null)
const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
const mine = ref<ContestMySubmission[]>([])
const revealed = ref(false)

const showSubmit = ref(false)
const showWorkbench = ref(false)
const submitting = ref(false)
const expandedMine = ref<number[]>([])

const examId = computed(() => Number(route.params.id))
const pid = computed(() => Number(route.params.pid))
const running = computed(() => exam.value?.status === 'running')
const statusText = computed(() => {
  switch (exam.value?.status) {
    case 'running': return '进行中'
    case 'scheduled': return '未开始'
    default: return '已结束'
  }
})

// 在线编程左栏题面：题面本身已含 Markdown 样例章节，直接渲染
const workbenchStatement = computed(() => problem.value?.description ?? '')

async function load() {
  loading.value = true
  try {
    exam.value = await examApi.detail(examId.value)
    problem.value = await problemApi.detail(pid.value)
    await loadMine()
  } catch {
    router.replace('/classes')
  } finally {
    loading.value = false
  }
}

async function loadMine() {
  try {
    const r = await examApi.mySubmissions(examId.value)
    mine.value = r.list.filter((s) => s.pid === pid.value)
    revealed.value = r.revealed
  } catch {
    mine.value = []
  }
}

function goProblem(targetPid: number) {
  if (targetPid === pid.value) return
  router.push(`/exams/${examId.value}/problems/${targetPid}`)
}

function toggleExpand(id: number) {
  const i = expandedMine.value.indexOf(id)
  if (i >= 0) expandedMine.value.splice(i, 1)
  else expandedMine.value.push(id)
}

async function handleSubmit(_lang: string, code: string) {
  submitting.value = true
  try {
    const r = await examApi.submit(examId.value, pid.value, _lang, code)
    ElMessage.success('已提交')
    showSubmit.value = false
    showWorkbench.value = false
    await loadMine()
    // 考时不轮询结果（不出分）；已公开时轮询刷新
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
    if (s && s.status && s.status !== 'Pending' && s.status !== 'Judging') {
      if (pollTimer) clearInterval(pollTimer)
    }
  }, 1500)
}

onMounted(load)

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<template>
  <div v-loading="loading">
    <PageShell v-if="exam && problem">
      <template #standalone>
        <el-card class="standalone-card" :body-style="{ padding: '10px 20px' }">
          <div class="sa-time">
            <el-link type="primary" @click="router.push(`/exams/${exam.id}`)">{{ exam.title }}</el-link>
            <el-tag size="small" :type="running ? 'success' : 'info'">{{ statusText }}</el-tag>
            <span class="dim">考试时间：{{ fmtDate(exam.start) }} ~ {{ fmtDate(exam.end) }}</span>
            <span v-if="!revealed" class="dim">考时只记录提交次数，不出分</span>
          </div>
          <div class="sa-switch">
            <el-button
              v-for="(p, i) in exam.problems"
              :key="p.id"
              :type="p.id === pid ? 'primary' : 'default'"
              circle
              size="small"
              class="switch-btn"
              @click="goProblem(p.id)"
            >
              {{ i + 1 }}
            </el-button>
            <el-link class="sa-back" type="primary" @click="router.push(`/exams/${exam.id}`)">返回考试</el-link>
          </div>
        </el-card>
      </template>

      <el-card>
        <div class="title-row">
          <span class="big-title">#{{ problem.id }} {{ problem.title }}</span>
        </div>
        <div class="info-row">
          <el-tag size="small" type="info">时间限制 {{ problem.time_limit_ms }} ms</el-tag>
          <el-tag size="small" type="info">内存限制 {{ fmtMem(problem.memory_limit_kb) }}</el-tag>
        </div>
        <MdView :content="problem.description" />
      </el-card>

      <template #aside>
        <el-card class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" :disabled="!running" @click="showSubmit = true">
              提交
            </el-button>
            <el-button plain style="width: 100%" :disabled="!running" @click="showWorkbench = true">
              在线编程
            </el-button>
            <div v-if="!running" class="dim center">考试未在进行中</div>
          </div>
        </el-card>

        <el-card class="aside-card">
          <template #header>
            <span>我的提交（{{ mine.length }}）{{ revealed ? '' : ' · 考时仅记录次数' }}</span>
          </template>
          <div v-for="s in mine" :key="s.id" class="sub-row" @click="toggleExpand(s.id)">
            <div class="sub-line">
              <span class="dim">#{{ s.id }}</span>
              <template v-if="revealed && s.status">
                <el-tag size="small" :type="statusStyle(s.status).type">{{ statusStyle(s.status).label }}</el-tag>
                <span v-if="s.score != null">{{ s.score }} 分</span>
                <span v-if="s.time_ms != null" class="dim">{{ s.time_ms }} ms</span>
              </template>
              <span v-else class="dim">已记录</span>
              <span class="dim sub-time">{{ fmtDate(s.created_at) }}</span>
            </div>
            <pre v-if="revealed && expandedMine.includes(s.id) && s.code" class="mono sub-code">{{ s.code }}</pre>
          </div>
          <el-empty v-if="!mine.length" description="还没有提交" :image-size="40" />
        </el-card>
      </template>
    </PageShell>

    <template v-if="exam && problem">
      <SubmitDialog
        v-model="showSubmit"
        :title="`#${problem.id} ${problem.title}`"
        :submitting="submitting"
        @submit="handleSubmit"
      />

      <CodeWorkbench
        v-model="showWorkbench"
        :title="`#${problem.id} ${problem.title}`"
        :statement="workbenchStatement"
        :submitting="submitting"
        @submit="handleSubmit"
      />
    </template>
  </div>
</template>

<style scoped>
.standalone-card {
  margin-bottom: 0;
}

.sa-time {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  font-size: 14px;
}

.sa-switch {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}

.switch-btn {
  font-weight: 600;
}

.sa-back {
  margin-left: auto;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.big-title {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.4;
}

.info-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 10px 0 14px;
}

.aside-card + .aside-card {
  margin-top: 16px;
}

.entry-actions {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.center {
  text-align: center;
}

.sub-row {
  padding: 6px 4px;
  border-bottom: 1px dashed #ebeef5;
  cursor: pointer;
}

.sub-row:hover {
  background: #f5f7fa;
}

.sub-line {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  flex-wrap: wrap;
}

.sub-time {
  margin-left: auto;
  font-size: 12px;
}

.sub-code {
  margin-top: 6px;
  padding: 8px 10px;
  background: #f8f8f8;
  border-radius: 4px;
  max-height: 280px;
  overflow: auto;
  font-size: 12px;
  cursor: auto;
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
}

.dim {
  color: #909399;
  font-size: 13px;
}
</style>

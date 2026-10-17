<script setup lang="ts">
// 比赛/考试做题页共享组件（v0.5e ① 合并，原 ContestProblemView/ExamProblemView 94→97% 同构）。
// 差异仅三处：文案（比赛/考试）、详情页路由前缀（/contests|/exams）、加载失败回退
// （比赛回赛事页、考试回班级列表）——全部由 kind 派生。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { contestApi, examApi, problemApi, type RoundKind } from '@/api'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import { fmtDate, fmtMem, statusDesc, statusStyle } from '@/utils/format'
import type { ContestDetail, ContestMySubmission, ProblemDetail } from '@/types'

const props = defineProps<{ kind: RoundKind }>()

const route = useRoute()
const router = useRouter()

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
const submitting = ref(false)
const expandedMine = ref<number[]>([])

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

// 在线编程左栏题面：题面本身已含 Markdown 样例章节，直接渲染；
// 本场启用文件 IO 时追加文件 IO 提示（学生写代码时需要随时对照文件名）
const workbenchStatement = computed(() => {
  const base = problem.value?.description ?? ''
  if (round.value?.file_io) {
    const inName = problem.value?.file_io_input || 'problem.in'
    const outName = problem.value?.file_io_output || 'problem.out'
    return `${base}\n\n> ⚠️ 本题请以文件 IO 方式提交，输入输出文件名是 ${inName} / ${outName}\n`
  }
  return base
})

// 加载失败回退：比赛回赛事页；考试回班级列表（考试路由不在主导航）
function fallbackRoute(): string {
  return props.kind === 'exam' ? '/classes' : `${roundBase.value}/${roundId.value}`
}

async function load() {
  loading.value = true
  try {
    round.value = await api.value.detail(roundId.value)
    problem.value = await problemApi.detail(pid.value)
    await loadMine()
  } catch {
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

function toggleExpand(id: number) {
  const i = expandedMine.value.indexOf(id)
  if (i >= 0) expandedMine.value.splice(i, 1)
  else expandedMine.value.push(id)
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
    if (s && s.status && s.status !== 'Pending' && s.status !== 'Judging') {
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
          <div class="sa-time">
            <el-link type="primary" @click="router.push(`${roundBase}/${round.id}`)">{{ round.title }}</el-link>
            <el-tag size="small" :type="running ? 'success' : 'info'">{{ statusText }}</el-tag>
            <span class="dim">{{ name }}时间：{{ fmtDate(round.start) }} ~ {{ fmtDate(round.end) }}</span>
            <span v-if="!revealed" class="dim">OI 赛制：赛时只记录提交次数，不出分</span>
          </div>
          <div class="sa-switch">
            <el-button
              v-for="(p, i) in round.problems"
              :key="p.id"
              :type="p.id === pid ? 'primary' : 'default'"
              circle
              size="small"
              class="switch-btn"
              @click="goProblem(p.id)"
            >
              {{ i + 1 }}
            </el-button>
            <el-link class="sa-back" type="primary" @click="router.push(`${roundBase}/${round.id}`)">返回{{ name }}</el-link>
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
        <el-alert
          v-if="round.file_io"
          type="error"
          :closable="false"
          class="fileio-card"
          show-icon
          title="本题请以文件 IO 方式提交"
          :description="`输入输出文件名是 ${problem.file_io_input || 'problem.in'} / ${problem.file_io_output || 'problem.out'}`"
        />
        <MdView :content="problem.description" />
      </el-card>
      <template #aside>
        <el-card class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" plain style="width: 100%" :disabled="!running" @click="showSubmit = true">
              提交
            </el-button>
            <el-button type="success" plain style="width: 100%" :disabled="!running" @click="showWorkbench = true">
              在线编程
            </el-button>
            <div v-if="!running" class="dim center">{{ name }}未在进行中</div>
          </div>
        </el-card>

        <el-card class="aside-card">
          <template #header>
            <span>我的提交（{{ mine.length }}）{{ revealed ? '' : ' · 赛时仅记录次数' }}</span>
          </template>
          <div v-for="s in mine" :key="s.id" class="sub-row" @click="toggleExpand(s.id)">
            <div class="sub-line">
              <span class="dim">#{{ s.id }}</span>
              <template v-if="revealed && s.status">
                <el-tooltip v-if="statusDesc(s.status)" placement="top" effect="light">
                  <template #content>
                    <div class="status-tip">
                      <div>{{ statusDesc(s.status)!.meaning }}</div>
                      <div v-if="statusDesc(s.status)!.cause">常见原因：{{ statusDesc(s.status)!.cause }}</div>
                    </div>
                  </template>
                  <el-tag size="small" :type="statusStyle(s.status).type" class="status-tag">{{ statusStyle(s.status).label }}</el-tag>
                </el-tooltip>
                <el-tag v-else size="small" :type="statusStyle(s.status).type">{{ statusStyle(s.status).label }}</el-tag>
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

.fileio-card {
  margin-top: 16px;
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
  font-size: 15px;
  flex-wrap: wrap;
}

.sub-time {
  margin-left: auto;
  font-size: 15px;
}

.sub-code {
  margin-top: 6px;
  padding: 8px 10px;
  background: #f8f8f8;
  border-radius: 4px;
  max-height: 280px;
  overflow: auto;
  font-size: 15px;
  cursor: auto;
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
}
</style>

<script setup lang="ts">
// 比赛/考试做题页共享组件（v0.5.4 ① 合并，原 ContestProblemView/ExamProblemView 94→97% 同构）。
// 差异仅三处：文案（比赛/考试）、详情页路由前缀（/contests|/exams）、加载失败回退
// （比赛回赛事页、考试回班级列表）——全部由 kind 派生。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { contestApi, examApi, problemApi, type RoundKind } from '@/api'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import SubStatus from '@/components/SubStatus.vue'
import PageShell from '@/components/PageShell.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import SolutionsDialog from '@/components/problem/SolutionsDialog.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import { fmtDate, statusDesc } from '@/utils/format'
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

      <ProblemStatement :problem="problem" :file-io="round.file_io" />
      <template #aside>
        <el-card class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" plain style="width: 100%" :disabled="!running" @click="showSubmit = true">
              提交
            </el-button>
            <el-button type="success" plain style="width: 100%" :disabled="!running" @click="showWorkbench = true">
              在线编程
            </el-button>
            <el-tooltip
              v-if="revealed"
              placement="top"
              :content="problem.can_view_solutions ? '查看本题题解' : '该题拿到满分后才能查看题解'"
            >
              <el-button type="warning" plain style="width: 100%" @click="openSolutions">题解</el-button>
            </el-tooltip>
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
                  <SubStatus :status="s.status" :score="s.score" />
                </el-tooltip>
                <SubStatus v-else :status="s.status" :score="s.score" />
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
        :problem="problem"
        :file-io="round.file_io"
        :submitting="submitting"
        @submit="handleSubmit"
      />

      <SolutionsDialog v-model="showSolutions" :problem-id="pid" />
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

.sa-back {
  margin-left: auto;
}

.center {
  text-align: center;
}

.sub-row {
  padding: 6px 4px;
  border-bottom: 1px dashed var(--el-border-color-lighter);
  cursor: pointer;
}

.sub-row:hover {
  background: var(--el-fill-color-light);
}

.sub-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.sub-time {
  margin-left: auto;
}

/* 列表行内的代码块：与上一行留间距、不继承行的手型光标（其余走全局 .sub-code/.mono） */
.sub-row .sub-code {
  margin-top: 6px;
  cursor: auto;
}
</style>

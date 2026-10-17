<script setup lang="ts">
// 题目详情（v0.5e ② 拆分后主视图）：题面/锁定指引/隐藏开关/右列入口卡 + 提交链路；
// 题解、我的提交、他人代码、编辑题目四个弹窗已拆至 components/problem/*（自治拉数据）。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { problemApi, submissionApi } from '@/api'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import EditProblemDialog from '@/components/problem/EditProblemDialog.vue'
import MySubmissionsDialog from '@/components/problem/MySubmissionsDialog.vue'
import OthersCodeDialog from '@/components/problem/OthersCodeDialog.vue'
import SolutionsDialog from '@/components/problem/SolutionsDialog.vue'
import { useUserStore } from '@/stores/user'
import { fmtMem, statusStyle } from '@/utils/format'
import type { ProblemDetail } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
let timer: ReturnType<typeof setInterval> | undefined

// 右列：提交 / 在线编程 / 题解 / 我的提交 / 他人代码 五个入口，内容全部进弹窗
const showSubmit = ref(false)
const showWorkbench = ref(false)
const submitting = ref(false)
const showMine = ref(false)
const showOthers = ref(false)
const showSolutions = ref(false)
const showEdit = ref(false)
const mineDialogRef = ref<InstanceType<typeof MySubmissionsDialog>>()

const lockedRunning = computed(() => !!problem.value?.locked)

const lockRoute = computed(() => {
  const p = problem.value
  if (!p) return ''
  return p.lock_kind === 'exam' && p.exam_id ? `/exams/${p.exam_id}` : `/contests/${p.contest_id}`
})
const lockWhat = computed(() => (problem.value?.lock_kind === 'exam' ? '考试' : '比赛'))
const canSetHidden = computed(() => !!problem.value?.can_set_hidden)
const submitDisabled = computed(() => !userStore.token || lockedRunning.value)
const submitDisabledReason = computed(() => {
  if (lockedRunning.value) return `${lockWhat.value}期间请从${lockWhat.value}页面提交`
  if (!userStore.token) return '请先登录'
  return ''
})

// 在线编程左栏题面：题面本身已含 Markdown 样例章节，直接渲染
const workbenchStatement = computed(() => problem.value?.description ?? '')

async function load() {
  loading.value = true
  try {
    problem.value = await problemApi.detail(Number(route.params.id))
  } catch {
    router.replace('/problems')
  } finally {
    loading.value = false
  }
}

function openSubmitEntry() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  showSubmit.value = true
}

function openWorkbenchEntry() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  showWorkbench.value = true
}

function openSolutions() {
  if (!problem.value?.can_view_solutions) {
    ElMessage.warning('该题拿到满分后才能查看题解（老师群体不受限）')
    return
  }
  showSolutions.value = true
}

function openMine() {
  showMine.value = true
}

function openOthers() {
  if (!problem.value?.can_view_others) {
    ElMessage.warning('该题拿到满分后可查看他人的代码（老师群体不受限）')
    return
  }
  showOthers.value = true
}

async function handleSubmit(lang: string, code: string) {
  if (!problem.value) return
  submitting.value = true
  try {
    const r = await submissionApi.create(problem.value.id, lang, code)
    ElMessage.success(`已提交 #${r.id}，正在判题`)
    showSubmit.value = false
    showWorkbench.value = false
    mineDialogRef.value?.refresh(1)
    startPolling(r.id)
  } catch {
    // 错误提示已由 axios 拦截器统一弹出
  } finally {
    submitting.value = false
  }
}

function startPolling(id: number) {
  stopPolling()
  timer = setInterval(async () => {
    try {
      const s = await submissionApi.get(id)
      if (s.status !== 'Pending' && s.status !== 'Judging') {
        stopPolling()
        mineDialogRef.value?.refresh()
        const st = statusStyle(s.status)
        ElMessage.success(`#${id} 判题完成：${st.label}${s.score != null ? ` · ${s.score} 分` : ''}`)
      }
    } catch {
      stopPolling()
    }
  }, 1200)
}

function stopPolling() {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

async function toggleHidden() {
  if (!problem.value) return
  const target = !problem.value.hidden
  await problemApi.setHidden(problem.value.id, target)
  problem.value.hidden = target
  ElMessage.success(target ? '已对学生隐藏（default 不可见不可交）' : '已恢复可见')
}

onMounted(load)

onUnmounted(stopPolling)
</script>

<template>
  <div v-loading="loading">
    <template v-if="problem">
      <PageShell>
        <el-alert
          v-if="problem.locked"
          :title="`该题正在${lockWhat}中`"
          type="warning"
          :closable="false"
          class="mb8"
        >
          <template #default>
            <div class="lock-bar">
              <span>{{ lockWhat }}期间只能从{{ lockWhat }}页面提交（{{ problem.contest_title ?? problem.exam_title ?? '' }}）</span>
              <el-button type="warning" size="small" plain @click="router.push(lockRoute)">前往{{ lockWhat }}</el-button>
            </div>
          </template>
        </el-alert>

        <el-card>
          <div class="title-row">
            <span class="big-title">#{{ problem.id }} {{ problem.title }}</span>
            <el-tag v-if="problem.hidden" type="danger">主题库隐藏</el-tag>
            <el-button
              v-if="canSetHidden"
              size="small"
              :type="problem.hidden ? 'warning' : 'default'"
              class="hidden-btn"
              @click="toggleHidden"
            >
              隐藏
            </el-button>
          </div>
          <div class="info-row">
            <el-tag type="success">时间限制 {{ problem.time_limit_ms }} ms</el-tag>
            <el-tag type="success">内存限制 {{ fmtMem(problem.memory_limit_kb) }}</el-tag>
          </div>
          <MdView :content="problem.description" />
        </el-card>

        <template #aside>
        <el-card v-if="problem.can_edit_statement" class="aside-card">
          <template #header><span>管理</span></template>
          <el-button type="primary" style="width: 100%" @click="showEdit = true">编辑题目</el-button>
        </el-card>

        <el-card class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" plain style="width: 100%" :disabled="submitDisabled" @click="openSubmitEntry">
              提交
            </el-button>
            <el-button type="success" plain style="width: 100%" :disabled="submitDisabled" @click="openWorkbenchEntry">
              在线编程
            </el-button>
            <el-button type="warning" plain style="width: 100%" @click="openSolutions">题解</el-button>
            <el-button v-if="userStore.token" plain style="width: 100%" @click="openMine">我的提交</el-button>
            <el-button plain style="width: 100%" @click="openOthers">他人代码</el-button>
            <div v-if="submitDisabledReason" class="dim center">{{ submitDisabledReason }}</div>
          </div>
        </el-card>
      </template>
    </PageShell>

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

    <SolutionsDialog v-model="showSolutions" :problem-id="problem.id" />

    <MySubmissionsDialog ref="mineDialogRef" v-model="showMine" :problem-id="problem.id" />

    <OthersCodeDialog v-model="showOthers" :problem-id="problem.id" :problem-title="problem.title" />

    <EditProblemDialog v-model="showEdit" :problem="problem" @saved="load" />
  </template>
  </div>
</template>

<style scoped>
.lock-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
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
</style>

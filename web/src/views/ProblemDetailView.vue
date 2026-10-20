<script setup lang="ts">
// 题目详情（v0.5.4 ② 拆分后主视图）：题面/锁定指引/隐藏开关/右列入口卡 + 提交链路；
// 题解、我的提交、所有代码三个弹窗拆至 components/problem/*（自治拉数据）；编辑题目为独立路由页。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { problemApi, submissionApi } from '@/api'
import { judgeQueueApi } from '@/api/judgeQueue'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import PageShell from '@/components/PageShell.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import MySubmissionsDialog from '@/components/problem/MySubmissionsDialog.vue'
import AllCodesDialog from '@/components/problem/AllCodesDialog.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import SolutionsDialog from '@/components/problem/SolutionsDialog.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { useUserStore } from '@/stores/user'
import { isQueueStatus, statusStyle } from '@/utils/format'
import type { ProblemDetail } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const tagStore = useTagTreeStore()
tagStore.ensure() // 标签名映射异步就绪，信息卡响应式跟随

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
let timer: ReturnType<typeof setInterval> | undefined

// 右列：提交 / 在线编程 / 题解 / 我的提交 / 所有代码 五个入口，内容全部进弹窗
const showSubmit = ref(false)
const showWorkbench = ref(false)
const submitting = ref(false)
const showMine = ref(false)
const showOthers = ref(false)
const showSolutions = ref(false)
const mineDialogRef = ref<InstanceType<typeof MySubmissionsDialog>>()

const lockedRunning = computed(() => !!problem.value?.locked)

const lockRoute = computed(() => {
  const p = problem.value
  if (!p) return ''
  return p.lock_kind === 'exam' && p.exam_id ? `/exams/${p.exam_id}` : `/contests/${p.contest_id}`
})
const lockWhat = computed(() => (problem.value?.lock_kind === 'exam' ? '考试' : '比赛'))
const canSetHidden = computed(() => !!problem.value?.can_set_hidden)
const submitDisabled = computed(() => !userStore.token || lockedRunning.value || problem.value?.has_cases === false)

// 批量重测（v0.7.1）：本题全部已出结果的提交低优先级重排队，不挤占学生新提交
const rejudgeAllRunning = ref(false)
async function rejudgeAll() {
  const p = problem.value
  if (!p || rejudgeAllRunning.value) return
  try {
    await ElMessageBox.confirm(
      `将把本题所有已出结果的提交重新评测（低优先级，不影响学生新提交的评测顺序），确定？`,
      '批量重测',
      { type: 'warning', confirmButtonText: '批量重测', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  rejudgeAllRunning.value = true
  try {
    const r = await judgeQueueApi.rejudgeProblem(p.id)
    ElMessage.success(r.count > 0 ? `已加入重测队列：${r.count} 条` : '本题没有可重测的提交')
  } finally {
    rejudgeAllRunning.value = false
  }
}
const submitDisabledReason = computed(() => {
  if (problem.value?.has_cases === false) return '本题暂无数据，无法评测。请联系管理员上传数据'
  if (lockedRunning.value) return `${lockWhat.value}期间请从${lockWhat.value}页面提交`
  if (!userStore.token) return '请先登录'
  return ''
})

// 题目信息卡（v0.5.5）：统计 + 标签；比赛/考试做题页（RoundProblemView）为独立视图，天然无此卡
const tagIds = computed(() => problem.value?.tags ?? [])

// 标签浮层：默认只显示「显示标签」入口。悬停即展、移开即收；点击则锁定，
// 移开后保留 5 秒再收起（再点一次立即收起）。
const tagsVisible = ref(false)
const tagsLatched = ref(false)
let tagsTimer: ReturnType<typeof setTimeout> | undefined

function openTags() {
  if (tagsTimer) {
    clearTimeout(tagsTimer)
    tagsTimer = undefined
  }
  tagsVisible.value = true
}

function scheduleCloseTags() {
  if (tagsTimer) clearTimeout(tagsTimer)
  if (tagsLatched.value) {
    tagsTimer = setTimeout(() => {
      tagsVisible.value = false
      tagsLatched.value = false
    }, 5000)
  } else {
    tagsVisible.value = false
  }
}

function toggleTags() {
  if (tagsLatched.value) {
    tagsLatched.value = false
    if (tagsTimer) clearTimeout(tagsTimer)
    tagsVisible.value = false
  } else {
    tagsLatched.value = true
    openTags()
  }
}

const acceptRate = computed(() => {
  const p = problem.value
  if (!p?.submit_count) return '—'
  const pct = ((p.accepted_count ?? 0) / p.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
})

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
    ElMessage.warning('该题拿到满分后才能查看题解')
    return
  }
  showSolutions.value = true
}

function openMine() {
  showMine.value = true
}

function openOthers() {
  if (!problem.value?.can_view_others) {
    ElMessage.warning('该题拿到满分后可查看他人的代码')
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
      if (!isQueueStatus(s.status)) {
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

        <ProblemStatement :problem="problem">
          <template #extra>
            <el-tag v-if="problem.hidden" type="danger">主题库隐藏</el-tag>
            <el-button
              v-if="canSetHidden"
              size="small"
              :type="problem.hidden ? 'danger' : 'default'"
              class="hidden-btn"
              @click="toggleHidden"
            >
              隐藏
            </el-button>
          </template>
        </ProblemStatement>

        <template #aside>
        <el-card v-if="problem.can_edit_statement" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="router.push(`/problems/${problem.id}/edit`)">编辑题目</el-button>
            <el-button plain style="width: 100%" :loading="rejudgeAllRunning" @click="rejudgeAll">批量重测本题提交</el-button>
          </div>
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
            <el-button plain style="width: 100%" @click="openOthers">所有代码</el-button>
            <div v-if="submitDisabledReason" class="dim center">{{ submitDisabledReason }}</div>
          </div>
        </el-card>

        <el-card class="aside-card">
          <template #header><span>题目信息</span></template>
          <div class="info-stats">
            <span>提交 <b>{{ problem.submit_count ?? 0 }}</b></span>
            <span>通过 <b>{{ problem.accepted_count ?? 0 }}</b></span>
            <span>通过率 <b>{{ acceptRate }}</b></span>
          </div>
          <div v-if="tagIds.length" class="info-tags">
            <el-popover :visible="tagsVisible" placement="bottom-start" popper-class="tags-pop">
              <template #reference>
                <span
                  class="show-tags"
                  @mouseenter="openTags"
                  @mouseleave="scheduleCloseTags"
                  @click="toggleTags"
                >
                  显示标签
                </span>
              </template>
              <div class="tags-pop-body" @mouseenter="openTags" @mouseleave="scheduleCloseTags">
                <el-tag
                  v-for="id in tagIds"
                  :key="id"
                  size="small"
                  class="pop-tag"
                  :class="`lv-${tagStore.levelOf(id)}`"
                >
                  {{ tagStore.nameOf(id) }}
                </el-tag>
              </div>
            </el-popover>
          </div>
          <div v-else class="dim">暂无标签</div>
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
      :problem="problem"
      :submitting="submitting"
      @submit="handleSubmit"
    />

    <SolutionsDialog v-model="showSolutions" :problem-id="problem.id" />

    <MySubmissionsDialog ref="mineDialogRef" v-model="showMine" :problem-id="problem.id" />

    <AllCodesDialog v-model="showOthers" :problem-id="problem.id" :problem-title="problem.title" />
</template>
  </div>
</template>

<style scoped>
.info-stats {
  display: flex;
  gap: 14px;
}

.info-stats b {
  font-weight: var(--soj-fw-emphasis);
}

.info-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

.lock-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.center {
  text-align: center;
}

/* 隐藏按钮推到题面卡右上角 */
.hidden-btn {
  margin-left: auto;
}

/* 「显示标签」入口 */
.show-tags {
  color: var(--el-color-primary);
  cursor: pointer;
  font-size: var(--soj-fs-body);
}

.show-tags:hover {
  text-decoration: underline;
}
</style>

<style>
/* 标签浮层（el-popover 挂 body，需非 scoped）：同表格标签栏的五级配色 */
.tags-pop-body {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.tags-pop-body .el-tag {
  background: var(--lv-bg);
  border-color: var(--lv-border);
  color: var(--el-text-color-primary);
}
</style>

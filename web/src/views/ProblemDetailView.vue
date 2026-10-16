<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { problemApi, submissionApi } from '@/api'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import { useUserStore } from '@/stores/user'
import { fmtDate, fmtMem, statusStyle } from '@/utils/format'
import { MAX_MARKDOWN_BYTES, markdownBytes } from '@/utils/vditor'
import type { ProblemDetail, Submission } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
let timer: ReturnType<typeof setInterval> | undefined

// 右列：提交弹窗 / 在线编程 / 我的提交与他人代码（Tab，各 5 条分页）/ 编辑题面
const showSubmit = ref(false)
const showWorkbench = ref(false)
const submitting = ref(false)
const activeTab = ref('mine')

const SIDE_PAGE_SIZE = 5
const mySubs = ref<Submission[]>([])
const myTotal = ref(0)
const myPage = ref(1)
const expandedMine = ref<number[]>([])
const others = ref<Submission[]>([])
const othersTotal = ref(0)
const othersPage = ref(1)
const expandedOthers = ref<number[]>([])
const showEditStatement = ref(false)
const editStatement = ref('')
const savingStatement = ref(false)

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
    await Promise.all([loadMine(), loadOthers()])
  } catch {
    router.replace('/problems')
  } finally {
    loading.value = false
  }
}

async function loadMine(p = 1) {
  if (!userStore.token) return
  try {
    const r = await problemApi.mySubmissions(Number(route.params.id), p, SIDE_PAGE_SIZE)
    mySubs.value = r.list
    myTotal.value = r.total
    myPage.value = p
  } catch {
    mySubs.value = []
  }
}

async function loadOthers(p = 1) {
  if (!problem.value?.can_view_others) return
  try {
    const r = await problemApi.submissions(Number(route.params.id), p, SIDE_PAGE_SIZE)
    others.value = r.list
    othersTotal.value = r.total
    othersPage.value = p
  } catch {
    others.value = []
  }
}

function toggleExpand(list: number[], id: number) {
  const i = list.indexOf(id)
  if (i >= 0) list.splice(i, 1)
  else list.push(id)
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

async function handleSubmit(_lang: string, code: string) {
  if (!problem.value) return
  submitting.value = true
  try {
    const r = await submissionApi.create(problem.value.id, _lang, code)
    ElMessage.success(`已提交 #${r.id}，正在判题`)
    showSubmit.value = false
    showWorkbench.value = false
    activeTab.value = 'mine'
    loadMine(1)
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
        loadMine()
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

function openEditStatement() {
  editStatement.value = problem.value?.description ?? ''
  showEditStatement.value = true
}

async function saveStatement() {
  if (!problem.value) return
  if (!editStatement.value.trim()) {
    ElMessage.warning('题面不能为空')
    return
  }
  if (markdownBytes(editStatement.value) > MAX_MARKDOWN_BYTES) {
    ElMessage.error('题面过长（上限 256KB）')
    return
  }
  savingStatement.value = true
  try {
    await problemApi.setStatement(problem.value.id, editStatement.value)
    problem.value.description = editStatement.value
    ElMessage.success('题面已保存')
    showEditStatement.value = false
  } finally {
    savingStatement.value = false
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
              <el-button type="warning" size="small" @click="router.push(lockRoute)">前往{{ lockWhat }}</el-button>
            </div>
          </template>
        </el-alert>

        <el-card>
          <div class="title-row">
            <span class="big-title">#{{ problem.id }} {{ problem.title }}</span>
            <el-tag v-if="problem.hidden" size="small" type="danger">主题库隐藏</el-tag>
          </div>
          <div class="info-row">
            <el-tag size="small" type="info">时间限制 {{ problem.time_limit_ms }} ms</el-tag>
            <el-tag size="small" type="info">内存限制 {{ fmtMem(problem.memory_limit_kb) }}</el-tag>
            <el-button
              v-if="canSetHidden"
              size="small"
              :type="problem.hidden ? 'warning' : 'default'"
              class="hidden-btn"
              @click="toggleHidden"
            >
              {{ problem.hidden ? '对学生隐藏中 · 点击恢复' : '对学生隐藏' }}
            </el-button>
          </div>
          <MdView :content="problem.description" />
        </el-card>

      <template #aside>
        <el-card v-if="problem.can_edit_statement" class="aside-card">
          <template #header><span>管理</span></template>
          <el-button type="primary" plain style="width: 100%" @click="openEditStatement">编辑题面</el-button>
        </el-card>

        <el-card class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" :disabled="submitDisabled" @click="openSubmitEntry">
              提交
            </el-button>
            <el-button plain style="width: 100%" :disabled="submitDisabled" @click="openWorkbenchEntry">
              在线编程
            </el-button>
            <div v-if="submitDisabledReason" class="dim center">{{ submitDisabledReason }}</div>
          </div>
        </el-card>

        <el-card class="aside-card">
          <template #header>
            <div class="aside-head">
              <el-radio-group v-model="activeTab" size="small">
                <el-radio-button value="mine">我的提交</el-radio-button>
                <el-radio-button value="others" :disabled="!problem.can_view_others">他人代码</el-radio-button>
              </el-radio-group>
            </div>
          </template>

          <template v-if="activeTab === 'mine'">
            <template v-if="userStore.token">
              <div
                v-for="s in mySubs"
                :key="s.id"
                class="sub-row"
                @click="toggleExpand(expandedMine, s.id)"
              >
                <div class="sub-line">
                  <span class="dim">#{{ s.id }}</span>
                  <el-tag size="small" :type="statusStyle(s.status).type">{{ statusStyle(s.status).label }}</el-tag>
                  <span v-if="s.score != null">{{ s.score }} 分</span>
                  <span class="dim sub-time">{{ fmtDate(s.created_at) }}</span>
                </div>
                <pre v-if="expandedMine.includes(s.id) && s.code" class="mono sub-code">{{ s.code }}</pre>
              </div>
              <el-empty v-if="!mySubs.length" description="还没有提交过" :image-size="40" />
              <div v-if="myTotal > SIDE_PAGE_SIZE" class="side-pager">
                <el-pagination
                  size="small"
                  background
                  layout="prev, pager, next"
                  :total="myTotal"
                  :page-size="SIDE_PAGE_SIZE"
                  :current-page="myPage"
                  @current-change="(p: number) => loadMine(p)"
                />
              </div>
            </template>
            <div v-else class="dim">登录后查看提交记录</div>
          </template>

          <template v-else>
            <div class="dim unlock-tip">你已在该题拿到满分，可查看所有人的代码</div>
            <div
              v-for="s in others"
              :key="s.id"
              class="sub-row"
              @click="toggleExpand(expandedOthers, s.id)"
            >
              <div class="sub-line">
                <span class="dim">#{{ s.id }}</span>
                <span class="sub-user">{{ s.username }}</span>
                <el-tag size="small" :type="statusStyle(s.status).type">{{ statusStyle(s.status).label }}</el-tag>
                <span v-if="s.score != null">{{ s.score }} 分</span>
              </div>
              <pre v-if="expandedOthers.includes(s.id) && s.code" class="mono sub-code">{{ s.code }}</pre>
            </div>
            <div v-if="othersTotal > SIDE_PAGE_SIZE" class="side-pager">
              <el-pagination
                size="small"
                background
                layout="prev, pager, next"
                :total="othersTotal"
                :page-size="SIDE_PAGE_SIZE"
                :current-page="othersPage"
                @current-change="(p: number) => loadOthers(p)"
              />
            </div>
            <el-empty v-if="!others.length" description="还没有人提交过" :image-size="40" />
          </template>
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

    <el-dialog v-model="showEditStatement" title="编辑题面（Markdown / 所见即所得）" width="860" top="4vh">
      <MdEditor v-model="editStatement" height="480px" />
      <template #footer>
        <el-button @click="showEditStatement = false">取消</el-button>
        <el-button type="primary" :loading="savingStatement" @click="saveStatement">保存</el-button>
      </template>
    </el-dialog>
    </template>
  </div>
</template>


<style scoped>
.mb8 {
  margin-bottom: 8px;
}

.lock-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
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

.hidden-btn {
  margin-left: auto;
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

.aside-head {
  display: flex;
  justify-content: center;
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

.sub-user {
  font-weight: 600;
  font-size: 13px;
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

.unlock-tip {
  margin-bottom: 6px;
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
}

.dim {
  color: #909399;
  font-size: 13px;
}

.side-pager {
  display: flex;
  justify-content: center;
  margin-top: 10px;
}
</style>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { UploadUserFile } from 'element-plus'

import { problemApi, submissionApi } from '@/api'
import CodeWorkbench from '@/components/CodeWorkbench.vue'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import ProblemForm from '@/components/ProblemForm.vue'
import type { ProblemFormPayload } from '@/components/ProblemForm.vue'
import SubmissionDetailDialog from '@/components/SubmissionDetailDialog.vue'
import SubmitDialog from '@/components/SubmitDialog.vue'
import { useUserStore } from '@/stores/user'
import { fmtDate, fmtMem, statusDesc, statusStyle } from '@/utils/format'
import { languageOptions } from '@/utils/code'
import type { ProblemDetail, SolutionItem, Submission } from '@/types'

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

const PAGE_SIZE = 10
const mySubs = ref<Submission[]>([])
const myTotal = ref(0)
const myPage = ref(1)
// 我的提交弹窗：列表点行 → 同弹窗内切详情视图
const showMine = ref(false)
const mineView = ref<'list' | 'detail'>('list')
const mineDetail = ref<Submission | null>(null)

const others = ref<Submission[]>([])
const othersTotal = ref(0)
const othersSort = ref<'time' | 'mem'>('time')
const othersLang = ref('')
const showOthers = ref(false)
const showOthersDetail = ref(false)
const othersDetail = ref<Submission | null>(null)

function openOthersDetail(s: Submission) {
  othersDetail.value = s
  showOthersDetail.value = true
}

// 题解（v0.5b-1）：满分 ∨ 老师群体可见可写；置顶/删他人需 solution.manage
const solutions = ref<SolutionItem[]>([])
const solutionsTotal = ref(0)
const solutionsPage = ref(1)
const canManageSolutions = ref(false)
const showSolutions = ref(false)
const showSolutionEditor = ref(false)
const solutionEditing = ref<SolutionItem | null>(null)
const solutionDraft = ref('')

const canEditMeta = computed(() => !!problem.value?.can_edit_statement)

async function loadSolutions(p = 1) {
  const r = await problemApi.solutions(Number(route.params.id), p, PAGE_SIZE)
  solutions.value = r.list
  solutionsTotal.value = r.total
  canManageSolutions.value = r.can_manage
  solutionsPage.value = p
}

function openSolutions() {
  if (!problem.value?.can_view_solutions) {
    ElMessage.warning('该题拿到满分后才能查看题解（老师群体不受限）')
    return
  }
  showSolutions.value = true
  loadSolutions(1)
}

function openMine() {
  showMine.value = true
  mineView.value = 'list'
  mineDetail.value = null
  loadMine(1)
}

function openMineDetail(s: Submission) {
  mineDetail.value = s
  mineView.value = 'detail'
}

function openOthers() {
  if (!problem.value?.can_view_others) {
    ElMessage.warning('该题拿到满分后可查看他人的代码（老师群体不受限）')
    return
  }
  showOthers.value = true
  loadOthers()
}

function openWriteSolution() {
  solutionEditing.value = null
  solutionDraft.value = ''
  showSolutionEditor.value = true
}

function openEditSolution(s: SolutionItem) {
  solutionEditing.value = s
  solutionDraft.value = s.content
  showSolutionEditor.value = true
}

async function saveSolution() {
  if (!solutionDraft.value.trim()) {
    ElMessage.warning('题解内容不能为空')
    return
  }
  if (solutionEditing.value) {
    await problemApi.updateSolution(problem.value!.id, solutionEditing.value.id, solutionDraft.value)
    ElMessage.success('题解已更新')
  } else {
    await problemApi.postSolution(problem.value!.id, solutionDraft.value)
    ElMessage.success('题解已发布')
  }
  showSolutionEditor.value = false
  await loadSolutions()
}

async function removeSolution(s: SolutionItem) {
  await ElMessageBox.confirm('确定删除这条题解？删除后不可恢复。', '删除题解', {
    type: 'warning',
    confirmButtonText: '删除',
    cancelButtonText: '取消',
  })
  await problemApi.deleteSolution(problem.value!.id, s.id)
  ElMessage.success('题解已删除')
  await loadSolutions()
}

async function togglePin(s: SolutionItem) {
  await problemApi.pinSolution(problem.value!.id, s.id, !s.pinned)
  await loadSolutions()
}

// 编辑题目（v0.5b-4 复用建题界面）：表单在 ProblemForm 共享组件，本页只保留提交流程；
// zip 可选——留空仅更新题面/信息，选择新 zip 则整包替换测试数据（旧测试点不保留）。
const showEdit = ref(false)
const savingEdit = ref(false)
const editFormRef = ref<InstanceType<typeof ProblemForm>>()
const editInitial = ref<Partial<Omit<ProblemFormPayload, 'zip' | 'checker'>>>({})
const fileIOHint = computed(() => ({
  input: problem.value?.file_io_input || 'problem.in',
  output: problem.value?.file_io_output || 'problem.out',
}))

function openEdit() {
  const p = problem.value!
  editInitial.value = {
    title: p.title,
    time_limit_ms: p.time_limit_ms,
    memory_limit_mb: Math.max(16, Math.round(p.memory_limit_kb / 1024)),
    compare: p.compare === 'tolerant' ? 'tolerant' : 'exact',
    float_tolerance: p.float_tolerance ?? 0.0001,
    statement: p.description ?? '',
    file_io_name: (p.file_io_input || 'problem.in').replace(/\.in$/, ''),
  }
  checkerFile.value = null
  showCheckerUpload.value = false
  showEdit.value = true
}

async function onSaveEditClick() {
  const p = editFormRef.value?.payload()
  if (!p || !problem.value) return
  if (p.zip) {
    const confirmed = await ElMessageBox.confirm(
      '将以新 zip 整包替换测试数据：旧测试点全部删除，样例章节按新数据重建。确认替换？',
      '替换测试数据',
      { type: 'warning', confirmButtonText: '替换', cancelButtonText: '取消' },
    )
      .then(() => true)
      .catch(() => false)
    if (!confirmed) return
  }
  await saveEdit(p)
}

async function saveEdit(p: ProblemFormPayload) {
  if (!problem.value) return
  savingEdit.value = true
  try {
    await problemApi.setStatement(problem.value.id, p.statement)
    problem.value.description = p.statement
    await problemApi.updateMeta(problem.value.id, {
      title: p.title,
      time_limit_ms: p.time_limit_ms,
      memory_limit_kb: p.memory_limit_mb * 1024,
      compare: p.compare,
      file_io_name: p.file_io_name,
      ...(p.compare === 'tolerant' ? { float_tolerance: p.float_tolerance } : {}),
    })
    if (p.zip) {
      const fd = new FormData()
      fd.append('zip', p.zip)
      await problemApi.replaceTestdata(problem.value.id, fd)
    }
    ElMessage.success('题目已保存')
    showEdit.value = false
    await load()
  } finally {
    savingEdit.value = false
  }
}

// 自定义校验器（v0.5b-2）：上传 .cpp 源文件，判题时懒编译；移除恢复按比对模式判定。
// 管理界面并入「编辑题目」弹窗，上传/移除为即时操作（不随弹窗「保存」提交）。
// 未启用时上传区默认收起（保留「配置校验器」入口），已启用或点开后显示。
const checkerFile = ref<File | null>(null)
const savingChecker = ref(false)
const showCheckerUpload = ref(false)

function onCheckerChange(file: UploadUserFile) {
  checkerFile.value = (file.raw as File) ?? null
}

async function saveChecker() {
  if (!problem.value || !checkerFile.value) return
  savingChecker.value = true
  try {
    const fd = new FormData()
    fd.append('checker', checkerFile.value)
    await problemApi.uploadChecker(problem.value.id, fd)
    ElMessage.success('校验器已上传，后续提交由校验器判定')
    checkerFile.value = null
    await load()
  } finally {
    savingChecker.value = false
  }
}

async function removeChecker() {
  if (!problem.value) return
  await ElMessageBox.confirm('移除后恢复按比对模式判定，确认移除？', '移除校验器', {
    type: 'warning',
    confirmButtonText: '移除',
    cancelButtonText: '取消',
  })
  await problemApi.removeChecker(problem.value.id)
  ElMessage.success('校验器已移除')
  await load()
}

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
    await loadMine()
  } catch {
    router.replace('/problems')
  } finally {
    loading.value = false
  }
}

async function loadMine(p = 1) {
  if (!userStore.token) return
  try {
    const r = await problemApi.mySubmissions(Number(route.params.id), p, PAGE_SIZE)
    mySubs.value = r.list
    myTotal.value = r.total
    myPage.value = p
  } catch {
    mySubs.value = []
  }
}

async function loadOthers() {
  if (!problem.value?.can_view_others) return
  try {
    const r = await problemApi.submissions(
      Number(route.params.id),
      1,
      PAGE_SIZE,
      othersSort.value,
      othersLang.value || undefined,
    )
    others.value = r.list
    othersTotal.value = r.total
  } catch {
    others.value = []
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

async function handleSubmit(_lang: string, code: string) {
  if (!problem.value) return
  submitting.value = true
  try {
    const r = await submissionApi.create(problem.value.id, _lang, code)
    ElMessage.success(`已提交 #${r.id}，正在判题`)
    showSubmit.value = false
    showWorkbench.value = false
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
            <el-tag size="small" type="info">时间限制 {{ problem.time_limit_ms }} ms</el-tag>
            <el-tag size="small" type="info">内存限制 {{ fmtMem(problem.memory_limit_kb) }}</el-tag>
          </div>
          <MdView :content="problem.description" />
        </el-card>

        <template #aside>
        <el-card v-if="problem.can_edit_statement" class="aside-card">
          <template #header><span>管理</span></template>
          <el-button type="primary" plain style="width: 100%" @click="openEdit">编辑题目</el-button>
        </el-card>

        <el-card class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" :disabled="submitDisabled" @click="openSubmitEntry">
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

    <!-- 题解弹窗：置顶优先，10 条/页 -->
    <el-dialog v-model="showSolutions" title="题解" width="1020" top="4vh">
      <div class="dlg-toolbar">
        <span class="dim">共 {{ solutionsTotal }} 篇</span>
        <el-button type="primary" plain @click="openWriteSolution">发表题解</el-button>
      </div>
      <div v-for="s in solutions" :key="s.id" class="solution-item">
        <div class="solution-meta">
          <el-tag v-if="s.pinned" size="small" type="warning">置顶</el-tag>
          <span class="sub-user">{{ s.nickname }}</span>
          <span class="dim">
            {{ fmtDate(s.created_at) }}
            <template v-if="s.updated_at !== s.created_at"> · 已编辑</template>
          </span>
          <span class="solution-actions">
            <el-button v-if="canManageSolutions" link type="primary" @click="togglePin(s)">
              {{ s.pinned ? '取消置顶' : '置顶' }}
            </el-button>
            <el-button v-if="s.uid === userStore.uid" link type="primary" @click="openEditSolution(s)">
              编辑
            </el-button>
            <el-button
              v-if="s.uid === userStore.uid || canManageSolutions"
              link
              type="danger"
              @click="removeSolution(s)"
            >
              删除
            </el-button>
          </span>
        </div>
        <MdView :content="s.content" />
      </div>
      <el-empty v-if="!solutions.length" description="还没有题解，快来发第一篇" :image-size="48" />
      <div v-if="solutionsTotal > PAGE_SIZE" class="side-pager">
        <el-pagination
          background
          layout="prev, pager, next"
          :total="solutionsTotal"
          :page-size="PAGE_SIZE"
          :current-page="solutionsPage"
          @current-change="(p: number) => loadSolutions(p)"
        />
      </div>
    </el-dialog>

    <!-- 我的提交弹窗：列表点行 → 同弹窗切详情（代码/耗时/内存/错误信息/测试点明细） -->
    <el-dialog v-model="showMine" title="我的提交" width="1020" top="4vh" @closed="mineView = 'list'">
      <template v-if="mineView === 'list'">
        <el-table :data="mySubs" highlight-current-row @row-click="openMineDetail">
          <el-table-column label="状态" width="170">
            <template #default="{ row }">
              <el-tooltip v-if="statusDesc(row.status)" placement="top" effect="light">
                <template #content>
                  <div class="status-tip">
                    <div>{{ statusDesc(row.status)!.meaning }}</div>
                    <div v-if="statusDesc(row.status)!.cause">常见原因：{{ statusDesc(row.status)!.cause }}</div>
                  </div>
                </template>
                <el-tag size="small" :type="statusStyle(row.status).type" class="status-tag">{{ statusStyle(row.status).label }}</el-tag>
              </el-tooltip>
              <el-tag v-else size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="score" label="得分" width="80" />
          <el-table-column prop="language" label="语言" width="100" />
          <el-table-column label="耗时" width="100">
            <template #default="{ row }">{{ row.time_ms }} ms</template>
          </el-table-column>
          <el-table-column label="内存" width="110">
            <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
          </el-table-column>
          <el-table-column label="提交时间">
            <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!mySubs.length" description="还没有提交过" :image-size="48" />
        <div v-if="myTotal > PAGE_SIZE" class="side-pager">
          <el-pagination
            background
            layout="prev, pager, next"
            :total="myTotal"
            :page-size="PAGE_SIZE"
            :current-page="myPage"
            @current-change="(p: number) => loadMine(p)"
          />
        </div>
      </template>
      <template v-else-if="mineDetail">
        <el-button class="mb8" @click="mineView = 'list'">← 返回列表</el-button>
        <el-descriptions :column="3" border class="detail-desc">
          <el-descriptions-item label="状态">
            <el-tooltip v-if="statusDesc(mineDetail.status)" placement="top" effect="light">
              <template #content>
                <div class="status-tip">
                  <div>{{ statusDesc(mineDetail.status)!.meaning }}</div>
                  <div v-if="statusDesc(mineDetail.status)!.cause">常见原因：{{ statusDesc(mineDetail.status)!.cause }}</div>
                </div>
              </template>
              <el-tag size="small" :type="statusStyle(mineDetail.status).type" class="status-tag">{{ statusStyle(mineDetail.status).label }}</el-tag>
            </el-tooltip>
            <el-tag v-else size="small" :type="statusStyle(mineDetail.status).type">{{ statusStyle(mineDetail.status).label }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="得分">{{ mineDetail.score }}</el-descriptions-item>
          <el-descriptions-item label="语言">{{ mineDetail.language }}</el-descriptions-item>
          <el-descriptions-item label="耗时">{{ mineDetail.time_ms }} ms</el-descriptions-item>
          <el-descriptions-item label="内存">{{ fmtMem(mineDetail.memory_kb) }}</el-descriptions-item>
          <el-descriptions-item label="提交时间">{{ fmtDate(mineDetail.created_at) }}</el-descriptions-item>
        </el-descriptions>
        <template v-if="mineDetail.error_info">
          <div class="detail-sub">错误信息</div>
          <pre class="mono sub-code">{{ mineDetail.error_info }}</pre>
        </template>
        <div class="detail-sub">代码</div>
        <pre class="mono sub-code">{{ mineDetail.code }}</pre>
        <template v-if="mineDetail.detail?.length">
          <div class="detail-sub">测试点明细</div>
          <el-table :data="mineDetail.detail" border>
            <el-table-column prop="index" label="测试点" width="90" />
            <el-table-column label="结果">
              <template #default="{ row }">
                <el-tag size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="耗时" width="120">
              <template #default="{ row }">{{ row.time_ms }} ms</template>
            </el-table-column>
            <el-table-column label="内存" width="120">
              <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
            </el-table-column>
          </el-table>
        </template>
      </template>
    </el-dialog>

    <!-- 他人代码弹窗：耗时升序为默认（并列按内存），最多显示 10 条，可切排序与语言；点行查看代码 -->
    <el-dialog v-model="showOthers" title="他人代码" width="1020" top="4vh">
      <div class="dlg-toolbar">
        <el-radio-group v-model="othersSort" @change="loadOthers">
          <el-radio-button value="time">时间优先</el-radio-button>
          <el-radio-button value="mem">空间优先</el-radio-button>
        </el-radio-group>
        <el-select
          v-model="othersLang"
          clearable
          placeholder="全部语言"
          style="width: 160px"
          @change="loadOthers"
        >
          <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
        </el-select>
        <span class="dim">共 {{ othersTotal }} 份，显示前 {{ PAGE_SIZE }} 份</span>
      </div>
      <el-table
        :data="others"
        size="small"
        border
        style="cursor: pointer"
        @row-click="(row: Submission) => openOthersDetail(row as Submission)"
      >
        <el-table-column label="状态" width="170">
          <template #default="{ row }">
            <el-tooltip v-if="statusDesc(row.status)" placement="top" effect="light">
              <template #content>
                <div class="status-tip">
                  <div>{{ statusDesc(row.status)!.meaning }}</div>
                  <div v-if="statusDesc(row.status)!.cause">常见原因：{{ statusDesc(row.status)!.cause }}</div>
                </div>
              </template>
              <el-tag size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
            </el-tooltip>
            <el-tag v-else size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="username" label="提交者" min-width="110" />
        <el-table-column prop="language" label="语言" width="100" />
        <el-table-column label="耗时" width="100">
          <template #default="{ row }">{{ row.time_ms }} ms</template>
        </el-table-column>
        <el-table-column label="内存" width="110">
          <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
        </el-table-column>
        <el-table-column prop="score" label="得分" width="80" />
        <el-table-column label="提交时间">
          <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!others.length" description="还没有人提交过" :image-size="48" />
    </el-dialog>

    <!-- 他人提交详情（点状态/行查看代码与测试点明细） -->
    <SubmissionDetailDialog
      v-model="showOthersDetail"
      :submission="othersDetail"
      :title="othersDetail ? `他人代码 · ${othersDetail.username} · ${othersDetail.problem_title || problem.title}` : ''"
    />


    <el-dialog v-model="showEdit" title="编辑题目（题面 / 信息 / 测试数据）" width="1020" top="4vh">
      <ProblemForm
        v-if="showEdit"
        ref="editFormRef"
        mode="edit"
        :saving="savingEdit"
        :initial="editInitial"
        :file-i-o-hint="fileIOHint"
      />
      <el-form label-position="top">
        <el-form-item>
          <div class="checker-block">
            <div class="checker-status">
              自定义校验器
              <el-tag v-if="problem.has_checker" size="small" type="success">已启用 · 判题由校验器决定（比对模式不生效）</el-tag>
              <el-tag v-else size="small" type="info">未启用 · 按比对模式判定</el-tag>
              <el-button
                v-if="!problem.has_checker && !showCheckerUpload"
                size="small"
                text
                type="primary"
                @click="showCheckerUpload = true"
              >
                配置校验器
              </el-button>
            </div>
            <template v-if="problem.has_checker || showCheckerUpload">
              <el-upload
                drag
                accept=".cpp"
                :limit="1"
                :auto-upload="false"
                :on-change="onCheckerChange"
                :on-remove="() => (checkerFile = null)"
                class="checker-upload"
              >
                <div class="checker-hint">
                  <p>{{ problem.has_checker ? '已有校验器，拖入新文件可替换' : '拖入 C++ 校验器源文件或点击选择（.cpp，≤256KB）' }}</p>
                  <p class="dim">调用约定：checker 输入 期望 考生输出，退出码 0 = 通过</p>
                </div>
              </el-upload>
              <div class="checker-actions">
                <el-button v-if="problem.has_checker" type="danger" plain @click="removeChecker">移除校验器</el-button>
                <el-button type="primary" :loading="savingChecker" :disabled="!checkerFile" @click="saveChecker">
                  上传并启用
                </el-button>
              </div>
            </template>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEdit = false">取消</el-button>
        <el-button type="primary" :loading="savingEdit" @click="onSaveEditClick">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showSolutionEditor" :title="solutionEditing ? '编辑题解' : '发表题解'" width="1020" top="4vh">
      <MdEditor v-model="solutionDraft" height="420px" />
      <template #footer>
        <el-button @click="showSolutionEditor = false">取消</el-button>
        <el-button type="primary" @click="saveSolution">发布</el-button>
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

.checker-hint {
  padding: 8px;
}

.checker-hint p {
  margin: 2px 0;
}

.checker-block {
  width: 100%;
}

.checker-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.checker-upload {
  width: 100%;
}

.checker-upload :deep(.el-upload),
.checker-upload :deep(.el-upload-dragger) {
  width: 100%;
}

.checker-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}

.mt8 {
  margin-top: 8px;
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

/* 纵向排列时取消 EP 相邻按钮的默认左移，保持居中 */
.entry-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

/* 弹窗内公共工具行 / 明细标题 */
.dlg-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.detail-sub {
  font-weight: 600;
  margin: 14px 0 8px;
}

.detail-desc {
  margin-bottom: 4px;
}

.mb8 {
  margin-bottom: 8px;
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

.solutions-card {
  margin-top: 16px;
}

.solutions-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.solution-item {
  padding: 10px 0 6px;
  border-bottom: 1px solid #f0f0f0;
}

.solution-item:last-child {
  border-bottom: none;
}

.solution-item :deep(.vditor-reset) {
  font-size: 13px;
}

.solution-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}

.solution-actions {
  margin-left: auto;
  display: flex;
  gap: 4px;
}

.mt8 {
  margin-top: 8px;
}

.status-tag {
  cursor: help;
}

.status-tip {
  line-height: 1.6;
  max-width: 260px;
  white-space: normal;
}

.status-tip div + div {
  margin-top: 4px;
  color: #909399;
}
</style>

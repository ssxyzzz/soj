<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { assignmentApi, authApi, contestApi, problemApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import { useUserStore } from '@/stores/user'
import { fmtDate, fmtMem, statusStyle } from '@/utils/format'
import type { AssignmentDetail, AssignmentProblem, ProgressRow, Submission } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const assignment = ref<AssignmentDetail | null>(null)
const loading = ref(true)
const progress = ref<ProgressRow[]>([])
const progressLoading = ref(false)

// 右列管理卡：调整题目弹窗（题库选题 + 拖动排序 + 快速删除，一次保存）
const showAdjust = ref(false)
const savingOrder = ref(false)
const editList = ref<{ id: number; title: string }[]>([])
const pickPid = ref<number | null>(null)
const problemOptions = ref<{ id: number; title: string }[]>([])
const dragIndex = ref<number | null>(null)

const myUid = ref<number | null>(null)

const myProgress = computed(() => {
  if (myUid.value == null) return null
  return progress.value.find((r) => r.uid === myUid.value) ?? null
})

const myPct = computed(() => {
  if (!assignment.value || !myProgress.value) return 0
  const total = assignment.value.problems.length
  if (!total) return 0
  return Math.round(((myProgress.value.solved ?? 0) / total) * 100)
})

async function load() {
  loading.value = true
  try {
    assignment.value = await assignmentApi.detail(Number(route.params.id))
    if (userStore.token) loadProgress()
  } catch {
    router.replace('/')
  } finally {
    loading.value = false
  }
}

async function loadProgress() {
  progressLoading.value = true
  try {
    progress.value = await assignmentApi.progress(Number(route.params.id))
  } catch {
    progress.value = []
  } finally {
    progressLoading.value = false
  }
}

function cellOf(row: ProgressRow, pid: number) {
  return row.problems.find((c) => c.pid === pid) ?? { pid, tries: 0, best: 0, solved: false }
}

// 完成情况单元格点看用户提交明细：老师（作业可管理）∨「查看用户提交明细」权限位
const canInspectSubs = computed(
  () => !!assignment.value?.manageable || userStore.can('submission.view.user'),
)
const showUserSubs = ref(false)
const userSubsLoading = ref(false)
const userSubs = ref<Submission[]>([])
const userSubsTitle = ref('')

async function openUserSubs(uid: number, uname: string, ap: AssignmentProblem) {
  if (!ap.contest_id) return
  showUserSubs.value = true
  userSubsLoading.value = true
  userSubsTitle.value = `${uname} · ${ap.title}`
  try {
    const r = await contestApi.userSubmissions(ap.contest_id, ap.id, uid)
    userSubs.value = r.list
  } finally {
    userSubsLoading.value = false
  }
}

function problemRoute(row: { id: number; contest_id?: number; exam_id?: number }) {
  if (row.exam_id) {
    router.push(`/exams/${row.exam_id}`)
  } else if (row.contest_id) {
    router.push(`/contests/${row.contest_id}`)
  } else {
    router.push(`/problems/${row.id}`)
  }
}

// ---- 调整题目弹窗 ----
function openAdjust() {
  if (!assignment.value) return
  editList.value = assignment.value.problems.map((p) => ({ id: p.id, title: p.title }))
  pickPid.value = null
  showAdjust.value = true
}

function addPicked() {
  if (!pickPid.value) {
    ElMessage.warning('请先从题库选择题目')
    return
  }
  if (editList.value.some((p) => p.id === pickPid.value)) {
    ElMessage.warning('该题已在作业里')
    return
  }
  const opt = problemOptions.value.find((p) => p.id === pickPid.value)
  editList.value.push({ id: pickPid.value, title: opt?.title ?? `#${pickPid.value}` })
  pickPid.value = null
}

function removeAt(i: number) {
  editList.value.splice(i, 1)
}

// 拖动排序：HTML5 drag & drop，拖到目标行位置
function onDragStart(i: number) {
  dragIndex.value = i
}

function onDrop(i: number) {
  if (dragIndex.value == null || dragIndex.value === i) return
  const [moved] = editList.value.splice(dragIndex.value, 1)
  editList.value.splice(i, 0, moved)
  dragIndex.value = null
}

function onDragOver(e: DragEvent) {
  e.preventDefault()
}

async function saveOrder() {
  if (!assignment.value) return
  savingOrder.value = true
  try {
    await assignmentApi.setProblems(assignment.value.id, editList.value.map((p) => p.id))
    ElMessage.success('题目清单已保存')
    showAdjust.value = false
    await load()
  } finally {
    savingOrder.value = false
  }
}

async function removeAssignment() {
  if (!assignment.value) return
  try {
    await ElMessageBox.confirm(
      `确定删除此次作业「${assignment.value.title}」吗？删除后不可恢复！`,
      '⚠️ 危险操作：确认删除',
      { type: 'error', confirmButtonText: '确认删除', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  await assignmentApi.remove(assignment.value.id)
  ElMessage.success('作业已删除')
  router.push(`/classes/${assignment.value.class_id}`)
}

async function loadProblemOptions() {
  const r = await problemApi.list(1, 100)
  problemOptions.value = r.list
}

onMounted(() => {
  if (userStore.token) {
    authApi.me().then((me) => {
      myUid.value = me.id
      loadProgress()
    }).catch(() => {})
  }
  load()
  loadProblemOptions()
})
</script>

<template>
  <div v-loading="loading">
    <PageShell v-if="assignment">
        <el-card class="mb16">
          <template #header>
            <div class="head">
              <span>{{ assignment.title }}</span>
              <el-link type="primary" @click="router.push(`/classes/${assignment.class_id}`)">返回班级</el-link>
            </div>
          </template>
          <template v-if="myProgress">
            <div class="progress-line">
              <span>我的进度：已完成 {{ myProgress.solved }} / {{ assignment.problems.length }} 题</span>
              <span class="dim">做题 {{ myProgress.attempted }} 题</span>
            </div>
            <el-progress
              :percentage="myPct"
              :status="myPct === 100 ? 'success' : undefined"
              :stroke-width="12"
            />
          </template>
          <div v-else class="dim">{{ userStore.token ? '暂无你的做题记录' : '登录后可查看自己的完成进度' }}</div>
        </el-card>

        <el-card v-if="assignment.intro" class="mb16">
          <div class="intro">{{ assignment.intro }}</div>
        </el-card>

        <el-card>
          <template #header><span>作业题目（{{ assignment.problems.length }}）</span></template>
          <el-table stripe :data="assignment.problems">
            <el-table-column prop="id" label="ID" width="100" />
            <el-table-column label="题目">
              <template #default="{ row }">
                <el-link type="primary" @click="problemRoute(row as AssignmentProblem)">
                  {{ row.title }}
                </el-link>
                <el-tag v-if="row.contest_id" size="small" type="success" class="ml8">比赛中</el-tag>
                <el-tag v-if="row.exam_id" size="small" type="success" class="ml8">考试中</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="做题方式" width="160">
              <template #default="{ row }">
                <el-link v-if="row.contest_id || row.exam_id" type="warning" @click="problemRoute(row as AssignmentProblem)">
                  去{{ row.exam_id ? '考试' : '比赛' }}页提交
                </el-link>
                <span v-else class="dim">点题目直接做，即时出分</span>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!assignment.problems.length" description="作业里还没有题目" :image-size="60" />
        </el-card>

        <el-card v-if="assignment.manageable" class="mt16">
          <template #header>
            <div class="head">
              <span>完成情况（{{ progress.length }} 名学生）</span>
              <el-button size="small" :loading="progressLoading" @click="loadProgress">刷新</el-button>
            </div>
          </template>
          <el-table stripe v-loading="progressLoading" :data="progress">
            <el-table-column prop="username" label="用户名" min-width="140" />
            <el-table-column prop="nickname" label="昵称" min-width="120" />
            <el-table-column
              v-for="ap in assignment!.problems"
              :key="ap.id"
              :label="'P' + ap.id"
              width="110"
              align="center"
            >
              <template #default="{ row }">
                <el-link
                  v-if="canInspectSubs && cellOf(row as ProgressRow, ap.id).tries > 0"
                  class="cell-link"
                  @click="openUserSubs((row as ProgressRow).uid, (row as ProgressRow).username, ap)"
                >
                  <el-tag v-if="cellOf(row as ProgressRow, ap.id).solved" size="small" type="success">通过</el-tag>
                  <el-tag v-else size="small" type="warning">
                    {{ cellOf(row as ProgressRow, ap.id).best }} 分 / {{ cellOf(row as ProgressRow, ap.id).tries }} 次
                  </el-tag>
                </el-link>
                <el-tag v-else-if="cellOf(row as ProgressRow, ap.id).solved" size="small" type="success">通过</el-tag>
                <el-tag v-else-if="cellOf(row as ProgressRow, ap.id).tries > 0" size="small" type="warning">
                  {{ cellOf(row as ProgressRow, ap.id).best }} 分 / {{ cellOf(row as ProgressRow, ap.id).tries }} 次
                </el-tag>
                <el-tag v-else size="small" type="info">未做</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="进度" width="120">
              <template #default="{ row }">{{ row.solved }} / {{ assignment!.problems.length }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!progress.length" description="班里还没有学生" :image-size="60" />
        </el-card>

      <template #aside>
        <el-card v-if="assignment.manageable" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="openAdjust">调整题目</el-button>
            <el-button type="danger" style="width: 100%" @click="removeAssignment">删作业</el-button>
          </div>
        </el-card>
      </template>
    </PageShell>

    <el-dialog v-model="showAdjust" title="调整作业题目" width="760" top="6vh">
      <div class="adjust-pick">
        <el-select v-model="pickPid" filterable placeholder="从题库选择题目加入" style="flex: 1">
          <el-option
            v-for="p in problemOptions"
            :key="p.id"
            :value="p.id"
            :label="`#${p.id} ${p.title}`"
          />
        </el-select>
        <el-button type="primary" @click="addPicked">添加</el-button>
      </div>
      <div class="adjust-list">
        <div
          v-for="(p, i) in editList"
          :key="p.id"
          class="adjust-row"
          draggable="true"
          @dragstart="onDragStart(i)"
          @drop="onDrop(i)"
          @dragover="onDragOver"
        >
          <span class="drag-handle">⠿</span>
          <span class="adjust-order">{{ i + 1 }}</span>
          <span class="adjust-title">#{{ p.id }} {{ p.title }}</span>
          <el-button size="small" type="danger" link @click="removeAt(i)">删除</el-button>
        </div>
        <el-empty v-if="!editList.length" description="还没有题目，从上方题库添加" :image-size="40" />
      </div>
      <div class="dim">拖动 ⠿ 调整顺序，右侧「删除」移除；点下方保存一次性生效</div>
      <template #footer>
        <el-button @click="showAdjust = false">取消</el-button>
        <el-button type="primary" :loading="savingOrder" @click="saveOrder">保存</el-button>
      </template>
    </el-dialog>

    <!-- 完成情况单元格点看的用户提交明细（含代码展开） -->
    <el-dialog v-model="showUserSubs" :title="`提交明细 · ${userSubsTitle}`" width="1020" top="4vh">
      <el-table stripe v-loading="userSubsLoading" :data="userSubs" border>
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="user-subs-detail">
              <pre v-if="row.code" class="mono sub-code">{{ row.code }}</pre>
              <el-table stripe v-if="row.detail?.length" :data="row.detail" size="small" border>
                <el-table-column prop="index" label="测试点" width="90" />
                <el-table-column label="结果">
                  <template #default="{ row: c }">
                    <el-tag size="small" :type="statusStyle(c.status).type">{{ statusStyle(c.status).label }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="耗时" width="120">
                  <template #default="{ row: c }">{{ c.time_ms }} ms</template>
                </el-table-column>
                <el-table-column label="内存" width="120">
                  <template #default="{ row: c }">{{ fmtMem(c.memory_kb) }}</template>
                </el-table-column>
              </el-table>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="170">
          <template #default="{ row }">
            <el-tag size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
          </template>
        </el-table-column>
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
      <el-empty v-if="!userSubsLoading && !userSubs.length" description="该学生在此题没有提交" :image-size="48" />
    </el-dialog>
  </div>
</template>

<style scoped>
.cell-link {
  vertical-align: baseline;
}

/* 卡片纵向间距 16px（编辑器上方） */
.mb16 {
  margin-bottom: 16px;
}

.mt16 {
  margin-top: 16px;
}

.progress-line {
  display: flex;
  justify-content: space-between;
  margin-bottom: 8px;
}

.intro {
  color: var(--el-text-color-regular);
  line-height: 1.7;
}

.adjust-pick {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.adjust-list {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  max-height: 320px;
  overflow: auto;
  margin-bottom: 8px;
}

.adjust-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--el-fill-color);
  cursor: grab;
  background: var(--el-bg-color);
}

.adjust-row:hover {
  background: var(--el-fill-color-light);
}

.drag-handle {
  color: var(--el-text-color-placeholder);
  cursor: grab;
}

.adjust-order {
  width: 24px;
  text-align: center;
  color: var(--el-text-color-secondary);
}

.adjust-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

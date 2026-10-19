<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { assignmentApi, authApi, problemApi } from '@/api'
import { downloadCsv } from '@/api/download'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import { buildAssignmentBoard } from '@/components/score/adapters'
import ScoreBoardDialog from '@/components/score/ScoreBoardDialog.vue'
import ScoreSummaryCard from '@/components/score/ScoreSummaryCard.vue'
import type { ScoreBoardView } from '@/components/score/types'
import UserSubmissionsDialog from '@/components/score/UserSubmissionsDialog.vue'
import { useUserStore } from '@/stores/user'
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
const showScoreBoard = ref(false)

// 完成情况单元格点看用户提交明细：老师（作业可管理）∨「查看用户提交明细」权限位
const canInspectSubs = computed(
  () => !!assignment.value?.manageable || userStore.can('submission.view.user'),
)

const scoreBoard = computed<ScoreBoardView | null>(() => {
  if (!assignment.value) return null
  return buildAssignmentBoard(
    assignment.value,
    progress.value,
    canInspectSubs.value,
    assignment.value.manageable,
  )
})
const scoreTitle = computed(() => (assignment.value ? `成绩表 · ${assignment.value.title}` : '成绩表'))

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

// 成绩导出 CSV（v0.5.6）：服务端按成员清单现算进度；文件名以服务端 Content-Disposition 为准
const exporting = ref(false)
async function exportCsv() {
  if (!assignment.value) return
  exporting.value = true
  try {
    await downloadCsv(`/assignments/${assignment.value.id}/export`, `${assignment.value.title}.csv`)
  } finally {
    exporting.value = false
  }
}

const showUserSubs = ref(false)
const userSubsLoading = ref(false)
const userSubs = ref<Submission[]>([])
const userSubsTitle = ref('')

async function openUserSubs(uid: number, uname: string, pid: number, ptitle: string) {
  showUserSubs.value = true
  userSubsLoading.value = true
  userSubsTitle.value = `${uname} · ${ptitle}`
  try {
    if (!assignment.value) return
    const r = await assignmentApi.userSubmissions(assignment.value.id, pid, uid)
    userSubs.value = r.list
  } finally {
    userSubsLoading.value = false
  }
}

function inspectCell(payload: { uid: number; name: string; pid: number; title: string }) {
  openUserSubs(payload.uid, payload.name, payload.pid, payload.title)
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
    }).catch(() => {})
  }
  load()
  loadProblemOptions()
})
</script>

<template>
  <div v-loading="loading">
    <PageShell v-if="assignment">
        <el-card class="mb16" body-style="padding: 0">
          <template #header>
            <div class="head">
              <span>{{ assignment.title }}</span>
              <el-link type="primary" @click="router.push(`/classes/${assignment.class_id}`)">返回班级</el-link>
            </div>
          </template>
        </el-card>

        <el-card v-if="assignment.intro_html || assignment.intro" class="mb16">
          <div class="intro"><MdView :content="assignment.intro_html || assignment.intro" /></div>
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

      <template #aside>
        <ScoreSummaryCard
          class="aside-card"
          :board="scoreBoard"
          :loading="progressLoading"
          :visible="!!userStore.token"
          empty-text="登录后查看成绩"
          :my-uid="myUid"
          :can-refresh="!!userStore.token"
          @open="showScoreBoard = true"
          @refresh="loadProgress"
        />

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

    <ScoreBoardDialog
      v-model="showScoreBoard"
      :board="scoreBoard"
      :title="scoreTitle"
      :loading="progressLoading"
      :can-export="!!assignment?.manageable"
      :exporting="exporting"
      :my-uid="myUid"
      @refresh="loadProgress"
      @export="exportCsv"
      @inspect="inspectCell"
    />

    <UserSubmissionsDialog
      v-model="showUserSubs"
      :title="`提交明细 · ${userSubsTitle}`"
      :loading="userSubsLoading"
      :list="userSubs"
      empty-text="该学生在此题没有提交"
    />
  </div>
</template>

<style scoped>
/* 卡片纵向间距 16px（编辑器上方） */
.mb16 {
  margin-bottom: 16px;
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

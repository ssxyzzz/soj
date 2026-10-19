<script setup lang="ts">
// 作业详情（v0.7 按 design.md 重排）：名称卡（20px + 说明）→ 提交进度 → 题目表；
// 副列管理卡 → 成绩表。题目表列：T1/状态/题目/标签/通过率。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { assignmentApi, authApi, problemApi } from '@/api'
import { downloadCsv } from '@/api/download'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import FitTags from '@/components/problem/FitTags.vue'
import SubStatus from '@/components/SubStatus.vue'
import { buildAssignmentBoard } from '@/components/score/adapters'
import ScoreBoardDialog from '@/components/score/ScoreBoardDialog.vue'
import ScoreSummaryCard from '@/components/score/ScoreSummaryCard.vue'
import type { ScoreBoardView } from '@/components/score/types'
import UserSubmissionsDialog from '@/components/score/UserSubmissionsDialog.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { useUserStore } from '@/stores/user'
import { MAX_RICHTEXT_BYTES, richtextBytes } from '@/utils/richtext'
import type { AssignmentDetail, AssignmentProblem, ProgressRow, Submission } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const tagStore = useTagTreeStore()

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

// 编辑名称/说明（v0.7）
const showEdit = ref(false)
const savingEdit = ref(false)
const editTitle = ref('')
const editIntro = ref('')

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

const problemTotal = computed(() => assignment.value?.problems?.length ?? 0)
const submittedCount = computed(
  () => (assignment.value?.problems ?? []).filter((p) => p.submitted).length,
)
const submitPct = computed(() => {
  if (!problemTotal.value) return 0
  return Math.round((submittedCount.value / problemTotal.value) * 100)
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
    router.push(`/exams/${row.exam_id}/problems/${row.id}`)
  } else if (row.contest_id) {
    router.push(`/contests/${row.contest_id}/problems/${row.id}`)
  } else {
    router.push(`/problems/${row.id}`)
  }
}

// 通过率 = AC 提交 / 总提交（与题库同口径）
function acceptRate(row: { submit_count?: number; accepted_count?: number }): string {
  if (!row.submit_count) return '—'
  const pct = ((row.accepted_count ?? 0) / row.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
}

// ---- 编辑名称/说明 ----
function openEdit() {
  if (!assignment.value) return
  editTitle.value = assignment.value.title
  editIntro.value = assignment.value.intro_html || assignment.value.intro || ''
  showEdit.value = true
}

async function saveEdit() {
  if (!assignment.value) return
  if (!editTitle.value.trim()) {
    ElMessage.warning('作业名称不能为空')
    return
  }
  if (richtextBytes(editIntro.value) > MAX_RICHTEXT_BYTES) {
    ElMessage.error('作业说明过长（上限 256KB）')
    return
  }
  savingEdit.value = true
  try {
    await assignmentApi.update(assignment.value.id, {
      title: editTitle.value.trim(),
      intro_html: editIntro.value,
    })
    ElMessage.success('作业已保存')
    showEdit.value = false
    await load()
  } finally {
    savingEdit.value = false
  }
}

// ---- 调整题目弹窗 ----
function openAdjust() {
  if (!assignment.value) return
  const source = assignment.value.edit_problems ?? assignment.value.problems
  editList.value = source.map((p) => ({ id: p.id, title: p.title }))
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
  problemOptions.value = r.list.filter((p) => !p.hidden).map((p) => ({ id: p.id, title: p.title }))
}

onMounted(() => {
  tagStore.ensure() // 标签名映射异步就绪，题目表标签列响应式跟随
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
      <!-- 名称卡：名称 20px + 说明 -->
      <el-card>
        <div class="assignment-body">
          <div class="title-row">
            <span class="big-title">{{ assignment.title }}</span>
            <el-link class="back" type="primary" @click="router.push(`/classes/${assignment.class_id}`)">返回班级</el-link>
          </div>
          <div v-if="assignment.intro_html || assignment.intro" class="intro">
            <MdView :content="assignment.intro_html || assignment.intro" />
          </div>
        </div>
      </el-card>

      <!-- 提交进度（学生视角；老师看成绩表） -->
      <el-card v-if="!assignment.manageable" class="block">
        <div class="progress-head">
          <span class="progress-title">提交进度</span>
          <span>{{ submittedCount }} / {{ problemTotal }}</span>
        </div>
        <el-progress :percentage="submitPct" :stroke-width="10" />
        <div class="dim center">共 {{ problemTotal }} 道题，已提交 {{ submittedCount }} 道</div>
      </el-card>

      <!-- 题目表：T1 / 状态 / 题目 / 标签 / 通过率 -->
      <el-card class="block">
        <template #header><span>作业题目（{{ problemTotal }}）</span></template>
        <el-table v-if="problemTotal" stripe :data="assignment.problems">
          <el-table-column label="题号" width="80">
            <template #default="{ $index }">T{{ $index + 1 }}</template>
          </el-table-column>
          <el-table-column v-if="userStore.token" label="状态" width="150">
            <template #default="{ row }">
              <template v-if="row.result_hidden">
                <el-tag v-if="row.submitted" size="small" type="info">已提交</el-tag>
                <span v-else class="dim">—</span>
              </template>
              <SubStatus v-else-if="row.status" :status="row.status" :score="row.my_score" />
              <span v-else class="dim">未做</span>
            </template>
          </el-table-column>
          <el-table-column label="题目" min-width="260">
            <template #default="{ row }">
              <el-link type="primary" @click="problemRoute(row as AssignmentProblem)">
                {{ row.title }}
              </el-link>
            </template>
          </el-table-column>
          <el-table-column label="标签" min-width="170">
            <template #default="{ row }">
              <FitTags v-if="!row.result_hidden && row.tags?.length" :ids="row.tags" />
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="通过率" width="110">
            <template #default="{ row }">
              <el-tooltip
                v-if="!row.result_hidden && row.submit_count"
                :content="`AC ${row.accepted_count ?? 0} / 提交 ${row.submit_count}`"
                placement="top"
              >
                <span>{{ acceptRate(row) }}</span>
              </el-tooltip>
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else description="作业里还没有题目" :image-size="60" />
      </el-card>

      <template #aside>
        <!-- 管理卡 -->
        <el-card v-if="assignment.manageable" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="openEdit">编辑作业</el-button>
            <el-button style="width: 100%" @click="openAdjust">调整题目</el-button>
            <el-button type="danger" style="width: 100%" @click="removeAssignment">删作业</el-button>
          </div>
        </el-card>

        <!-- 成绩表：作业始终显示（教师全班、学生自己） -->
        <ScoreSummaryCard
          v-if="userStore.token"
          class="aside-card"
          :board="scoreBoard"
          :loading="progressLoading"
          :visible="true"
          empty-text="登录后查看成绩"
          :my-uid="myUid"
          :can-refresh="true"
          @open="showScoreBoard = true"
          @refresh="loadProgress"
        />
      </template>
    </PageShell>

    <el-dialog v-model="showEdit" title="编辑作业" width="760" top="5vh">
      <el-form label-position="top">
        <el-form-item label="作业名称" required>
          <el-input v-model="editTitle" maxlength="64" />
        </el-form-item>
        <el-form-item label="作业说明">
          <MdEditor v-model="editIntro" height="340px" variant="compact" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEdit = false">取消</el-button>
        <el-button type="primary" :loading="savingEdit" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>

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
          <span class="adjust-order">T{{ i + 1 }}</span>
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
.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.assignment-body {
  max-width: 860px;
  margin: 0 auto;
}

.big-title {
  font-size: var(--soj-fs-page-title);
  font-weight: var(--soj-fw-emphasis);
}

.back {
  margin-left: auto;
}

.intro {
  margin-top: 10px;
  color: var(--el-text-color-regular);
  line-height: 1.7;
}

.block {
  margin-top: 16px;
}

.progress-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.progress-title {
  font-weight: var(--soj-fw-emphasis);
}

.center {
  text-align: center;
  margin-top: 6px;
}

.adjust-pick {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.adjust-list {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
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
  width: 32px;
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

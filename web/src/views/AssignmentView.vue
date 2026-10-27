<script setup lang="ts">
// 作业详情（v0.7 按 design.md 重排）：名称卡（20px + 说明）→ 题目表；
// 副列管理卡 → 成绩表。题目表列：T1/状态/题目/难度/标签/通过率（难度 2026-10-27 补，对齐题库）。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { assignmentApi, authApi } from '@/api'
import { downloadFile } from '@/api/download'
import MdEditor from '@/components/MdEditor.vue'
import ProblemPicker from '@/components/ProblemPicker.vue'
import CopyFromListButton from '@/components/CopyFromListButton.vue'
import TierBadge from '@/components/TierBadge.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import FitTags from '@/components/problem/FitTags.vue'
import SubStatus from '@/components/SubStatus.vue'
import { buildAssignmentBoard } from '@/components/score/adapters'
import ScoreBoardDialog from '@/components/score/ScoreBoardDialog.vue'
import ScoreSummaryCard from '@/components/score/ScoreSummaryCard.vue'
import type { ScoreBoardView } from '@/components/score/types'
import UserSubmissionsDialog from '@/components/score/UserSubmissionsDialog.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { useUserStore } from '@/stores/user'
import { MAX_CONTENT_BYTES, contentBytes } from '@/utils/md'
import type { AssignmentDetail, AssignmentProblem, PickedProblem, ProgressRow, Submission } from '@/types'

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
const editList = ref<PickedProblem[]>([])

// 从题单复制（v0.18.3 拍板 7）：拍平合并进已选（按题号去重），隐藏题提示跳过数量
function mergeFromList(items: { id: number; title: string }[], skippedHidden: number) {
  const have = new Set(editList.value.map((p) => p.id))
  let added = 0
  for (const it of items) {
    if (!have.has(it.id)) {
      editList.value.push({ id: it.id, title: it.title })
      added++
    }
  }
  ElMessage.success(`已复制 ${added} 题${skippedHidden ? `，跳过隐藏 ${skippedHidden} 题` : ''}`)
}

// 编辑作业（v0.7.4：名称 + 说明 + 删作业收进同一弹窗，「编辑说明」入口取消）
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

// 排名列：老师（可管理）∨「查看班级作业成绩表」权限位（全班视角）时展示；
// 行数（全班 / 只自己）由服务端按同一权限判定下发，这里只对齐排名列显隐
const showRank = computed(
  () => !!assignment.value?.manageable || userStore.can('assignment.score.view'),
)

const scoreBoard = computed<ScoreBoardView | null>(() => {
  if (!assignment.value) return null
  return buildAssignmentBoard(assignment.value, progress.value, canInspectSubs.value, showRank.value)
})
const scoreTitle = computed(() => (assignment.value ? `成绩表 · ${assignment.value.title}` : '成绩表'))

const problemTotal = computed(() => assignment.value?.problems?.length ?? 0)

// v3「完成情况」卡：成绩表我的行总分；已提交 = 有我方状态的题数
const myScore = computed(() => {
  const row = scoreBoard.value?.rows.find((r) => r.uid === myUid.value)
  return row?.total ?? null
})
const mySubmitted = computed(
  () => (assignment.value?.problems ?? []).filter((p) => p.status || p.submitted).length,
)

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
    await downloadFile(`/assignments/${assignment.value.id}/export`, `${assignment.value.title}.csv`)
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

// ---- 编辑作业 / 删作业 ----
function openEdit() {
  if (!assignment.value) return
  editTitle.value = assignment.value.title
  editIntro.value = assignment.value.intro || ''
  showEdit.value = true
}

async function saveEdit() {
  if (!assignment.value) return
  if (!editTitle.value.trim()) {
    ElMessage.warning('作业名称不能为空')
    return
  }
  if (contentBytes(editIntro.value) > MAX_CONTENT_BYTES) {
    ElMessage.error('作业说明过长（上限 256KB）')
    return
  }
  savingEdit.value = true
  try {
    await assignmentApi.update(assignment.value.id, {
      title: editTitle.value.trim(),
      intro: editIntro.value,
    })
    ElMessage.success('作业已保存')
    showEdit.value = false
    await load()
  } finally {
    savingEdit.value = false
  }
}

// ---- 调整题目弹窗（v0.7.3 起用统一选题器 ProblemPicker） ----
function openAdjust() {
  if (!assignment.value) return
  const source = assignment.value.edit_problems ?? assignment.value.problems
  editList.value = source.map((p) => ({ id: p.id, title: p.title }))
  showAdjust.value = true
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
  showEdit.value = false
  router.push(`/classes/${assignment.value.class_id}`)
}

onMounted(() => {
  tagStore.ensure() // 标签名映射异步就绪，题目表标签列响应式跟随
  if (userStore.token) {
    authApi.me().then((me) => {
      myUid.value = me.id
    }).catch(() => {})
  }
  load()
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
          <div v-if="assignment.intro" class="intro">
            <MdView :content="assignment.intro" />
          </div>
        </div>
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
              <span class="tlink" @click="problemRoute(row as AssignmentProblem)">{{ row.title }}</span>
            </template>
          </el-table-column>
          <!-- 难度（2026-10-27）：TierBadge 与题库难度列同款（锁题隐藏期随行隐藏信息口径，不给难度也无妨——服务端已下发） -->
          <el-table-column label="难度" width="110">
            <template #default="{ row }">
              <TierBadge :tier="(row as AssignmentProblem).difficulty || 3" kind="difficulty" />
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
        <!-- 管理卡（v0.7.4 精简）：编辑说明与删作业收进编辑弹窗 -->
        <RailCard v-if="assignment.manageable" ov="管理" title="作业管理" class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="openEdit">编辑作业</el-button>
            <el-button style="width: 100%" @click="openAdjust">调整题目</el-button>
          </div>
        </RailCard>

        <!-- v3「完成情况」：我的得分 / 已提交（登录可见） -->
        <RailCard v-if="userStore.token" ov="说明" title="完成情况" class="aside-card">
          <div class="soj-kv">
            <div class="k">
              <b>{{ myScore ?? '—' }}<small>/{{ problemTotal * 100 }}</small></b>
              <span>我的得分</span>
            </div>
            <div class="k">
              <b>{{ mySubmitted }}<small>/{{ problemTotal }}</small></b>
              <span>已提交</span>
            </div>
          </div>
        </RailCard>

        <!-- 成绩表：作业始终显示（老师与持「查看班级作业成绩表」者全班，否则只自己——行数由服务端决定） -->
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

    <el-dialog v-model="showEdit" title="编辑作业" width="960" top="5vh">
      <el-form label-position="top">
        <el-form-item label="作业名称" required>
          <el-input v-model="editTitle" maxlength="64" />
        </el-form-item>
        <el-form-item label="作业说明">
          <MdEditor v-model="editIntro" height="150px" variant="compact" />
        </el-form-item>
      </el-form>
      <template #footer>
        <div class="edit-footer">
          <el-button type="danger" @click="removeAssignment">删作业</el-button>
          <div>
            <el-button @click="showEdit = false">取消</el-button>
            <el-button type="primary" :loading="savingEdit" @click="saveEdit">保存</el-button>
          </div>
        </div>
      </template>
    </el-dialog>

    <el-dialog v-model="showAdjust" title="调整作业题目" width="960" top="5vh">
      <div class="copy-row">
        <CopyFromListButton @merge="mergeFromList" />
      </div>
      <ProblemPicker v-model="editList" />
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
/* 「从题单复制」按钮行与下方选题器留出间距（2026-10-26：原贴在一起） */
.copy-row {
  margin-bottom: 10px;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* 随主列撑满卡片，与卡片左右缘对齐 */
.assignment-body {
  margin: 0;
}

/* v3 实体大标题：藏蓝 26/800（作业名） */
.big-title {
  font-size: var(--soj-fs-entity-detail);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -0.3px;
  color: var(--soj-navy);
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








/* 编辑弹窗 footer：删作业靠左，取消/保存靠右 */
.edit-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}
</style>
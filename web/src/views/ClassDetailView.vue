<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { classApi, examApi } from '@/api'
import { createAssignment as apiCreateAssignment, dissolveClass, transferClass, updateClass } from '@/api/classManage'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import ProblemPicker from '@/components/ProblemPicker.vue'
import SwitchRow from '@/components/SwitchRow.vue'
import { fmtDate } from '@/utils/format'
import { MAX_CONTENT_BYTES, contentBytes } from '@/utils/md'
import { useUserStore } from '@/stores/user'
import type { AssignmentListItem, ClassDetail, ClassMember, PickedProblem } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const cl = ref<ClassDetail | null>(null)
const loading = ref(true)
const section = ref<'assignments' | 'exams' | 'members'>('assignments')
const assignments = ref<AssignmentListItem[]>([])
const assignmentsVisible = ref(false)

const joinCode = ref('')
const joining = ref(false)
const showCreateAssignment = ref(false)
const creatingAssignment = ref(false)
const assignmentForm = reactive({ title: '', intro: '', picked: [] as PickedProblem[] })
const importText = ref('')
const importing = ref(false)

// 发起考试
const exams = ref<{ id: number; title: string; status: string; start: string; end: string; problem_count: number }[]>([])
const examsVisible = ref(false)
const showExam = ref(false)
const creatingExam = ref(false)
const examForm = ref({
  title: '',
  intro: '',
  range: [] as string[],
  hiddenDays: 0,
  picked: [] as PickedProblem[],
  fileIO: false,
  scoreboard: true,
  hideDuring: true,
})

async function loadExams() {
  if (!cl.value) return
  try {
    const r = await examApi.listByClass(cl.value.id)
    examsVisible.value = r.visible
    exams.value = r.list
  } catch {
    exams.value = []
  }
}

function openExam(row: { id: number }) {
  router.push(`/exams/${row.id}`)
}

async function createExam() {
  if (!cl.value) return
  if (!examForm.value.title.trim() || examForm.value.range.length !== 2) {
    ElMessage.warning('请填写考试名称与起止时间')
    return
  }
  if (!examForm.value.picked.length) {
    ElMessage.warning('请从选题器选择至少一道题目')
    return
  }
  creatingExam.value = true
  try {
    const r = await examApi.create(cl.value.id, {
      title: examForm.value.title.trim(),
      intro: examForm.value.intro.trim(),
      start: new Date(examForm.value.range[0]).toISOString(),
      end: new Date(examForm.value.range[1]).toISOString(),
      hidden_days: examForm.value.hiddenDays,
      problem_ids: examForm.value.picked.map((p) => p.id),
      file_io: examForm.value.fileIO,
      scoreboard: examForm.value.scoreboard,
      hide_during: examForm.value.hideDuring,
    })
    ElMessage.success('考试创建成功')
    showExam.value = false
    router.push(`/exams/${r.id}`)
  } finally {
    creatingExam.value = false
  }
}

async function load() {
  loading.value = true
  try {
    cl.value = await classApi.detail(Number(route.params.id))
    await loadAssignments()
    await loadExams()
  } catch {
    router.replace('/classes')
  } finally {
    loading.value = false
  }
}

async function loadAssignments() {
  if (!cl.value) return
  try {
    const r = await classApi.listAssignments(cl.value.id)
    assignmentsVisible.value = r.visible
    assignments.value = r.list
  } catch {
    assignments.value = []
  }
}

async function join() {
  if (!cl.value) return
  joining.value = true
  try {
    await classApi.join(cl.value.id, joinCode.value)
    ElMessage.success('加入成功')
    await load()
  } finally {
    joining.value = false
  }
}

const leaving = ref(false)
async function leave() {
  if (!cl.value || leaving.value) return
  leaving.value = true
  try {
    await classApi.leave(cl.value.id)
    ElMessage.success('已退出班级')
    await load()
  } finally {
    leaving.value = false
  }
}

// 转让班级（v0.7.2，建班人 ∨ root；v0.7.4 收进编辑班级弹窗）：
// 从成员中选受让人，二次确认后换主；受让人成为新建班人，原建班人降为班级管理员
const transferring = ref(false)
const transferUid = ref<number | null>(null)
const transferCandidates = computed(() =>
  (cl.value?.members ?? []).filter((m) => !m.is_owner),
)
async function doTransfer() {
  if (!cl.value || !transferUid.value || transferring.value) return
  const target = transferCandidates.value.find((m) => m.uid === transferUid.value)
  try {
    await ElMessageBox.confirm(
      `确定把班级「${cl.value.name}」转让给「${target?.username ?? '#' + transferUid.value}」吗？转让后对方将成为建班人，你将变为班级管理员。`,
      '转让班级',
      { type: 'warning', confirmButtonText: '确认转让', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  transferring.value = true
  try {
    await transferClass(cl.value.id, transferUid.value)
    ElMessage.success('班级已转让')
    showEditClass.value = false
    await load()
  } finally {
    transferring.value = false
  }
}

// 解散班级（v0.7.2，建班人 ∨ root；v0.7.4 收进编辑班级弹窗）：
// 班级仍挂有考试/作业时后端拒绝并提示
const dissolving = ref(false)
async function dissolve() {
  if (!cl.value || dissolving.value) return
  try {
    await ElMessageBox.confirm(
      `确定解散班级「${cl.value.name}」吗？班内全部考试与作业将一并删除、所有成员将被移出，删除后不可恢复！`,
      '⚠️ 危险操作：确认解散',
      { type: 'error', confirmButtonText: '确认解散', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  dissolving.value = true
  try {
    await dissolveClass(cl.value.id)
    ElMessage.success('班级已解散')
    showEditClass.value = false
    router.replace('/classes')
  } finally {
    dissolving.value = false
  }
}

async function regenInvite() {
  if (!cl.value) return
  const r = await classApi.regenInvite(cl.value.id)
  if (cl.value) cl.value.invite_code = r.invite_code
  ElMessage.success('已重置邀请码')
}

function copyInvite() {
  if (cl.value?.invite_code) {
    navigator.clipboard?.writeText(cl.value.invite_code)
    ElMessage.success('邀请码已复制')
  }
}

async function changeMemberRole(row: ClassMember, role: string) {
  if (!cl.value) return
  await classApi.setMemberRole(cl.value.id, row.uid, role)
  ElMessage.success(`已把 ${row.username} 设为${role === 'admin' ? '班级管理员' : '普通成员'}`)
  await load()
}

async function importMembers() {
  if (!cl.value || !importText.value.trim()) {
    ElMessage.warning('请填写用户名列表')
    return
  }
  importing.value = true
  try {
    const r = await classApi.importMembers(cl.value.id, importText.value)
    ElMessage.success(`导入完成：${r.added}/${r.total}`)
    importText.value = ''
    await load()
  } finally {
    importing.value = false
  }
}

async function createAssignment() {
  if (!cl.value) return
  if (!assignmentForm.title.trim()) {
    ElMessage.warning('请填写作业名称')
    return
  }
  creatingAssignment.value = true
  try {
    const r = await apiCreateAssignment(cl.value.id, {
      title: assignmentForm.title.trim(),
      intro: assignmentForm.intro.trim(),
      problem_ids: assignmentForm.picked.map((p) => p.id),
    })
    ElMessage.success('作业创建成功')
    showCreateAssignment.value = false
    assignmentForm.title = ''
    assignmentForm.intro = ''
    assignmentForm.picked = []
    router.push(`/assignments/${r.id}`)
  } finally {
    creatingAssignment.value = false
  }
}

// ---- 编辑班级（v0.7.3 名称/公告；v0.7.4 私密开关 + 转让 + 解散收进同一弹窗） ----
const showEditClass = ref(false)
const savingClass = ref(false)
const editClassForm = reactive({ name: '', intro: '', private: false })
// 可见性/转让/解散仅建班人 ∨ root（与后端 SetClassPrivate/Transfer/Delete 同门槛）
const isClassOwner = computed(
  () => !!cl.value && (cl.value.owner_id === userStore.uid || userStore.role === 'root'),
)

function openEditClass() {
  if (!cl.value) return
  editClassForm.name = cl.value.name
  editClassForm.intro = cl.value.intro || ''
  editClassForm.private = !!cl.value.private
  transferUid.value = null
  showEditClass.value = true
}

async function saveEditClass() {
  if (!cl.value) return
  if (!editClassForm.name.trim()) {
    ElMessage.warning('班级名称不能为空')
    return
  }
  if (contentBytes(editClassForm.intro) > MAX_CONTENT_BYTES) {
    ElMessage.error('班级公告过长（上限 256KB）')
    return
  }
  savingClass.value = true
  try {
    await updateClass(cl.value.id, {
      name: editClassForm.name.trim(),
      intro: editClassForm.intro,
      private: isClassOwner.value ? editClassForm.private : undefined,
    })
    ElMessage.success('班级已保存')
    showEditClass.value = false
    await load()
  } finally {
    savingClass.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <PageShell v-if="cl">
        <el-card>
          <div class="title-row">
            <h3>{{ cl.name }}</h3>
            <el-tag v-if="cl.private" size="small" type="warning">私密班</el-tag>
            <el-tag v-if="cl.joined" size="small" type="success">已加入</el-tag>
          </div>
          <div v-if="cl.intro" class="intro"><MdView :content="cl.intro" /></div>
          <!-- 创建时间在右栏「班级信息」卡展示，名称卡不再重复（v0.7.2 验收反馈） -->
          <div v-if="!cl.joined && !cl.manageable" class="join-bar">
            <el-input
              v-model="joinCode"
              placeholder="输入邀请码加入班级"
              style="width: 240px"
              maxlength="8"
              @keyup.enter="join"
            />
            <el-button type="primary" :loading="joining" @click="join">加入班级</el-button>
          </div>
        </el-card>

        <div class="section-switch">
          <div class="soj-seg">
            <button type="button" :class="{ on: section === 'assignments' }" @click="section = 'assignments'">作业</button>
            <button type="button" :class="{ on: section === 'exams' }" @click="section = 'exams'">考试</button>
            <button v-if="cl.members" type="button" :class="{ on: section === 'members' }" @click="section = 'members'">学生</button>
          </div>
        </div>

        <el-card>
          <template v-if="section === 'assignments'">
            <el-alert
              v-if="!assignmentsVisible"
              title="加入班级后才能查看和完成作业"
              type="info"
              :closable="false"
            />
            <template v-else>
              <el-table
stripe
                :data="assignments"
                style="cursor: pointer"
                @row-click="(row: AssignmentListItem) => router.push(`/assignments/${row.id}`)"
              >
                <el-table-column prop="title" label="作业" min-width="220" />
                <el-table-column prop="intro" label="说明" min-width="220">
                  <template #default="{ row }">{{ row.intro || '-' }}</template>
                </el-table-column>
                <el-table-column prop="problem_count" label="题数" width="90" />
                <el-table-column label="布置时间" width="200">
                  <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
                </el-table-column>
              </el-table>
              <el-empty v-if="!assignments.length" description="还没有作业" :image-size="60" />
            </template>
          </template>

          <template v-else-if="section === 'exams'">
            <el-table
stripe
              v-if="examsVisible"
              :data="exams"
              style="cursor: pointer"
              @row-click="openExam"
            >
              <el-table-column label="状态" width="100">
                <template #default="{ row }">
                  <el-tag :type="row.status === 'running' ? 'success' : row.status === 'scheduled' ? 'warning' : 'info'">
                    {{ row.status === 'running' ? '进行中' : row.status === 'scheduled' ? '未开始' : '已结束' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="title" label="考试" min-width="200" />
              <el-table-column label="时间" min-width="280">
                <template #default="{ row }">
                  {{ new Date(row.start).toLocaleString('zh-CN', { hour12: false }) }} ~
                  {{ new Date(row.end).toLocaleString('zh-CN', { hour12: false }) }}
                </template>
              </el-table-column>
              <el-table-column prop="problem_count" label="题数" width="80" />
            </el-table>
            <el-alert v-else title="加入班级后才能查看考试" type="info" :closable="false" />
            <el-empty v-if="examsVisible && !exams.length" description="还没有考试" :image-size="60" />
          </template>

          <template v-else>
            <el-table stripe :data="cl.members">
              <el-table-column prop="uid" label="ID" width="100" />
              <el-table-column prop="username" label="用户名" min-width="160" />
              <el-table-column label="身份" width="200">
                <template #default="{ row }">
                  <el-tag v-if="row.is_owner" size="small" type="warning">建班人</el-tag>
                  <el-select
                    v-else-if="cl.manageable"
                    :model-value="row.role"
                    size="small"
                    @change="(v: string) => changeMemberRole(row as ClassMember, v)"
                  >
                    <el-option value="admin" label="班级管理员" />
                    <el-option value="member" label="普通成员" />
                  </el-select>
                  <el-tag v-else size="small" :type="row.role === 'admin' ? 'success' : 'info'">
                    {{ row.role === 'admin' ? '班级管理员' : '成员' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="加入时间" width="200">
                <template #default="{ row }">{{ fmtDate(row.joined_at) }}</template>
              </el-table-column>
            </el-table>
            <div v-if="cl.manageable" class="import">
              <el-input
                v-model="importText"
                type="textarea"
                :rows="2"
                placeholder="批量导入：输入已注册用户名，用空格 / 逗号 / 换行分隔"
              />
              <el-button type="primary" :loading="importing" @click="importMembers">导入成员</el-button>
            </div>
          </template>
        </el-card>

      <template #aside>
        <!-- 管理卡在信息卡上方（v0.7.2 验收调整）；v0.7.4 可见性/转让/解散收进编辑班级弹窗 -->
        <RailCard v-if="cl.manageable" ov="管理" title="班级管理" class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="showCreateAssignment = true">布置作业</el-button>
            <el-button style="width: 100%" @click="showExam = true">发起考试</el-button>
            <el-button style="width: 100%" @click="openEditClass">编辑班级</el-button>
            <el-button style="width: 100%" @click="copyInvite">复制邀请码</el-button>
            <el-button style="width: 100%" @click="regenInvite">重置邀请码</el-button>
            <el-button
              v-if="cl.leaveable"
              :disabled="leaving"
              type="danger"
              style="width: 100%"
              title="建班人在班级没有其他成员时可退出"
              @click="leave"
            >
              退出班级
            </el-button>
          </div>
        </RailCard>

        <RailCard v-else-if="cl.joined && cl.leaveable" ov="管理" title="班级管理" class="aside-card">
          <el-button type="danger" style="width: 100%" @click="leave">退出班级</el-button>
        </RailCard>

        <RailCard ov="信息" title="班级信息" class="aside-card">
          <div class="soj-kv">
            <div class="k"><b>{{ cl.members?.length ?? '—' }}</b><span>成员</span></div>
            <div class="k"><b>{{ assignmentsVisible ? assignments.length : '—' }}</b><span>作业</span></div>
            <div class="k"><b>{{ examsVisible ? exams.length : '—' }}</b><span>考试</span></div>
            <div class="k"><b class="kv-date">{{ fmtDate(cl.created_at) }}</b><span>创建</span></div>
          </div>
          <div class="inv-wrap">
            <span class="soj-inv-chip">{{ cl.invite_code }}</span>
          </div>
          <p class="hint center">邀请码可分享给学生入班，重置后旧码失效</p>
        </RailCard>

        <RailCard ov="说明" title="可见性" class="aside-card">
          <p class="hint">私密班仅成员可见；作业与考试加入班级后才能查看和完成。</p>
        </RailCard>
      </template>
    </PageShell>

    <el-dialog v-model="showCreateAssignment" title="布置作业" width="960" top="5vh">
      <el-form label-position="top">
        <el-form-item label="作业名称" required>
          <el-input v-model="assignmentForm.title" maxlength="64" placeholder="如：第三章 循环练习" />
        </el-form-item>
        <el-form-item label="作业说明">
          <el-input v-model="assignmentForm.intro" type="textarea" :rows="2" placeholder="选填" />
        </el-form-item>
        <el-form-item label="作业题目（可先创建后在作业页调整）">
          <ProblemPicker v-model="assignmentForm.picked" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateAssignment = false">取消</el-button>
        <el-button type="primary" :loading="creatingAssignment" @click="createAssignment">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showExam" title="发起班级考试" width="960" top="5vh">
      <el-form label-position="top">
        <el-form-item label="考试名称" required>
          <el-input v-model="examForm.title" maxlength="64" />
        </el-form-item>
        <el-form-item label="考试说明">
          <el-input v-model="examForm.intro" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="起止时间（最长 7 天）" required>
          <el-date-picker
            v-model="examForm.range"
            type="datetimerange"
            start-placeholder="开始"
            end-placeholder="结束"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="赛后隐藏期（天，0 为不隐藏；隐藏期内榜单与提交记录不公开）">
          <el-input-number v-model="examForm.hiddenDays" :min="0" :max="7" />
        </el-form-item>
        <el-form-item label="考试题目" required>
          <ProblemPicker v-model="examForm.picked" />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="examForm.fileIO"
            label="本场启用文件 IO"
            tip="场内所有题按各题文件名对以文件读写判题，题面下会展示文件 IO 说明；创建后可在考试详情管理卡随时开关，改动只影响之后的提交"
          />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="examForm.scoreboard"
            label="开启成绩表"
            tip="关闭后参与者看不到成绩表，组织者仍可见"
          />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="examForm.hideDuring"
            label="赛时隐藏成绩"
            tip="隐藏状态/标签/通过率，考中只显示「已提交」；关闭后考中即可查看结果"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showExam = false">取消</el-button>
        <el-button type="primary" :loading="creatingExam" @click="createExam">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showEditClass" title="编辑班级" width="960" top="5vh">
      <el-form label-position="top">
        <el-form-item label="班级名称" required>
          <el-input v-model="editClassForm.name" maxlength="64" />
        </el-form-item>
        <el-form-item label="班级公告">
          <MdEditor v-model="editClassForm.intro" height="150px" variant="compact" />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="editClassForm.private"
            :disabled="!isClassOwner"
            label="私密班"
            tip="公开班所有人可见；私密班仅成员与 root/admin 可见；仅建班人与 root 可切换，随保存生效"
          />
        </el-form-item>
      </el-form>

      <!-- 建班人 ∨ root：转让 / 解散收进编辑弹窗（v0.7.4），各自保留二次确认 -->
      <template v-if="isClassOwner">
        <el-divider />
        <div class="danger-row">
          <div class="danger-info">
            <div>转让班级</div>
            <div class="dim">选择一位成员成为新建班人，你将变为班级管理员</div>
          </div>
          <div class="danger-act">
            <el-select
              v-model="transferUid"
              filterable
              size="small"
              placeholder="选择受让人"
              style="width: 180px"
            >
              <el-option
                v-for="m in transferCandidates"
                :key="m.uid"
                :value="m.uid"
                :label="`#${m.uid} ${m.username}`"
              />
            </el-select>
            <el-button type="danger" :loading="transferring" :disabled="!transferUid" @click="doTransfer">
              转让
            </el-button>
          </div>
        </div>
        <div class="danger-row">
          <div class="danger-info">
            <div>解散班级</div>
            <div class="dim">班内全部考试与作业一并删除、所有成员移出，不可恢复</div>
          </div>
          <el-button type="danger" :loading="dissolving" @click="dissolve">解散</el-button>
        </div>
      </template>

      <template #footer>
        <el-button @click="showEditClass = false">取消</el-button>
        <el-button type="primary" :loading="savingClass" @click="saveEditClass">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* v3-full 实体大标题：藏蓝 28/850（本地 800，班级名） */
.title-row h3 {
  font-size: var(--soj-fs-entity-detail);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -0.4px;
  color: var(--soj-navy);
}

.intro {
  margin: 10px 0 4px;
  color: var(--el-text-color-regular);
}

.join-bar {
  display: flex;
  gap: 12px;
  margin-top: 16px;
}

.section-switch {
  margin: 16px 0;
}

.inv-wrap {
  display: flex;
  justify-content: center;
  margin-top: 14px;
}

.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: 8px 0 0;
}

.hint.center {
  text-align: center;
}

.kv-date {
  font-size: var(--soj-fs-16);
}

.info-line {
  display: flex;
  justify-content: space-between;
  padding: 4px 0;
  color: var(--el-text-color-regular);
}

.tip {
  margin-top: 8px;
}

/* 编辑班级弹窗危险区：转让/解散行，左说明右动作 */
.danger-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 0;
}

.danger-row + .danger-row {
  border-top: 1px dashed var(--el-border-color-lighter);
}

.danger-info > div:first-child {
  font-weight: var(--soj-fw-emphasis);
}

.danger-act {
  display: flex;
  align-items: center;
  gap: 8px;
}

.import {
  margin-top: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
</style>

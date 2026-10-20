<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { assignmentApi, classApi, examApi, problemApi } from '@/api'
import { dissolveClass } from '@/api/classManage'
import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import { fmtDate } from '@/utils/format'
import { useUserStore } from '@/stores/user'
import type { AssignmentListItem, ClassDetail, ClassMember } from '@/types'

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
const assignmentForm = reactive({ title: '', intro: '' })
const problemOptions = ref<{ id: number; title: string }[]>([])
const importText = ref('')
const importing = ref(false)
const togglingPrivate = ref(false)

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
  selectedProblems: [] as number[],
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
  if (!examForm.value.selectedProblems.length) {
    ElMessage.warning('请至少选择一道题')
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
      problem_ids: examForm.value.selectedProblems,
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

// 解散班级（v0.7.2，建班人 ∨ root）：班级仍挂有考试/作业时后端拒绝并提示
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

async function togglePrivate() {
  if (!cl.value) return
  togglingPrivate.value = true
  try {
    const r = await classApi.setPrivate(cl.value.id, !cl.value.private)
    cl.value.private = r.private
    ElMessage.success(r.private ? '已设为私密班（仅成员可见）' : '已设为公开班')
  } finally {
    togglingPrivate.value = false
  }
}

async function loadProblemOptions() {
  const r = await problemApi.list(1, 100)
  problemOptions.value = r.list
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
    const r = await assignmentApi.create(
      cl.value.id,
      assignmentForm.title.trim(),
      assignmentForm.intro.trim(),
    )
    ElMessage.success('作业创建成功，记得往里加题')
    showCreateAssignment.value = false
    assignmentForm.title = ''
    assignmentForm.intro = ''
    router.push(`/assignments/${r.id}`)
  } finally {
    creatingAssignment.value = false
  }
}

onMounted(() => {
  load()
  loadProblemOptions()
})
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
          <div v-if="cl.intro_html || cl.intro" class="intro"><MdView :content="cl.intro_html || cl.intro" /></div>
          <div class="meta">
            <el-tag size="small" type="info">创建于 {{ fmtDate(cl.created_at) }}</el-tag>
          </div>
          <div v-if="!cl.joined && !cl.manageable" class="join-bar">
            <el-input
              v-model="joinCode"
              placeholder="输入邀请码加入班级"
              style="width: 240px"
              maxlength="8"
              @keyup.enter="join"
            />
            <el-button type="primary" plain :loading="joining" @click="join">加入班级</el-button>
          </div>
        </el-card>

        <div class="section-switch">
          <el-radio-group v-model="section" size="large">
            <el-radio-button value="assignments">作业</el-radio-button>
            <el-radio-button value="exams">考试</el-radio-button>
            <el-radio-button v-if="cl.members" value="members">学生</el-radio-button>
          </el-radio-group>
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
        <el-card class="aside-card">
          <template #header><span>班级信息</span></template>
          <div class="info-line"><span>成员</span><b>{{ cl.members?.length ?? '—' }}</b></div>
          <div class="info-line"><span>作业</span><b>{{ assignmentsVisible ? assignments.length : '—' }}</b></div>
          <div class="info-line"><span>考试</span><b>{{ examsVisible ? exams.length : '—' }}</b></div>
          <div class="info-line"><span>创建时间</span><b class="small">{{ fmtDate(cl.created_at) }}</b></div>
        </el-card>

        <el-card v-if="cl.manageable" class="aside-card">
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="showCreateAssignment = true">布置作业</el-button>
            <el-button type="success" style="width: 100%" @click="showExam = true; loadProblemOptions()">发起考试</el-button>
            <el-button plain style="width: 100%" @click="copyInvite">复制邀请码：{{ cl.invite_code }}</el-button>
            <el-button type="warning" plain style="width: 100%" @click="regenInvite">重置邀请码</el-button>
            <el-button
              v-if="cl.leaveable"
              :disabled="leaving"
              type="danger"
              plain
              style="width: 100%"
              title="建班人在班级没有其他成员时可退出"
              @click="leave"
            >
              退出班级
            </el-button>
            <el-button
              v-if="cl.owner_id === userStore.uid || userStore.role === 'root'"
              :loading="dissolving"
              type="danger"
              style="width: 100%"
              @click="dissolve"
            >
              解散班级
            </el-button>
            <el-button
              :disabled="togglingPrivate"
              type="info"
              plain
              style="width: 100%"
              title="仅建班人与 root 可切换"
              @click="togglePrivate"
            >
              {{ cl.private ? '切换为公开班' : '切换为私密班' }}
            </el-button>
          </div>
          <div class="dim tip">公开班所有人可见；私密班仅成员与 root/admin 可见</div>
        </el-card>

        <el-card v-else-if="cl.joined && cl.leaveable" class="aside-card">
          <template #header><span>管理</span></template>
          <el-button type="danger" plain style="width: 100%" @click="leave">退出班级</el-button>
        </el-card>
        <el-card v-else-if="cl.joined" class="aside-card">
          <div class="dim tip">如需退出班级，请联系老师将你移出</div>
        </el-card>
      </template>
    </PageShell>

    <el-dialog v-model="showCreateAssignment" title="布置作业" width="560">
      <el-form label-position="top">
        <el-form-item label="作业名称" required>
          <el-input v-model="assignmentForm.title" maxlength="64" placeholder="如：第三章 循环练习" />
        </el-form-item>
        <el-form-item label="作业说明">
          <el-input v-model="assignmentForm.intro" type="textarea" :rows="2" placeholder="选填" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateAssignment = false">取消</el-button>
        <el-button type="primary" :loading="creatingAssignment" @click="createAssignment">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showExam" title="发起班级考试" width="560">
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
        <el-form-item label="赛后隐藏期（天，0 为不隐藏；隐藏期内榜单与提交记录不可见）">
          <el-input-number v-model="examForm.hiddenDays" :min="0" :max="7" />
        </el-form-item>
        <el-form-item label="考试题目（从题库多选）" required>
          <el-select v-model="examForm.selectedProblems" multiple filterable style="width: 100%">
            <el-option
              v-for="p in problemOptions"
              :key="p.id"
              :value="p.id"
              :label="`#${p.id} ${p.title}`"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <div class="fileio-switch">
            <el-switch v-model="examForm.fileIO" />
            <span>
              本场启用文件 IO
              <span class="dim">（场内所有题按各题文件名对以文件读写判题，题面下会展示文件 IO 说明；可随时开关）</span>
            </span>
          </div>
        </el-form-item>
        <el-form-item>
          <div class="fileio-switch">
            <el-switch v-model="examForm.scoreboard" />
            <span>开启成绩表 <span class="dim">（关闭后参与者看不到成绩表，组织者仍可见）</span></span>
          </div>
        </el-form-item>
        <el-form-item>
          <div class="fileio-switch">
            <el-switch v-model="examForm.hideDuring" />
            <span>赛期内对考生不可见 <span class="dim">（隐藏状态/标签/通过率）</span></span>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showExam = false">取消</el-button>
        <el-button type="primary" :loading="creatingExam" @click="createExam">创建</el-button>
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

.title-row h3 {
  font-size: var(--soj-fs-page-title);
  font-weight: var(--soj-fw-emphasis);
}

.intro {
  margin: 10px 0 4px;
  color: var(--el-text-color-regular);
}

.meta {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.join-bar {
  display: flex;
  gap: 12px;
  margin-top: 16px;
}

.section-switch {
  margin: 16px 0;
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

.import {
  margin-top: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
</style>

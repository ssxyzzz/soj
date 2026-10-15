<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { assignmentApi, classApi, examApi, problemApi } from '@/api'
import { fmtDate } from '@/utils/format'
import type { AssignmentListItem, ClassDetail, ClassMember } from '@/types'

const route = useRoute()
const router = useRouter()

const cl = ref<ClassDetail | null>(null)
const loading = ref(true)
const assignments = ref<AssignmentListItem[]>([])
const assignmentsVisible = ref(false)

const joinCode = ref('')
const joining = ref(false)
const showCreateAssignment = ref(false)
const creatingAssignment = ref(false)
const assignmentForm = reactive({ title: '', intro: '' })
const addPid = ref<number | null>(null)
const addingProblem = ref(false)
const problemOptions = ref<{ id: number; title: string }[]>([])
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
  selectedProblems: [] as number[],
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

async function leave() {
  if (!cl.value) return
  await classApi.leave(cl.value.id)
  ElMessage.success('已退出班级')
  await load()
}

async function regenInvite() {
  if (!cl.value) return
  const r = await classApi.regenInvite(cl.value.id)
  if (cl.value) cl.value.invite_code = r.invite_code
  ElMessage.success('已重置邀请码')
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
  <div v-loading="loading" class="page">
    <template v-if="cl">
      <el-card>
        <template #header>
          <div class="head">
            <span class="title">{{ cl.name }}</span>
            <span class="actions">
              <template v-if="cl.manageable">
                <el-button size="small" type="success" plain @click="showExam = true; loadProblemOptions()">
                  发起考试
                </el-button>
                <el-button size="small" type="primary" plain @click="showCreateAssignment = true">
                  布置作业
                </el-button>
                <el-button size="small" @click="copyInvite">复制邀请码：{{ cl.invite_code }}</el-button>
                <el-button size="small" type="warning" plain @click="regenInvite">重置邀请码</el-button>
              </template>
              <el-button v-else-if="cl.joined" size="small" type="danger" plain @click="leave">
                退出班级
              </el-button>
            </span>
          </div>
        </template>
        <p class="intro">{{ cl.intro || '暂无简介' }}</p>
        <div v-if="!cl.joined && !cl.manageable" class="join-bar">
          <el-input
            v-model="joinCode"
            placeholder="输入邀请码加入班级"
            style="width: 240px"
            maxlength="8"
          />
          <el-button type="primary" :loading="joining" @click="join">加入班级</el-button>
        </div>
      </el-card>

      <el-card class="block">
        <template #header>
          <div class="head">
            <span>班级作业（{{ assignments.length }}）</span>
          </div>
        </template>
        <el-alert
          v-if="!assignmentsVisible"
          title="加入班级后才能查看和完成作业"
          type="info"
          :closable="false"
        />
        <template v-else>
          <el-table
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
      </el-card>

      <el-card class="block">
        <template #header>
          <div class="head">
            <span>班级考试（{{ exams.length }}）</span>
          </div>
        </template>
        <el-table
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
        <el-empty v-if="!exams.length" description="还没有考试" :image-size="60" />
      </el-card>

      <el-card v-if="cl.members" class="block">
        <template #header>
          <div class="head">
            <span>成员（{{ cl.members.length }}）</span>
          </div>
        </template>
        <el-table :data="cl.members">
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
      </el-card>

      <el-dialog v-model="showCreateAssignment" title="布置作业" width="480">
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
        </el-form>
        <template #footer>
          <el-button @click="showExam = false">取消</el-button>
          <el-button type="primary" :loading="creatingExam" @click="createExam">创建</el-button>
        </template>
      </el-dialog>
    </template>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.title {
  font-weight: 600;
  font-size: 17px;
}

.actions {
  display: flex;
  gap: 8px;
}

.intro {
  margin: 0 0 8px;
  color: #606266;
}

.join-bar {
  display: flex;
  gap: 12px;
  margin-top: 16px;
}

.block {
  margin-top: 16px;
}

.import {
  margin-top: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
</style>

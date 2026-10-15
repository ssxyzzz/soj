<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { assignmentApi, problemApi } from '@/api'
import type { AssignmentDetail, AssignmentProblem, ProgressRow } from '@/types'

const route = useRoute()
const router = useRouter()

const assignment = ref<AssignmentDetail | null>(null)
const loading = ref(true)
const addPid = ref<number | null>(null)
const addingProblem = ref(false)
const problemOptions = ref<{ id: number; title: string }[]>([])
const progress = ref<ProgressRow[]>([])
const progressLoading = ref(false)

async function load() {
  loading.value = true
  try {
    assignment.value = await assignmentApi.detail(Number(route.params.id))
    if (assignment.value.manageable) {
      loadProgress()
    }
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

function problemRoute(row: { id: number; contest_id?: number; exam_id?: number }) {
  if (row.exam_id) {
    router.push(`/exams/${row.exam_id}`)
  } else if (row.contest_id) {
    router.push(`/contests/${row.contest_id}`)
  } else {
    router.push(`/problems/${row.id}`)
  }
}

async function loadProblemOptions() {
  const r = await problemApi.list(1, 100)
  problemOptions.value = r.list
}

async function addProblem() {
  if (!assignment.value || !addPid.value) {
    ElMessage.warning('请选择题目')
    return
  }
  addingProblem.value = true
  try {
    await assignmentApi.addProblem(assignment.value.id, addPid.value)
    ElMessage.success('已添加')
    addPid.value = null
    await load()
  } finally {
    addingProblem.value = false
  }
}

async function removeProblem(pid: number) {
  if (!assignment.value) return
  await assignmentApi.removeProblem(assignment.value.id, pid)
  ElMessage.success('已移除')
  await load()
}

async function removeAssignment() {
  if (!assignment.value) return
  await assignmentApi.remove(assignment.value.id)
  ElMessage.success('作业已删除')
  router.push(`/classes/${assignment.value.class_id}`)
}

onMounted(() => {
  load()
  loadProblemOptions()
})
</script>

<template>
  <div v-loading="loading" class="page">
    <template v-if="assignment">
      <el-page-header class="mb8" @back="router.push(`/classes/${assignment.class_id}`)">
        <template #content>
          <span>{{ assignment.title }}</span>
        </template>
        <template #extra>
          <el-button v-if="assignment.manageable" size="small" type="danger" plain @click="removeAssignment">
            删除作业
          </el-button>
        </template>
      </el-page-header>

      <el-card>
        <p v-if="assignment.intro" class="intro">{{ assignment.intro }}</p>
        <template v-if="assignment.manageable">
          <div class="add-problem">
            <el-select v-model="addPid" filterable placeholder="从题库选择题目加入作业" style="width: 280px">
              <el-option
                v-for="p in problemOptions"
                :key="p.id"
                :value="p.id"
                :label="`#${p.id} ${p.title}`"
              />
            </el-select>
            <el-button type="primary" :loading="addingProblem" @click="addProblem">添加</el-button>
          </div>
        </template>
      </el-card>

      <el-card class="block">
        <template #header><span>作业题目（{{ assignment.problems.length }}）</span></template>
        <el-table :data="assignment.problems">
          <el-table-column prop="id" label="ID" width="100" />
          <el-table-column label="题目">
            <template #default="{ row }">
              <el-link
                type="primary"
                @click="problemRoute(row as AssignmentProblem)"
              >
                {{ row.title }}
              </el-link>
              <el-tag v-if="row.contest_id" size="small" type="success" class="ml8">比赛中</el-tag>
              <el-tag v-if="row.exam_id" size="small" type="success" class="ml8">考试中</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="做题方式" width="160">
            <template #default="{ row }">
              <el-link
                v-if="row.contest_id || row.exam_id"
                type="warning"
                @click="problemRoute(row as AssignmentProblem)"
              >
                去{{ row.exam_id ? '考试' : '比赛' }}页提交
              </el-link>
              <span v-else class="dim">点题目直接做，即时出分</span>
            </template>
          </el-table-column>
          <el-table-column v-if="assignment.manageable" label="操作" width="100">
            <template #default="{ row }">
              <el-button size="small" type="danger" link @click="removeProblem((row as AssignmentProblem).id)">移除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!assignment.problems.length" description="作业里还没有题目" :image-size="60" />
      </el-card>

      <el-card v-if="assignment.manageable" class="block">
        <template #header>
          <div class="head">
            <span>完成情况（{{ progress.length }} 名学生）</span>
            <el-button size="small" :loading="progressLoading" @click="loadProgress">刷新</el-button>
          </div>
        </template>
        <el-table v-loading="progressLoading" :data="progress">
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
              <el-tag v-if="cellOf(row as ProgressRow, ap.id).solved" size="small" type="success">通过</el-tag>
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
    </template>
  </div>
</template>

<style scoped>
.mb8 {
  margin-bottom: 8px;
}

.mt8 {
  margin-top: 8px;
}

.ml8 {
  margin-left: 8px;
}

.block {
  margin-top: 12px;
}

.intro {
  margin: 0 0 8px;
  color: #606266;
}

.add-problem {
  display: flex;
  gap: 8px;
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.dim {
  color: #909399;
  font-size: 13px;
}

.cell {
  margin-right: 12px;
  font-size: 13px;
}
</style>

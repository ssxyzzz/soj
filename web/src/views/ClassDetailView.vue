<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { classApi, contestApi, problemApi } from '@/api'
import { fmtDate } from '@/utils/format'
import type { ClassDetail, ClassMember } from '@/types'

const route = useRoute()
const router = useRouter()

const cl = ref<ClassDetail | null>(null)
const loading = ref(true)
const joinCode = ref('')
const joining = ref(false)
const addPid = ref<number | null>(null)
const addHidden = ref(false)
const addingProblem = ref(false)
const problemOptions = ref<{ id: number; title: string }[]>([])
const importText = ref('')
const importing = ref(false)

// 办赛
const showContest = ref(false)
const creatingContest = ref(false)
const contestForm = ref({
  title: '',
  intro: '',
  range: [] as string[],
  hiddenDays: 0,
  selectedProblems: [] as number[],
})

async function load() {
  loading.value = true
  try {
    cl.value = await classApi.detail(Number(route.params.id))
  } catch {
    router.replace('/classes')
  } finally {
    loading.value = false
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

async function addProblem() {
  if (!cl.value || !addPid.value) {
    ElMessage.warning('请选择题目')
    return
  }
  addingProblem.value = true
  try {
    await classApi.addProblem(cl.value.id, addPid.value, addHidden.value)
    ElMessage.success(addHidden.value ? '已添加（仅本班成员可见）' : '已添加')
    addPid.value = null
    addHidden.value = false
    await load()
  } finally {
    addingProblem.value = false
  }
}

async function removeProblem(pid: number) {
  if (!cl.value) return
  await classApi.removeProblem(cl.value.id, pid)
  ElMessage.success('已移除')
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

function copyInvite() {
  if (cl.value?.invite_code) {
    navigator.clipboard?.writeText(cl.value.invite_code)
    ElMessage.success('邀请码已复制')
  }
}

async function changeMemberRole(row: { uid: number; username: string; is_owner: boolean }, role: string) {
  if (!cl.value) return
  await classApi.setMemberRole(cl.value.id, row.uid, role)
  ElMessage.success(`已把 ${row.username} 设为${role === 'admin' ? '班级管理员' : '普通成员'}`)
  await load()
}

async function createContest() {
  if (!cl.value) return
  if (!contestForm.value.title.trim() || contestForm.value.range.length !== 2) {
    ElMessage.warning('请填写比赛名称与起止时间')
    return
  }
  if (!contestForm.value.selectedProblems.length) {
    ElMessage.warning('请至少选择一道题')
    return
  }
  creatingContest.value = true
  try {
    const r = await contestApi.create({
      class_id: cl.value.id,
      title: contestForm.value.title.trim(),
      intro: contestForm.value.intro.trim(),
      start: new Date(contestForm.value.range[0]).toISOString(),
      end: new Date(contestForm.value.range[1]).toISOString(),
      hidden_days: contestForm.value.hiddenDays,
      problem_ids: contestForm.value.selectedProblems,
    })
    ElMessage.success('比赛创建成功')
    showContest.value = false
    router.push(`/contests/${r.id}`)
  } finally {
    creatingContest.value = false
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
                <el-button size="small" type="success" plain @click="showContest = true; loadProblemOptions()">
                  举办比赛
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
            <span>班级题目（{{ cl.problems.length }}）</span>
            <div v-if="cl.manageable" class="add-problem">
              <el-select
                v-model="addPid"
                filterable
                placeholder="从题库选择题目"
                style="width: 260px"
              >
                <el-option
                  v-for="p in problemOptions"
                  :key="p.id"
                  :value="p.id"
                  :label="`#${p.id} ${p.title}`"
                />
              </el-select>
              <el-checkbox v-model="addHidden" label="仅本班成员可见" />
              <el-button type="primary" :loading="addingProblem" @click="addProblem">添加</el-button>
            </div>
          </div>
        </template>
        <el-table :data="cl.problems">
          <el-table-column prop="id" label="ID" width="100" />
          <el-table-column label="题目">
            <template #default="{ row }">
              <el-link
                type="primary"
                @click="row.contest_id ? router.push(`/contests/${row.contest_id}`) : router.push(`/problems/${row.id}`)"
              >
                {{ row.title }}
              </el-link>
              <el-tag v-if="row.hidden" size="small" type="warning" class="hidden-tag">仅本班</el-tag>
              <el-tag v-if="row.contest_id" size="small" type="success" class="hidden-tag">比赛中</el-tag>
            </template>
          </el-table-column>
          <el-table-column v-if="cl.manageable" label="操作" width="100">
            <template #default="{ row }">
              <el-button size="small" type="danger" link @click="removeProblem(row.id)">移除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!cl.problems.length" description="还没有引用题目" :image-size="60" />
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

      <el-dialog v-model="showContest" title="举办班级比赛" width="560">
        <el-form label-position="top">
          <el-form-item label="比赛名称" required>
            <el-input v-model="contestForm.title" maxlength="64" />
          </el-form-item>
          <el-form-item label="简介">
            <el-input v-model="contestForm.intro" type="textarea" :rows="2" />
          </el-form-item>
          <el-form-item label="起止时间（最长 7 天）" required>
            <el-date-picker
              v-model="contestForm.range"
              type="datetimerange"
              start-placeholder="开始"
              end-placeholder="结束"
              style="width: 100%"
            />
          </el-form-item>
          <el-form-item label="赛后隐藏期（天，0 为不隐藏）">
            <el-input-number v-model="contestForm.hiddenDays" :min="0" :max="7" />
          </el-form-item>
          <el-form-item label="比赛题目（从题库多选）" required>
            <el-select v-model="contestForm.selectedProblems" multiple filterable style="width: 100%">
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
          <el-button @click="showContest = false">取消</el-button>
          <el-button type="primary" :loading="creatingContest" @click="createContest">创建</el-button>
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
  margin: 0;
  color: #606266;
  line-height: 1.7;
}

.join-bar {
  display: flex;
  gap: 12px;
  margin-top: 16px;
}

.block {
  margin-top: 16px;
}

.add-problem {
  display: flex;
  gap: 8px;
  align-items: center;
}

.hidden-tag {
  margin-left: 8px;
}

.import {
  margin-top: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
</style>

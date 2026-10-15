<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import type { AdminUser } from '@/types'

const list = ref<AdminUser[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)

const rateLimit = ref(3)
const savingSettings = ref(false)

const showCreate = ref(false)
const creating = ref(false)
const createForm = reactive({ username: '', password: '', nickname: '', role: 'default' })

async function load() {
  loading.value = true
  try {
    const r = await adminApi.listUsers(page.value, pageSize)
    list.value = r.list
    total.value = r.total
  } finally {
    loading.value = false
  }
}

async function loadSettings() {
  try {
    const s = await adminApi.getSettings()
    rateLimit.value = s.rate_limit_per_minute
  } catch {
    // 设置加载失败不阻塞用户管理
  }
}

async function saveSettings() {
  savingSettings.value = true
  try {
    await adminApi.updateSettings(rateLimit.value)
    ElMessage.success('设置已保存')
  } finally {
    savingSettings.value = false
  }
}

function onPageChange(p: number) {
  page.value = p
  load()
}

async function changeRole(row: AdminUser, role: string) {
  await adminApi.updateUser(row.id, { role })
  ElMessage.success(`已把 ${row.username} 的角色改为 ${role}`)
  load()
}

async function resetPassword(row: AdminUser) {
  const { value } = await ElMessageBox.prompt(`为 ${row.username} 设置新密码（至少 6 位）`, '重置密码', {
    inputPattern: /^.{6,64}$/,
    inputErrorMessage: '密码长度需在 6-64 之间',
  })
  await adminApi.updateUser(row.id, { password: value })
  ElMessage.success('密码已重置')
}

async function create() {
  if (!createForm.username || !createForm.password) {
    ElMessage.warning('请填写用户名和密码')
    return
  }
  creating.value = true
  try {
    await adminApi.createUser(
      createForm.username,
      createForm.password,
      createForm.nickname,
      createForm.role,
    )
    ElMessage.success('用户已创建')
    showCreate.value = false
    createForm.username = ''
    createForm.password = ''
    createForm.nickname = ''
    createForm.role = 'default'
    load()
  } finally {
    creating.value = false
  }
}

onMounted(() => {
  load()
  loadSettings()
})
</script>

<template>
  <div class="page">
    <el-card>
      <template #header>
        <div class="head">
          <span>用户管理（共 {{ total }} 人）</span>
          <el-button type="primary" size="small" @click="showCreate = true">新建用户</el-button>
        </div>
      </template>
      <el-table v-loading="loading" :data="list">
        <el-table-column prop="id" label="ID" width="90" />
        <el-table-column prop="username" label="用户名" min-width="160" />
        <el-table-column prop="nickname" label="昵称" min-width="160" />
        <el-table-column label="角色" width="200">
          <template #default="{ row }">
            <el-select :model-value="row.role" size="small" @change="(v: string) => changeRole(row as AdminUser, v)">
              <el-option value="default" label="默认 default" />
              <el-option value="admin" label="管理员 admin" />
              <el-option value="root" label="超级管理员 root" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button size="small" link type="warning" @click="resetPassword(row as AdminUser)">重置密码</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          background
          layout="prev, pager, next, total"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          @current-change="onPageChange"
        />
      </div>
    </el-card>

    <el-card class="block">
      <template #header><span>系统设置</span></template>
      <el-form label-position="left" label-width="200px" style="max-width: 480px">
        <el-form-item label="default 用户每分钟提交上限">
          <el-input-number v-model="rateLimit" :min="1" :max="60" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="savingSettings" @click="saveSettings">保存</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-dialog v-model="showCreate" title="新建用户" width="420">
      <el-form label-position="top">
        <el-form-item label="用户名" required>
          <el-input v-model="createForm.username" placeholder="3-32 位字母/数字/下划线" />
        </el-form-item>
        <el-form-item label="初始密码" required>
          <el-input v-model="createForm.password" placeholder="至少 6 位" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="createForm.nickname" placeholder="默认与用户名相同" />
        </el-form-item>
        <el-form-item label="角色">
          <el-radio-group v-model="createForm.role">
            <el-radio-button value="default">默认</el-radio-button>
            <el-radio-button value="admin">管理员</el-radio-button>
            <el-radio-button value="root">超级管理员</el-radio-button>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.block {
  margin-top: 16px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>

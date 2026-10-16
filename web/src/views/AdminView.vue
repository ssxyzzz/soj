<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import { markdownBytes } from '@/utils/vditor'
import type { AdminUser } from '@/types'

// 管理后台独立布局（v0.4.6）：左列功能卡点选切换，主列显示对应内容
const section = ref<'users' | 'notice' | 'settings'>('users')

const list = ref<AdminUser[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const keyword = ref('')

const rateLimit = ref(3)
const savingSettings = ref(false)
const homeNotice = ref('')
const savingNotice = ref(false)

const showCreate = ref(false)
const creating = ref(false)
const createForm = reactive({ username: '', password: '', nickname: '', role: 'default' })

async function load() {
  loading.value = true
  try {
    const r = await adminApi.listUsers(page.value, pageSize, keyword.value.trim() || undefined)
    list.value = r.list
    total.value = r.total
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

function resetSearch() {
  keyword.value = ''
  page.value = 1
  load()
}

async function loadSettings() {
  try {
    const s = await adminApi.getSettings()
    rateLimit.value = s.rate_limit_per_minute
    homeNotice.value = s.home_notice
  } catch {
    // 设置加载失败不阻塞用户管理
  }
}

async function saveNotice() {
  if (markdownBytes(homeNotice.value) > 64 << 10) {
    ElMessage.error('公告过长（上限 64KB）')
    return
  }
  savingNotice.value = true
  try {
    await adminApi.updateSettings({ home_notice: homeNotice.value })
    ElMessage.success('公告已保存，首页即刻生效')
  } finally {
    savingNotice.value = false
  }
}

async function saveSettings() {
  savingSettings.value = true
  try {
    await adminApi.updateSettings({ rate_limit_per_minute: rateLimit.value })
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
  <div class="admin-layout">
    <aside class="admin-nav">
      <div
        class="nav-item"
        :class="{ active: section === 'users' }"
        @click="section = 'users'"
      >
        <span class="nav-title">编辑用户</span>
        <span class="nav-sub">搜索 / 建号 / 角色 / 重置密码</span>
      </div>
      <div
        class="nav-item"
        :class="{ active: section === 'notice' }"
        @click="section = 'notice'"
      >
        <span class="nav-title">公告编辑</span>
        <span class="nav-sub">首页公告（Markdown）</span>
      </div>
      <div
        class="nav-item"
        :class="{ active: section === 'settings' }"
        @click="section = 'settings'"
      >
        <span class="nav-title">系统设置</span>
        <span class="nav-sub">提交限速等</span>
      </div>
    </aside>

    <main class="admin-main">
      <el-card v-if="section === 'users'">
        <template #header>
          <div class="head">
            <span>用户管理（共 {{ total }} 人）</span>
            <el-button type="primary" size="small" @click="showCreate = true">新建用户</el-button>
          </div>
        </template>
        <div class="search-row">
          <el-input
            v-model="keyword"
            placeholder="搜索用户（用户名 / 昵称）"
            clearable
            style="width: 280px"
            @keyup.enter="search"
            @clear="resetSearch"
          />
          <el-button type="primary" @click="search">搜索</el-button>
          <el-button v-if="keyword" @click="resetSearch">重置</el-button>
        </div>
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

      <el-card v-else-if="section === 'notice'">
        <template #header><span>首页公告（Markdown，留空则首页不展示）</span></template>
        <MdEditor v-model="homeNotice" height="300px" />
        <div class="notice-save">
          <el-button type="primary" :loading="savingNotice" @click="saveNotice">保存公告</el-button>
        </div>
      </el-card>

      <el-card v-else>
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
    </main>

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
.admin-layout {
  max-width: 1280px;
  margin: 0 auto;
  display: flex;
  gap: 24px;
  /* stretch：主列卡片随左列高度拉伸，两列上沿与下沿对齐；
     不用 sticky——切换分区时内容高度变化会让吸附列来回跳动 */
  align-items: stretch;
}

.admin-nav {
  flex: 0 0 300px;
  width: 300px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.nav-item {
  background: #f0f7ff;
  border: 1px solid #d9ecff;
  border-radius: 8px;
  padding: 14px 16px;
  cursor: pointer;
  transition: all 0.2s;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.nav-item:hover {
  border-color: #409eff;
}

.nav-item.active {
  background: #409eff;
  border-color: #409eff;
}

.nav-item.active .nav-title,
.nav-item.active .nav-sub {
  color: #fff;
}

.nav-title {
  font-weight: 600;
  font-size: 14px;
  color: #303133;
}

.nav-sub {
  font-size: 12px;
  color: #909399;
}

.admin-main {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 420px;
  display: flex;
  flex-direction: column;
}

.admin-main :deep(> .el-card) {
  flex: 1 1 auto;
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.search-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.notice-save {
  margin-top: 12px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

@media (max-width: 992px) {
  .admin-layout {
    flex-direction: column;
  }

  .admin-nav {
    position: static;
    width: auto;
    flex: 1 1 auto;
    flex-direction: row;
    flex-wrap: wrap;
  }

  .nav-item {
    flex: 1 1 140px;
  }
}
</style>

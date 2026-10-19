<script setup lang="ts">
// 用户管理面板（v0.5.4 ② 拆分自 AdminView，行为不变）：
// 单表 + 前端分组行渲染；搜索 200ms 防抖自动匹配、点选定位；default 学生组默认不显示。
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import { useAdminRolesStore } from '@/stores/adminRoles'
import type { AdminUser } from '@/types'

const roleStore = useAdminRolesStore()

const pageSize = 100
const keyword = ref('')
const pinned = ref<AdminUser | null>(null)
const loading = ref(false)

// 默认视图的组：default（学生组）不显示，除非搜索命中
const defaultViewGroups = computed(() => roleStore.roleGroups.filter((g) => g.id !== 'default'))

// 默认视图按组缓存（每组一次请求，整组展示）
const defaultGroups = reactive<Record<string, { list: AdminUser[]; total: number }>>({})

// 搜索视图：单次查询（不带角色过滤），前端分组渲染 + 统一分页
const search = reactive<{ page: number; pageSize: number; total: number; list: AdminUser[] }>({
  page: 1,
  pageSize: 100,
  total: 0,
  list: [],
})

const showCreate = ref(false)
const creating = ref(false)
const createForm = reactive({ username: '', password: '', nickname: '', role: 'default' })

async function loadDefaultView() {
  loading.value = true
  try {
    await Promise.all(
      defaultViewGroups.value.map(async (g) => {
        const r = await adminApi.listUsers(1, pageSize, undefined, g.id)
        defaultGroups[g.id] = { list: r.list, total: r.total }
      }),
    )
  } finally {
    loading.value = false
  }
}

// 退出定位回默认视图；有关键词则刷新搜索结果（翻页也走这里）
async function runSearch(page = 1) {
  const kw = keyword.value.trim()
  if (!kw) {
    pinned.value = null
    await loadDefaultView()
    return
  }
  loading.value = true
  try {
    const r = await adminApi.listUsers(page, search.pageSize, kw)
    search.page = page
    search.total = r.total
    search.list = r.list
  } finally {
    loading.value = false
  }
}

interface SuggestItem {
  value: string
  user: AdminUser
}

// 自动补全（200ms 防抖由 el-autocomplete 处理）：同一次请求同时喂下拉与表格
async function suggest(query: string, cb: (items: SuggestItem[]) => void) {
  const kw = query.trim()
  if (!kw) {
    pinned.value = null
    await loadDefaultView()
    cb([])
    return
  }
  // 输入变化即退出定位；点选后回填的用户名不触发退出
  if (pinned.value && kw !== pinned.value.username) pinned.value = null
  try {
    const r = await adminApi.listUsers(1, pageSize, kw)
    search.page = 1
    search.total = r.total
    search.list = r.list
    cb(r.list.slice(0, 20).map((u) => ({ value: u.username, user: u })))
  } catch {
    cb([])
  }
}

// el-autocomplete 的 select 回调参数是宽松的 Record 类型，这里收窄回 SuggestItem
function onSelect(item: Record<string, unknown>) {
  pinned.value = (item as unknown as SuggestItem).user
}

// ---- 单表行模型：分组行 / 提示行 / 用户行，前端渲染分组 ----
type UserRow =
  | { type: 'group'; key: string; label: string; meta?: string }
  | { type: 'hint'; key: string; label: string }
  | ({ type: 'user' } & AdminUser)

const tableRows = computed<UserRow[]>(() => {
  if (pinned.value) {
    const u = pinned.value
    return [
      { type: 'group', key: `g-${u.role}`, label: `${roleStore.roleName(u.role)}（${u.role}）`, meta: '已定位' },
      { type: 'user', ...u },
    ]
  }
  const kw = keyword.value.trim()
  if (kw) {
    const rows: UserRow[] = []
    for (const g of roleStore.roleGroups) {
      const users = search.list.filter((u) => u.role === g.id)
      if (!users.length) continue
      rows.push({
        type: 'group',
        key: `g-${g.id}`,
        label: `${g.name}（${g.id}）`,
        meta: `命中 ${users.length} 人`,
      })
      users.forEach((u) => rows.push({ type: 'user', ...u }))
    }
    return rows
  }
  const rows: UserRow[] = []
  for (const g of defaultViewGroups.value) {
    const d = defaultGroups[g.id]
    if (!d) continue
    rows.push({ type: 'group', key: `g-${g.id}`, label: `${g.name}（${g.id}）`, meta: `共 ${d.total} 人` })
    if (!d.list.length) rows.push({ type: 'hint', key: `h-${g.id}`, label: '该组暂无用户' })
    else {
      d.list.forEach((u) => rows.push({ type: 'user', ...u }))
      if (d.total > d.list.length)
        rows.push({
          type: 'hint',
          key: `h-${g.id}`,
          label: `该组共 ${d.total} 人，仅显示前 ${d.list.length} 人，可在上方搜索定位`,
        })
    }
  }
  return rows
})

function userSpanMethod({ row, columnIndex }: { row: UserRow; columnIndex: number }) {
  if (row.type !== 'user') {
    return columnIndex === 0 ? { rowspan: 1, colspan: 7 } : { rowspan: 0, colspan: 0 }
  }
  return { rowspan: 1, colspan: 1 }
}

// 展开行内容：用户所属角色的权限，按七类分组给出权限名（默认收起，点箭头展开）
function permSummaries(roleId: string) {
  const perms = roleStore.roles.find((r) => r.id === roleId)?.perms ?? []
  return roleStore.permGroups
    .map((g) => ({
      label: g.label,
      names: roleStore.catalog
        .filter((p) => p.group === g.id && perms.includes(p.id))
        .map((p) => p.label),
    }))
    .filter((g) => g.names.length)
}

function rowClassName({ row }: { row: UserRow }) {
  return row.type === 'user' ? '' : 'row-noexpand' // 分组/提示行不显示展开箭头
}

// 行内操作后按当前视图刷新：定位 → 原地更新；搜索 → 重查当前页；默认 → 重拉各组
async function refreshAfterMutation(userId: number, patch?: Partial<AdminUser>) {
  if (pinned.value?.id === userId && patch) {
    pinned.value = { ...pinned.value, ...patch }
  }
  if (keyword.value.trim()) await runSearch(search.page)
  else await loadDefaultView()
}

async function changeRole(row: AdminUser, role: string) {
  await adminApi.updateUser(row.id, { role })
  ElMessage.success(`已把 ${row.username} 的角色改为 ${roleStore.roleName(role)}（${role}）`)
  await refreshAfterMutation(row.id, { role })
}

// ---- 重置弹窗：真名 / 密码 / 确认密码（用户名是身份标识，后端不支持改，只读展示）----
const showEdit = ref(false)
const editSaving = ref(false)
const editForm = reactive({ id: 0, username: '', real_name: '', password: '', confirm: '' })

function openEdit(row: AdminUser) {
  editForm.id = row.id
  editForm.username = row.username
  editForm.real_name = row.real_name ?? ''
  editForm.password = ''
  editForm.confirm = ''
  showEdit.value = true
}

async function submitEdit() {
  if (editForm.password) {
    if (editForm.password.length < 6 || editForm.password.length > 64) {
      ElMessage.warning('密码长度需在 6-64 之间')
      return
    }
    if (editForm.password !== editForm.confirm) {
      ElMessage.warning('两次输入的密码不一致')
      return
    }
  }
  try {
    await ElMessageBox.confirm(`确认修改用户 ${editForm.username} 的信息？`, '确认修改', {
      type: 'warning',
      confirmButtonText: '确认修改',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  editSaving.value = true
  try {
    await adminApi.updateUser(editForm.id, {
      real_name: editForm.real_name,
      ...(editForm.password ? { password: editForm.password } : {}),
    })
    ElMessage.success('用户信息已修改')
    showEdit.value = false
    await refreshAfterMutation(editForm.id, { real_name: editForm.real_name })
  } finally {
    editSaving.value = false
  }
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
    await refreshAfterMutation(0)
  } finally {
    creating.value = false
  }
}

onMounted(async () => {
  await roleStore.ensure()
  await loadDefaultView()
})

defineExpose({ refresh: loadDefaultView })
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>用户管理</span>
        <el-button type="primary" size="small" @click="showCreate = true">新建用户</el-button>
      </div>
    </template>
    <div class="search-row">
      <el-autocomplete
        v-model="keyword"
        :fetch-suggestions="suggest"
        :debounce="200"
        clearable
        placeholder="搜索用户（用户名 / 昵称），输入后自动匹配"
        class="search-input"
        @select="onSelect"
      >
        <template #default="{ item }">
          <div class="sug-item">
            <span class="sug-username">{{ item.user.username }}</span>
            <span class="sug-nickname">{{ item.user.nickname }}</span>
            <el-tag size="small" type="info">{{ roleStore.roleName(item.user.role) }}</el-tag>
          </div>
        </template>
      </el-autocomplete>
      <el-tag v-if="pinned" closable @close="pinned = null">
        已定位：{{ pinned.username }}（{{ roleStore.roleName(pinned.role) }}）
      </el-tag>
    </div>

    <el-table
      v-loading="loading"
      :data="tableRows"
      size="small"
      :span-method="userSpanMethod"
      :row-class-name="rowClassName"
      class="user-table"
    >
      <el-table-column type="expand">
        <template #default="{ row }">
          <div v-if="row.type === 'user'" class="perm-expand">
            <template v-if="permSummaries(row.role).length">
              <div v-for="g in permSummaries(row.role)" :key="g.label" class="perm-expand-line">
                <span class="perm-expand-group">{{ g.label }}</span>
                <span>{{ g.names.join('、') }}</span>
              </div>
            </template>
            <span v-else class="dim">该角色暂无权限</span>
          </div>
          <div v-else-if="row.type === 'group'" class="row-group">
            <span>{{ row.label }}</span>
            <el-tag v-if="row.meta" size="small" type="info" class="ml8">{{ row.meta }}</el-tag>
          </div>
          <span v-else-if="row.type === 'hint'" class="row-hint">{{ row.label }}</span>
        </template>
      </el-table-column>
      <el-table-column label="ID" width="90">
        <template #default="{ row }">
          <span v-if="row.type === 'user'">{{ row.id }}</span>
        </template>
      </el-table-column>
      <el-table-column label="用户名" min-width="160">
        <template #default="{ row }">{{ row.type === 'user' ? row.username : '' }}</template>
      </el-table-column>
      <el-table-column label="昵称" min-width="160">
        <template #default="{ row }">{{ row.type === 'user' ? row.nickname : '' }}</template>
      </el-table-column>
      <el-table-column label="真实姓名" min-width="110">
        <template #default="{ row }">
          <span v-if="row.type === 'user'">{{ row.real_name || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="角色" width="170">
        <template #default="{ row }">
          <el-tooltip
            v-if="row.type === 'user'"
            :disabled="row.id !== 1"
            content="初始管理员（ID=1）的 root 角色受保护，不可修改"
            placement="top"
          >
            <el-select
              :model-value="row.role"
              size="small"
              style="width: 150px"
              :disabled="row.id === 1"
              @change="(v: string) => changeRole(row as AdminUser, v)"
            >
              <el-option
                v-for="r in roleStore.roles"
                :key="r.id"
                :value="r.id"
                :label="`${r.name}（${r.id}）`"
              />
            </el-select>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column label="重置" width="90">
        <template #default="{ row }">
          <el-button
            v-if="row.type === 'user'"
            size="small"
            link
            type="warning"
            @click="openEdit(row as AdminUser)"
          >
            重置
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="!pinned && keyword.trim() && search.total > search.pageSize" class="group-pager">
      <el-pagination
        background
        layout="prev, pager, next"
        :total="search.total"
        :page-size="search.pageSize"
        :current-page="search.page"
        @current-change="(p: number) => runSearch(p)"
      />
    </div>
    <el-empty
      v-if="!loading && !tableRows.length"
      :description="keyword ? `没有匹配「${keyword.trim()}」的用户` : '暂无用户'"
      :image-size="60"
    />

    <el-dialog v-model="showCreate" title="新建用户" width="560">
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
          <el-select v-model="createForm.role" style="width: 100%">
            <el-option
              v-for="r in roleStore.roles"
              :key="r.id"
              :value="r.id"
              :label="`${r.name}（${r.id}）`"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showEdit" :title="`重置 · ${editForm.username}`" width="520">
      <el-form label-position="top">
        <el-form-item label="用户名">
          <el-input v-model="editForm.username" disabled />
        </el-form-item>
        <el-form-item label="真实姓名">
          <el-input v-model="editForm.real_name" maxlength="32" placeholder="仅管理员后台可见；留空清除" />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input
            v-model="editForm.password"
            type="password"
            show-password
            placeholder="至少 6 位；留空表示不修改密码"
          />
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input v-model="editForm.confirm" type="password" show-password placeholder="再次输入新密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEdit = false">取消</el-button>
        <el-button type="primary" :loading="editSaving" @click="submitEdit">确认修改</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<style scoped>
.search-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.search-input {
  width: 360px;
}

.sug-item {
  display: flex;
  align-items: center;
  gap: 8px;
  line-height: 1.6;
}

.sug-username {
  font-weight: 600;
}

.sug-nickname {
  color: var(--el-text-color-secondary);
  flex: 1 1 auto;
}

/* 单表内的角色分组行 / 提示行 */
.row-group {
  display: flex;
  align-items: center;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.row-hint {
  color: var(--el-text-color-secondary);
}

/* 分组/提示行不显示展开箭头（展开行只有用户行用） */
.user-table :deep(.row-noexpand) .el-table__expand-icon {
  display: none;
}

/* 展开行：角色权限按分组陈列 */
.perm-expand {
  padding: 4px 12px;
}

.perm-expand-line {
  display: flex;
  gap: 8px;
  line-height: 1.9;
}

.perm-expand-group {
  flex: none;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.user-table :deep(.el-table__row) .row-group,
.user-table :deep(.el-table__row) .row-hint {
  padding-left: 2px;
}

.group-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
</style>

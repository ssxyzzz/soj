<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, Delete, Edit } from '@element-plus/icons-vue'

import { adminApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import { useUserStore } from '@/stores/user'
import { markdownBytes } from '@/utils/vditor'
import type { AdminUser, PermGroupInfo, PermInfo, RoleInfo } from '@/types'

const userStore = useUserStore()

// 管理后台独立布局（v0.4.6）：左列功能卡点选切换，主列显示对应内容。
// 角色管理（v0.4.7）仅 root 可见（后端同样硬编码 root，增删改不可下放）。
const section = ref<'users' | 'roles' | 'notice' | 'settings'>('users')

// ---- 用户管理（v0.4.10）：单表 + 前端分组行渲染 ----
// - 默认（无搜索词）：除 default（学生组，搜索命中才出现）外的各组照常显示，
//   整组拉取（后端单页上限 100，超出给出搜索提示行）；
// - 搜索：输入 200ms 防抖自动触发（el-autocomplete 内建防抖），不带角色过滤一次查询，
//   前端按角色分组渲染，未命中的组不显示；
// - 自动补全下拉点选：定位到该用户，列表只显示这一行；修改输入或清空即退出定位。
const pageSize = 100
const keyword = ref('')
const pinned = ref<AdminUser | null>(null)
const loading = ref(false)

const roles = ref<RoleInfo[]>([])
const catalog = ref<PermInfo[]>([])
const permGroups = ref<PermGroupInfo[]>([])

// 展示顺序：root、admin、自定义（按 id）、default（学生组最大放最后）
const roleGroups = computed<RoleInfo[]>(() => {
  const pick = (id: string) => roles.value.find((r) => r.id === id)
  const out: RoleInfo[] = []
  const root = pick('root')
  const admin = pick('admin')
  const def = pick('default')
  if (root) out.push(root)
  if (admin) out.push(admin)
  out.push(...roles.value.filter((r) => !r.builtin))
  if (def) out.push(def)
  return out
})

// 默认视图的组：default（学生组）不显示，除非搜索命中
const defaultViewGroups = computed(() => roleGroups.value.filter((g) => g.id !== 'default'))

// 默认视图按组缓存（每组一次请求，整组展示）
const defaultGroups = reactive<Record<string, { list: AdminUser[]; total: number }>>({})

// 搜索视图：单次查询（不带角色过滤），前端分组渲染 + 统一分页
const search = reactive<{ page: number; pageSize: number; total: number; list: AdminUser[] }>({
  page: 1,
  pageSize: 100,
  total: 0,
  list: [],
})

const rateLimit = ref(3)
const savingSettings = ref(false)
const homeNotice = ref('')
const savingNotice = ref(false)

const showCreate = ref(false)
const creating = ref(false)
const createForm = reactive({ username: '', password: '', nickname: '', role: 'default' })

// 角色管理：Hydro 风格二维矩阵（权限为行、角色为列），按权限分类插入分组行
const permDraft = reactive<Record<string, string[]>>({})
const showCreateRole = ref(false)
const creatingRole = ref(false)
const createRoleForm = reactive({ id: '', name: '', perms: [] as string[] })

function roleName(id: string): string {
  return roles.value.find((r) => r.id === id)?.name ?? id
}

async function loadRoles() {
  const r = await adminApi.listRoles()
  roles.value = r.list
  catalog.value = r.catalog
  // 兼容未带 groups 的旧后端响应
  permGroups.value = r.groups ?? []
  for (const role of r.list) {
    permDraft[role.id] = [...role.perms]
  }
}

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
function onSelect(item: Record<string, any>) {
  pinned.value = (item as SuggestItem).user
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
      { type: 'group', key: `g-${u.role}`, label: `${roleName(u.role)}（${u.role}）`, meta: '已定位' },
      { type: 'user', ...u },
    ]
  }
  const kw = keyword.value.trim()
  if (kw) {
    const rows: UserRow[] = []
    for (const g of roleGroups.value) {
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
    return columnIndex === 0 ? { rowspan: 1, colspan: 4 } : { rowspan: 0, colspan: 0 }
  }
  return { rowspan: 1, colspan: 1 }
}

// 行内操作后按当前视图刷新：定位 → 原地更新；搜索 → 重查当前页；默认 → 重拉各组
async function refreshAfterMutation(userId: number, patch?: Partial<AdminUser>) {
  if (pinned.value?.id === userId && patch) {
    pinned.value = { ...pinned.value, ...patch }
  }
  if (keyword.value.trim()) await runSearch(search.page)
  else await loadDefaultView()
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

async function changeRole(row: AdminUser, role: string) {
  await adminApi.updateUser(row.id, { role })
  ElMessage.success(`已把 ${row.username} 的角色改为 ${roleName(role)}（${role}）`)
  await refreshAfterMutation(row.id, { role })
}

async function resetPassword(row: AdminUser) {
  const { value } = await ElMessageBox.prompt(`为 ${row.username} 设置新密码（至少 6 位）`, '重置密码', {
    inputPattern: /^.{6,64}$/,
    inputErrorMessage: '密码长度需在 6-64 之间',
  })
  await adminApi.updateUser(row.id, { password: value })
  ElMessage.success('密码已重置')
}

async function editRealName(row: AdminUser) {
  const { value } = await ElMessageBox.prompt(
    `设置 ${row.username} 的真实姓名（仅管理员后台可见，用于班级名单对号；留空清除）`,
    '真实姓名',
    {
      inputValue: row.real_name ?? '',
      inputPattern: /^.{0,32}$/,
      inputErrorMessage: '最多 32 字符',
    },
  )
  await adminApi.updateUser(row.id, { real_name: value })
  ElMessage.success('真实姓名已保存')
  await refreshAfterMutation(row.id, { real_name: value })
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

// 点选即保存：先调接口，成功后才更新本地草稿；失败勾选自动回弹（拦截器已报错）
async function togglePerm(role: RoleInfo, perm: string) {
  const cur = permDraft[role.id] ?? []
  const next = cur.includes(perm) ? cur.filter((p) => p !== perm) : [...cur, perm]
  await adminApi.updateRole(role.id, { perms: next })
  permDraft[role.id] = next
  await loadRoles()
}

async function renameRole(role: RoleInfo) {
  const { value } = await ElMessageBox.prompt('修改角色名称', '改名', {
    inputValue: role.name,
    inputPattern: /^.{1,32}$/,
    inputErrorMessage: '名称需在 1-32 字符',
  })
  await adminApi.updateRole(role.id, { name: value })
  ElMessage.success('已改名')
  await loadRoles()
}

async function removeRole(role: RoleInfo) {
  await ElMessageBox.confirm(
    `确定删除自定义角色「${role.name}（${role.id}）」？删除后不可恢复。`,
    '删除角色',
    { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
  )
  await adminApi.deleteRole(role.id)
  ElMessage.success('角色已删除')
  await loadRoles()
  await loadDefaultView()
}

async function createRole() {
  if (!/^[a-z][a-z0-9_]{1,31}$/.test(createRoleForm.id)) {
    ElMessage.warning('角色 ID 需为 2-32 位小写字母/数字/下划线，且以字母开头')
    return
  }
  if (!createRoleForm.name) {
    ElMessage.warning('请填写角色名称')
    return
  }
  creatingRole.value = true
  try {
    await adminApi.createRole(createRoleForm.id, createRoleForm.name, createRoleForm.perms)
    ElMessage.success('角色已创建')
    showCreateRole.value = false
    createRoleForm.id = ''
    createRoleForm.name = ''
    createRoleForm.perms = []
    await loadRoles()
    await loadDefaultView()
  } finally {
    creatingRole.value = false
  }
}

// ---- 角色矩阵行模型：权限分类分组行 + 权限位行（空分类直接不显示） ----
type PermRow = { type: 'group'; label: string } | { type: 'perm'; perm: PermInfo }

const permRows = computed<PermRow[]>(() => {
  const rows: PermRow[] = []
  for (const g of permGroups.value) {
    const perms = catalog.value.filter((p) => p.group === g.id)
    if (!perms.length) continue
    rows.push({ type: 'group', label: g.label })
    perms.forEach((p) => rows.push({ type: 'perm', perm: p }))
  }
  return rows
})

function permSpanMethod({ row, columnIndex }: { row: PermRow; columnIndex: number }) {
  if (row.type === 'group') {
    return columnIndex === 0
      ? { rowspan: 1, colspan: 1 + roleGroups.value.length }
      : { rowspan: 0, colspan: 0 }
  }
  return { rowspan: 1, colspan: 1 }
}

// 新建角色弹窗：权限位按分类分块勾选（空分类不显示）
const nonEmptyPermGroups = computed(() =>
  permGroups.value.filter((g) => catalog.value.some((p) => p.group === g.id)),
)

function permsOfGroup(groupId: string): PermInfo[] {
  return catalog.value.filter((p) => p.group === groupId)
}

onMounted(async () => {
  // 无 admin.user.manage 权限位时拉角色清单会 403：角色表留空并继续初始化，
  // 保证有权限的面板（公告/系统设置）正常加载
  await loadRoles().catch(() => {})
  await loadDefaultView()
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
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'roles' }"
        @click="section = 'roles'"
      >
        <span class="nav-title">角色管理</span>
        <span class="nav-sub">权限位 / 自定义角色</span>
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
                <el-tag size="small" type="info">{{ roleName(item.user.role) }}</el-tag>
              </div>
            </template>
          </el-autocomplete>
          <el-tag v-if="pinned" closable @close="pinned = null">
            已定位：{{ pinned.username }}（{{ roleName(pinned.role) }}）
          </el-tag>
        </div>

        <el-table
          v-loading="loading"
          :data="tableRows"
          size="small"
          :span-method="userSpanMethod"
          class="user-table"
        >
          <el-table-column label="ID" width="90">
            <template #default="{ row }">
              <div v-if="row.type === 'group'" class="row-group">
                <span>{{ row.label }}</span>
                <el-tag v-if="row.meta" size="small" type="info" class="ml8">{{ row.meta }}</el-tag>
              </div>
              <span v-else-if="row.type === 'hint'" class="row-hint">{{ row.label }}</span>
              <span v-else>{{ row.id }}</span>
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
          <el-table-column label="操作" width="320">
            <template #default="{ row }">
              <template v-if="row.type === 'user'">
                <el-tooltip
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
                      v-for="r in roles"
                      :key="r.id"
                      :value="r.id"
                      :label="`${r.name}（${r.id}）`"
                    />
                  </el-select>
                </el-tooltip>
                <el-button size="small" link type="primary" @click="editRealName(row as AdminUser)">姓名</el-button>
                <el-button size="small" link type="warning" @click="resetPassword(row as AdminUser)">重置密码</el-button>
              </template>
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
      </el-card>

      <el-card v-else-if="section === 'roles'">
        <template #header>
          <div class="head">
            <span>角色管理</span>
            <el-button type="primary" size="small" @click="showCreateRole = true">新建角色</el-button>
          </div>
        </template>
        <el-alert
          class="role-alert"
          type="info"
          :closable="false"
          show-icon
          title="角色 = 一组权限位。按首页/题库/比赛/班级/排名/提交记录/管理后台分类展示（暂无权限位的分类不显示）。勾选即保存、对所有挂靠用户即刻生效；root 为代码级超角色不可修改（整列锁定）；删除自定义角色前需先改派其挂靠用户。"
        />

        <el-table :data="permRows" border size="small" class="perm-matrix" :span-method="permSpanMethod">
          <el-table-column label="权限位" width="200" fixed>
            <template #default="{ row }">
              <div v-if="row.type === 'group'" class="perm-group-row">{{ row.label }}</div>
              <el-tooltip v-else :content="row.perm.desc" placement="top">
                <span>{{ row.perm.label }}</span>
              </el-tooltip>
            </template>
          </el-table-column>
          <el-table-column
            v-for="r in roleGroups"
            :key="r.id"
            min-width="130"
            align="center"
          >
            <template #header>
              <div class="col-head">
                <div class="col-name">{{ r.name }}</div>
                <div class="col-meta">
                  <span class="muted">{{ r.user_count }} 人</span>
                  <el-tag v-if="r.builtin" size="small" :type="r.id === 'root' ? 'danger' : 'warning'">
                    {{ r.id === 'root' ? '锁定' : '内置' }}
                  </el-tag>
                  <template v-else>
                    <el-icon class="col-act" title="改名" @click="renameRole(r)"><Edit /></el-icon>
                    <el-tooltip
                      :disabled="r.user_count === 0"
                      content="仍有用户挂靠，请先改派后再删除"
                      placement="top"
                    >
                      <el-icon
                        class="col-act"
                        :class="{ 'col-act-disabled': r.user_count > 0 }"
                        title="删除角色"
                        @click="r.user_count === 0 && removeRole(r)"
                      ><Delete /></el-icon>
                    </el-tooltip>
                  </template>
                </div>
              </div>
            </template>
            <template #default="{ row }">
              <template v-if="row.type === 'perm'">
                <el-icon v-if="r.id === 'root'" class="root-check"><Check /></el-icon>
                <el-checkbox
                  v-else
                  :model-value="(permDraft[r.id] ?? []).includes(row.perm.id)"
                  @change="togglePerm(r, row.perm.id)"
                />
              </template>
            </template>
          </el-table-column>
        </el-table>
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
        <el-form label-position="left" label-width="220px" style="max-width: 520px">
          <el-form-item label="普通用户每分钟提交上限">
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
          <el-select v-model="createForm.role" style="width: 100%">
            <el-option
              v-for="r in roles"
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

    <el-dialog v-model="showCreateRole" title="新建自定义角色" width="620">
      <el-form label-position="top">
        <el-form-item label="角色 ID（创建后不可改）" required>
          <el-input v-model="createRoleForm.id" placeholder="如 backup_op / author（2-32 位小写字母/数字/下划线）" />
        </el-form-item>
        <el-form-item label="角色名称" required>
          <el-input v-model="createRoleForm.name" placeholder="如 备份员 / 出题老师" maxlength="32" />
        </el-form-item>
        <el-form-item label="权限位（创建后可随时调整）">
          <div class="perm-blocks">
            <div v-for="g in nonEmptyPermGroups" :key="g.id" class="perm-block">
              <div class="perm-block-title">{{ g.label }}</div>
              <el-checkbox-group v-model="createRoleForm.perms" class="perm-group">
                <el-checkbox v-for="p in permsOfGroup(g.id)" :key="p.id" :value="p.id" :title="p.desc">
                  {{ p.label }}
                </el-checkbox>
              </el-checkbox-group>
            </div>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateRole = false">取消</el-button>
        <el-button type="primary" :loading="creatingRole" @click="createRole">创建</el-button>
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
  color: #909399;
  font-size: 12px;
  flex: 1 1 auto;
}

/* 单表内的角色分组行 / 提示行 */
.row-group {
  display: flex;
  align-items: center;
  font-weight: 600;
  color: #303133;
}

.row-hint {
  color: #909399;
  font-size: 12px;
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

.ml8 {
  margin-left: 8px;
}

.muted {
  color: #909399;
  font-weight: 400;
  font-size: 12px;
}

.role-alert {
  margin-top: 4px;
}

.perm-matrix {
  margin-top: 12px;
}

.perm-matrix :deep(.el-table__cell) {
  vertical-align: middle;
}

/* 权限分类分组行 */
.perm-group-row {
  font-weight: 600;
  color: #303133;
  background: #fafcff;
  margin: -8px -16px;
  padding: 8px 16px;
}

.col-head {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  line-height: 1.4;
}

.col-name {
  font-weight: 600;
}

.col-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

.col-act {
  cursor: pointer;
  color: #909399;
  font-size: 14px;
}

.col-act:hover {
  color: #409eff;
}

.col-act-disabled {
  color: #c0c4cc;
  cursor: not-allowed;
}

.col-act-disabled:hover {
  color: #c0c4cc;
}

.root-check {
  color: #67c23a;
  font-size: 16px;
  font-weight: 700;
}

.perm-blocks {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}

.perm-block-title {
  font-weight: 600;
  font-size: 13px;
  color: #303133;
  margin-bottom: 4px;
}

.perm-group {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
}

.notice-save {
  margin-top: 12px;
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

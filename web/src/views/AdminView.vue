<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, Delete, Edit } from '@element-plus/icons-vue'

import { adminApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import { useUserStore } from '@/stores/user'
import { markdownBytes } from '@/utils/vditor'
import type { AdminUser, PermInfo, RoleInfo } from '@/types'

const userStore = useUserStore()

// 管理后台独立布局（v0.4.6）：左列功能卡点选切换，主列显示对应内容。
// 角色管理（v0.4.7）仅 root 可见（后端同样硬编码 root，增删改不可下放）。
const section = ref<'users' | 'roles' | 'notice' | 'settings'>('users')

// 用户按角色分组展示，分组随角色列表动态生成（root → admin → 自定义 → default）；
// 搜索关键词同时作用于所有组
const pageSize = 20
const keyword = ref('')
const loading = ref(false)

const roles = ref<RoleInfo[]>([])
const catalog = ref<PermInfo[]>([])

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

const groups = reactive<Record<string, { list: AdminUser[]; total: number; page: number }>>({})

const rateLimit = ref(3)
const savingSettings = ref(false)
const homeNotice = ref('')
const savingNotice = ref(false)

const showCreate = ref(false)
const creating = ref(false)
const createForm = reactive({ username: '', password: '', nickname: '', role: 'default' })

// 角色管理：Hydro 风格二维矩阵（权限为行、角色为列），点选即保存
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
  for (const role of r.list) {
    if (!groups[role.id]) groups[role.id] = { list: [], total: 0, page: 1 }
    permDraft[role.id] = [...role.perms]
  }
}

async function load() {
  loading.value = true
  try {
    await Promise.all(roleGroups.value.map((g) => loadGroup(g.id)))
  } finally {
    loading.value = false
  }
}

async function loadGroup(role: string) {
  const g = groups[role]
  const r = await adminApi.listUsers(g.page, pageSize, keyword.value.trim() || undefined, role)
  g.list = r.list
  g.total = r.total
}

function search() {
  for (const g of roleGroups.value) groups[g.id].page = 1
  load()
}

function resetSearch() {
  keyword.value = ''
  for (const g of roleGroups.value) groups[g.id].page = 1
  load()
}

function onGroupPage(role: string, p: number) {
  groups[role].page = p
  loadGroup(role)
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
  load()
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
  } finally {
    creatingRole.value = false
  }
}

onMounted(async () => {
  // 无 admin.user.manage 权限位时拉角色清单会 403：角色表留空并继续初始化，
  // 保证有权限的面板（公告/系统设置）正常加载
  await loadRoles().catch(() => {})
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

        <el-card
          v-for="g in roleGroups"
          :key="g.id"
          class="role-group"
          shadow="never"
        >
          <template #header>
            <div class="group-head">
              <span>
                {{ g.name }}（{{ g.id }}）
                <el-tag v-if="keyword" size="small" type="info" class="ml8">搜索命中 {{ groups[g.id].total }} 人</el-tag>
              </span>
            </div>
          </template>
          <el-table v-loading="loading" :data="groups[g.id].list" size="small">
            <el-table-column prop="id" label="ID" width="90" />
            <el-table-column prop="username" label="用户名" min-width="160" />
            <el-table-column prop="nickname" label="昵称" min-width="160" />
            <el-table-column label="操作" width="260">
              <template #default="{ row }">
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
                <el-button size="small" link type="warning" @click="resetPassword(row as AdminUser)">重置密码</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div v-if="groups[g.id].total > pageSize" class="group-pager">
            <el-pagination
              size="small"
              background
              layout="prev, pager, next"
              :total="groups[g.id].total"
              :page-size="pageSize"
              :current-page="groups[g.id].page"
              @current-change="(p: number) => onGroupPage(g.id, p)"
            />
          </div>
          <el-empty
            v-if="!groups[g.id].list.length"
            :description="keyword ? `没有匹配「${keyword}」的${g.name}` : `暂无${g.name}`"
            :image-size="40"
          />
        </el-card>
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
          title="角色 = 一组权限位。勾选即保存、对所有挂靠用户即刻生效；root 为代码级超角色不可修改（整列锁定）；删除自定义角色前需先改派其挂靠用户。"
        />

        <el-table :data="catalog" border size="small" class="perm-matrix">
          <el-table-column label="权限位" width="200" fixed>
            <template #default="{ row }">
              <el-tooltip :content="row.desc" placement="top">
                <span>{{ row.label }}</span>
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
              <el-icon v-if="r.id === 'root'" class="root-check"><Check /></el-icon>
              <el-checkbox
                v-else
                :model-value="(permDraft[r.id] ?? []).includes(row.id)"
                @change="togglePerm(r, row.id)"
              />
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
          <el-checkbox-group v-model="createRoleForm.perms" class="perm-group">
            <el-checkbox v-for="p in catalog" :key="p.id" :value="p.id" :title="p.desc">
              {{ p.label }}
            </el-checkbox>
          </el-checkbox-group>
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

.role-group {
  margin-top: 12px;
}

.role-group :deep(.el-card__header) {
  padding: 10px 16px;
  background: #fafcff;
}

.group-head {
  font-weight: 600;
  display: flex;
  justify-content: space-between;
  align-items: center;
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

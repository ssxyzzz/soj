<script setup lang="ts">
// 用户管理面板（v0.5.4 ② 拆分自 AdminView）：
// 按角色（权限位组合）分组展示全部用户，每组可点标题收起/展开（学生组默认收起）；
// 搜索 200ms 防抖自动匹配、点选定位，搜索视图命中的各组自动展开。
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import TierBadge from '@/components/TierBadge.vue'
import { useAdminRolesStore } from '@/stores/adminRoles'
import type { AdminUser, UserLevelInfo } from '@/types'

const roleStore = useAdminRolesStore()

const pageSize = 100
const keyword = ref('')
const pinned = ref<AdminUser | null>(null)
const loading = ref(false)

// 默认视图的组：全部角色按展示顺序（root → admin → 自定义 → default），学生组放在最后
const defaultViewGroups = computed(() => roleStore.roleGroups)

// 收起的分组：学生组默认收起（人数多、低频管理），其余默认展开
const collapsedGroups = ref<Set<string>>(new Set(['default']))

// 分组收起仅在默认视图生效：搜索视图命中的各组恒展开（结果一览无遗），
// 定位视图只有一人——两处分组头不显示折叠箭头、点击不改变收起状态
const canToggleGroups = computed(() => !pinned.value && !keyword.value.trim())

function toggleGroup(id: string) {
  if (!canToggleGroups.value) return
  const next = new Set(collapsedGroups.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsedGroups.value = next
}

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
  | { type: 'group'; key: string; id: string; label: string; meta?: string }
  | { type: 'hint'; key: string; label: string }
  | ({ type: 'user' } & AdminUser)

const tableRows = computed<UserRow[]>(() => {
  if (pinned.value) {
    const u = pinned.value
    return [
      { type: 'group', key: `g-${u.role}`, id: u.role, label: `${roleStore.roleName(u.role)}（${u.role}）`, meta: '已定位' },
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
        id: g.id,
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
    const folded = collapsedGroups.value.has(g.id)
    rows.push({
      type: 'group',
      key: `g-${g.id}`,
      id: g.id,
      label: `${g.name}（${g.id}）`,
      meta: folded ? `共 ${d.total} 人 · 已收起` : `共 ${d.total} 人`,
    })
    if (folded) continue
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

// 分组/提示行跨列：ID 列起跨满 4 列
function userSpanMethod({ row, columnIndex }: { row: UserRow; columnIndex: number }) {
  if (row.type !== 'user') {
    return columnIndex === 0
      ? { rowspan: 1, colspan: 4 }
      : { rowspan: 0, colspan: 0 }
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

// ---- 编辑弹窗：昵称 / 密码 / 确认密码（用户名是身份标识，后端不支持改，只读展示）----
const showEdit = ref(false)
const editSaving = ref(false)
const editForm = reactive({ id: 0, username: '', nickname: '', password: '', confirm: '' })

function openEdit(row: AdminUser) {
  editForm.id = row.id
  editForm.username = row.username
  editForm.nickname = row.nickname ?? ''
  editForm.password = ''
  editForm.confirm = ''
  showEdit.value = true
}

async function submitEdit() {
  if (!editForm.nickname.trim()) {
    ElMessage.warning('昵称不能为空')
    return
  }
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
      nickname: editForm.nickname.trim(),
      ...(editForm.password ? { password: editForm.password } : {}),
    })
    ElMessage.success('用户信息已修改')
    showEdit.value = false
    await refreshAfterMutation(editForm.id, { nickname: editForm.nickname.trim() })
  } finally {
    editSaving.value = false
  }
}

// ---- 经验调整弹窗：查看当前等级/经验，正数加、负数减（流水留审计）----
const showXp = ref(false)
const xpSaving = ref(false)
const xpTarget = reactive({ id: 0, username: '' })
const xpInfo = ref<UserLevelInfo | null>(null)
const xpForm = reactive({ delta: 100, reason: '' })

async function openXp(row: AdminUser) {
  xpTarget.id = row.id
  xpTarget.username = row.username
  xpInfo.value = null
  xpForm.delta = 100
  xpForm.reason = ''
  showXp.value = true
  try {
    xpInfo.value = await adminApi.getUserLevel(row.id)
  } catch {
    // 拉取失败弹窗内显示占位，仍允许提交调整
  }
}

async function submitXp() {
  if (!xpForm.delta) {
    ElMessage.warning('调整经验不能为 0（正数增加、负数减少）')
    return
  }
  const verb = xpForm.delta > 0 ? '增加' : '减少'
  try {
    await ElMessageBox.confirm(
      `确认为用户 ${xpTarget.username} ${verb} ${Math.abs(xpForm.delta)} 经验？`,
      '确认调整',
      { type: 'warning', confirmButtonText: '确认调整', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  xpSaving.value = true
  try {
    xpInfo.value = await adminApi.adjustUserXp(xpTarget.id, xpForm.delta, xpForm.reason.trim())
    ElMessage.success(`已为 ${xpTarget.username} ${verb} ${Math.abs(xpForm.delta)} 经验`)
    xpForm.delta = 100
    xpForm.reason = ''
  } finally {
    xpSaving.value = false
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
      class="user-table"
    >
      <el-table-column label="ID" width="90">
        <template #default="{ row }">
          <div
            v-if="row.type === 'group'"
            class="row-group"
            :class="{ 'is-toggleable': canToggleGroups }"
            :title="canToggleGroups ? (collapsedGroups.has(row.id) ? '点击展开该组用户' : '点击收起该组用户') : undefined"
            @click="toggleGroup(row.id)"
          >
            <span v-if="canToggleGroups" class="group-caret">{{ collapsedGroups.has(row.id) ? '▸' : '▾' }}</span>
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
      <el-table-column label="操作" width="130">
        <template #default="{ row }">
          <template v-if="row.type === 'user'">
            <el-button size="small" link type="warning" @click="openEdit(row as AdminUser)">编辑</el-button>
            <el-button size="small" link type="primary" @click="openXp(row as AdminUser)">经验</el-button>
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

    <el-dialog v-model="showEdit" :title="`编辑 · ${editForm.username}`" width="520">
      <el-form label-position="top">
        <el-form-item label="用户名">
          <el-input v-model="editForm.username" disabled />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="editForm.nickname" maxlength="32" placeholder="用于榜单、题解等公开页面展示" />
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

    <el-dialog v-model="showXp" :title="`调整经验 · ${xpTarget.username}`" width="520">
      <div class="xp-current">
        <template v-if="xpInfo">
          <TierBadge :tier="xpInfo.level" kind="level" size="md" />
          <span class="xp-nums">
            当前经验 <b>{{ xpInfo.xp }}</b>
            <template v-if="xpInfo.next_xp"> / 下一级 {{ xpInfo.next_xp }}</template>
            <template v-if="xpInfo.adjust">
              （含调整 {{ xpInfo.adjust > 0 ? '+' : '' }}{{ xpInfo.adjust }}）
            </template>
          </span>
        </template>
        <span v-else class="xp-loading">读取等级中…</span>
      </div>
      <el-form label-position="top">
        <el-form-item label="调整经验" required>
          <el-input-number v-model="xpForm.delta" :step="50" :step-strictly="false" style="width: 200px" />
          <span class="dim" style="margin-left: 8px">正数增加、负数减少；单次 ±1000000 以内</span>
        </el-form-item>
        <el-form-item label="备注（可选，随调整流水留档）">
          <el-input v-model="xpForm.reason" maxlength="100" show-word-limit placeholder="如：竞赛奖励 / 违规扣减" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showXp = false">关闭</el-button>
        <el-button type="primary" :loading="xpSaving" @click="submitXp">确认调整</el-button>
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
  font-weight: var(--soj-fw-emphasis);
}

.sug-nickname {
  color: var(--el-text-color-secondary);
  flex: 1 1 auto;
}

/* 单表内的角色分组行 / 提示行 */
.row-group {
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-text-color-primary);
  user-select: none;
}

/* 仅默认视图可折叠：分组头显示手型与悬停反馈 */
.row-group.is-toggleable {
  cursor: pointer;
}

.row-group.is-toggleable:hover {
  color: var(--el-color-primary);
}

/* 收起/展开指示：▾ 展开中 / ▸ 已收起 */
.group-caret {
  width: 14px;
  flex: none;
  text-align: center;
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-small);
}

.row-hint {
  color: var(--el-text-color-secondary);
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

/* 经验调整弹窗：当前等级徽章 + 经验数字 */
.xp-current {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}

.xp-nums b {
  font-weight: var(--soj-fw-emphasis);
}

.xp-loading {
  color: var(--el-text-color-secondary);
}
</style>

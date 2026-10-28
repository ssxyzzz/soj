<script setup lang="ts">
// 用户管理面板（v3-full 重制）：panel-h（标题 + 搜索胶囊 + 新建）+ 按角色分组的用户表
// （2026-10-27 拍板：普通用户组默认收起，其余展开；组头点击折叠）。
// 数据一次拉全量（循环分页，page_size 100）后本地分组/过滤——搜索零请求、组不会被分页切碎。
// 角色胶囊色：root 红 / admin 橙 / teacher 蓝 / 其余（自定义角色、default）灰。
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { UploadUserFile } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'

import { adminApi } from '@/api'
import { downloadFile } from '@/api/download'
import { verifyApi } from '@/api/verify'
import TierBadge from '@/components/TierBadge.vue'
import { useAdminRolesStore } from '@/stores/adminRoles'
import { useUserStore } from '@/stores/user'
import type { AdminUser, UserLevelInfo } from '@/types'

const roleStore = useAdminRolesStore()
const userStore = useUserStore()

const all = ref<AdminUser[]>([])
const keyword = ref('')
const loading = ref(false)

// 全量拉取（page_size 100 循环翻页；50 页兜底防异常死循环）
async function load() {
  loading.value = true
  try {
    const out: AdminUser[] = []
    for (let p = 1; p <= 50; p++) {
      const r = await adminApi.listUsers(p, 100)
      out.push(...r.list)
      if (out.length >= r.total || !r.list.length) break
    }
    all.value = out
  } finally {
    loading.value = false
  }
}

// 搜索本地过滤（用户名 / 昵称，与原服务端口径一致），零请求即时出结果
const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return all.value
  return all.value.filter(
    (u) => u.username.toLowerCase().includes(kw) || (u.nickname ?? '').toLowerCase().includes(kw),
  )
})

// 按角色分组：顺序沿用角色 store 的 roleGroups（root→admin→自定义→default 垫底），
// 不在 store 里的角色（已删自定义角色等）追加末尾；空组不显示
const groups = computed(() => {
  const byRole = new Map<string, AdminUser[]>()
  for (const u of filtered.value) {
    const arr = byRole.get(u.role)
    if (arr) arr.push(u)
    else byRole.set(u.role, [u])
  }
  const order = [
    ...roleStore.roleGroups.map((r) => r.id),
    ...[...byRole.keys()].filter((id) => !roleStore.roleGroups.some((r) => r.id === id)),
  ]
  return order
    .filter((id) => byRole.has(id))
    .map((id) => ({ role: id, name: roleStore.roleName(id), users: byRole.get(id)! }))
})

// 折叠态：未点过的角色按「default 收起、其余展开」的默认值；点击后记显式值
const expandedOverrides = reactive<Record<string, boolean>>({})
function isOpen(role: string): boolean {
  return expandedOverrides[role] ?? role !== 'default'
}
function toggle(role: string) {
  expandedOverrides[role] = !isOpen(role)
}

// 搜索时强制全部展开（折叠会藏住匹配项）；清空搜索恢复各自折叠态
const forceExpand = computed(() => !!keyword.value.trim())
const expanded = (role: string) => forceExpand.value || isOpen(role)

// v3 role-chip 色映射：root 红 / admin 橙 / teacher 蓝 / 其余灰
function roleClass(role: string): string {
  if (role === 'root') return 'role-root'
  if (role === 'admin') return 'role-admin'
  if (role === 'teacher') return 'role-teacher'
  return 'role-user'
}

// ---- 新建用户 ----
const showCreate = ref(false)
const creating = ref(false)
const createForm = reactive({ username: '', password: '', nickname: '', role: 'default', id: undefined as number | undefined })

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
      createForm.id,
    )
    ElMessage.success('用户已创建')
    showCreate.value = false
    createForm.username = ''
    createForm.password = ''
    createForm.nickname = ''
    createForm.role = 'default'
    createForm.id = undefined
    await load()
  } finally {
    creating.value = false
  }
}

// ---- 批量建号（CSV 表格导入，v0.24 小批）----
const showImport = ref(false)
const importing = ref(false)
const importFile = ref<UploadUserFile[]>([])
const importResult = ref<{ total: number; success: number; failed: { row: number; username: string; reason: string }[] } | null>(null)

function downloadTemplate() {
  downloadFile('/admin/users/import-template', 'SOJ批量建号模板.csv')
}

function onImportFile(file: UploadUserFile) {
  importFile.value = [file]
}

async function runImport() {
  const f = importFile.value[0]?.raw as File | undefined
  if (!f) {
    ElMessage.warning('请先选择 CSV 文件')
    return
  }
  importing.value = true
  importResult.value = null
  try {
    const fd = new FormData()
    fd.append('file', f)
    importResult.value = await adminApi.importUsers(fd)
    if (importResult.value.success > 0) {
      ElMessage.success(`批量建号完成：成功 ${importResult.value.success} 个`)
      await load()
    } else {
      ElMessage.warning('没有一个账号创建成功，请查看失败明细')
    }
  } finally {
    importing.value = false
  }
}

function resetImport() {
  importFile.value = []
  importResult.value = null
}

// ---- 编辑弹窗：用户名 / 昵称 / 密码 / 确认密码 ----
// 用户名是登录身份标识：持有 user.username.edit 权限位（默认仅 root）才可改，其余只读展示
const canRename = computed(() => userStore.can('user.username.edit'))
// 绑定邮箱解绑（v0.16）：root 硬编码（后端 RequireRoot）
const isRoot = computed(() => userStore.role === 'root')
const showEdit = ref(false)
const editSaving = ref(false)
const unbinding = ref(false)
// email/phone：绑定信息（v0.16），后端用户列表下发；AdminUser 类型不含，就地扩展
const editForm = reactive({
  id: 0,
  username: '',
  originUsername: '',
  nickname: '',
  password: '',
  confirm: '',
  email: '',
  phone: '',
})

function openEdit(row: AdminUser & { email?: string; phone?: string }) {
  editForm.id = row.id
  editForm.username = row.username
  editForm.originUsername = row.username
  editForm.nickname = row.nickname ?? ''
  editForm.password = ''
  editForm.confirm = ''
  editForm.email = row.email ?? ''
  editForm.phone = row.phone ?? ''
  showEdit.value = true
}

async function unbindEmail() {
  try {
    await ElMessageBox.confirm(
      `确认解绑用户 ${editForm.originUsername} 的邮箱 ${editForm.email}？（学生换邮箱场景，解绑后可重新绑定）`,
      '解绑邮箱',
      { type: 'warning', confirmButtonText: '解绑', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  unbinding.value = true
  try {
    await verifyApi.adminUnbindEmail(editForm.id)
    editForm.email = ''
    ElMessage.success('邮箱已解绑')
  } catch {
    // 拦截器统一提示
  } finally {
    unbinding.value = false
  }
}

async function submitEdit() {
  const renamed = canRename.value && editForm.username.trim() !== editForm.originUsername
  if (renamed && !/^[A-Za-z0-9_]{3,32}$/.test(editForm.username.trim())) {
    ElMessage.warning('用户名需为 3-32 位字母/数字/下划线')
    return
  }
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
    await ElMessageBox.confirm(`确认修改用户 ${editForm.originUsername} 的信息？`, '确认修改', {
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
      ...(renamed ? { username: editForm.username.trim() } : {}),
      ...(editForm.password ? { password: editForm.password } : {}),
    })
    ElMessage.success('用户信息已修改')
    showEdit.value = false
    await load()
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

onMounted(async () => {
  await roleStore.ensure()
  await load()
})

defineExpose({ refresh: () => load() })
</script>

<template>
  <el-card>
    <!-- v3 panel-h：标题 + 计数 + 右侧搜索胶囊 + 新建 -->
    <div class="panel-h panel-head">
      <span class="pt">编辑用户</span>
      <span class="dim count">共 {{ filtered.length }} 人</span>
      <div class="acts">
        <label class="soj-input head-search">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
          <input v-model="keyword" placeholder="搜索用户（用户名 / 昵称）" />
        </label>
        <el-button size="small" @click="showImport = true; resetImport()">批量导入</el-button>
        <el-button type="primary" size="small" @click="showCreate = true">新建用户</el-button>
      </div>
    </div>

    <!-- 按角色分组（2026-10-27 拍板）：组头点击折叠，default 组默认收起；搜索时强制展开 -->
    <template v-for="g in groups" :key="g.role">
      <button type="button" class="group-h" @click="toggle(g.role)">
        <span class="soj-role-chip" :class="roleClass(g.role)">{{ g.name }}</span>
        <span class="dim gcount">{{ g.users.length }} 人</span>
        <el-icon class="chev" :class="{ open: expanded(g.role) }"><ArrowDown /></el-icon>
      </button>
      <el-table v-if="expanded(g.role)" v-loading="loading" :data="g.users">
        <el-table-column label="ID" width="80">
          <template #default="{ row }">
            <span class="mono dim">{{ row.id }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="username" label="用户名" min-width="150">
          <template #default="{ row }">
            <span :class="{ 'username-bold': row.id === userStore.uid }">{{ row.username }}</span>
            <el-tag
              v-if="row.locked_until && new Date(row.locked_until) > new Date()"
              type="danger"
              size="small"
              class="ml8"
            >
              锁定中
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="昵称" min-width="150">
          <template #default="{ row }">
            <b class="nickname">{{ row.nickname }}</b>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="openEdit(row as AdminUser)">编辑</el-button>
            <el-button size="small" link type="primary" @click="openXp(row as AdminUser)">经验</el-button>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <el-empty
      v-if="!loading && !filtered.length"
      :description="keyword ? `没有匹配「${keyword.trim()}」的用户` : '暂无用户'"
      :image-size="60"
    />

    <!-- 批量建号（v0.24 小批）：下载模板 → 填表 → 上传 CSV；逐行 best-effort，失败行全量明细 -->
    <el-dialog v-model="showImport" title="批量导入用户" width="640">
      <el-alert
        title="三步：下载模板 → 按列填写（id 可留空自动分配，起点见系统设置-注册取号起点）→ 上传 CSV"
        type="info"
        :closable="false"
        style="margin-bottom: 14px"
      />
      <div class="import-actions">
        <el-button @click="downloadTemplate">⬇ 下载模板</el-button>
        <el-upload
          :file-list="importFile"
          accept=".csv"
          :auto-upload="false"
          :limit="1"
          :on-change="onImportFile"
          :on-remove="() => (importFile = [])"
          style="flex: 1"
        >
          <el-button>选择 CSV 文件</el-button>
        </el-upload>
      </div>
      <template v-if="importResult">
        <el-divider content-position="left">导入结果</el-divider>
        <p class="import-summary">
          共 {{ importResult.total }} 行 · 成功
          <b class="ok">{{ importResult.success }}</b> · 失败
          <b class="bad">{{ importResult.failed.length }}</b>
        </p>
        <el-table v-if="importResult.failed.length" :data="importResult.failed" size="small" max-height="240">
          <el-table-column label="表格行号" prop="row" width="90" />
          <el-table-column label="用户名" prop="username" width="150" show-overflow-tooltip />
          <el-table-column label="失败原因" prop="reason" min-width="180" show-overflow-tooltip />
        </el-table>
      </template>
      <template #footer>
        <el-button @click="showImport = false">关闭</el-button>
        <el-button type="primary" :loading="importing" @click="runImport">开始导入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showCreate" title="新建用户" width="560">
      <el-form label-position="top">
        <el-form-item label="用户名" required>
          <el-input v-model="createForm.username" placeholder="3-32 位字母/数字/下划线" />
        </el-form-item>
        <el-form-item label="初始密码" required>
          <el-input v-model="createForm.password" placeholder="至少 8 位" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="createForm.nickname" placeholder="默认与用户名相同" />
        </el-form-item>
        <el-form-item label="用户 ID">
          <el-input-number
            v-model="createForm.id"
            :min="1"
            :max="1000000000"
            :controls="false"
            placeholder="留空自动分配"
            style="width: 240px"
          />
          <span class="dim" style="margin-left: 8px">留空自动分配（起点见系统设置）；手动指定后后续自动取号会避开该号</span>
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

    <el-dialog v-model="showEdit" :title="`编辑 · ${editForm.originUsername}`" width="520">
      <el-form label-position="top">
        <el-form-item label="绑定信息">
          <div class="bind-row">
            <span :class="{ dim: !editForm.email }">
              邮箱：{{ editForm.email || '未绑定' }}<template v-if="editForm.phone"> · 手机：{{ editForm.phone }}</template>
            </span>
            <el-button
              v-if="isRoot && editForm.email"
              size="small"
              type="danger"
              :loading="unbinding"
              @click="unbindEmail"
            >
              解绑
            </el-button>
          </div>
        </el-form-item>
        <el-form-item label="用户名">
          <el-input
            v-model="editForm.username"
            :disabled="!canRename"
            :placeholder="canRename ? '3-32 位字母/数字/下划线' : '无修改用户名权限，只读'"
          />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="editForm.nickname" maxlength="6" placeholder="用于榜单、题解等公开页面展示（6 字以内）" />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input
            v-model="editForm.password"
            type="password"
            show-password
            placeholder="至少 8 位；留空表示不修改密码"
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
/* v3 panel-h 头部：标题 + 计数 + 右侧动作（搜索胶囊 + 新建） */
.panel-head {
  padding: 16px 20px;
}

.head-search {
  height: 36px;
  width: 260px;
  padding: 0 14px;
}

.count {
  font-size: var(--soj-fs-13);
}

.username-bold {
  font-weight: var(--soj-fw-bold);
}

/* 角色分组头（2026-10-27）：整行可点折叠，胶囊 + 人数 + 旋转箭头 */
.group-h {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 6px;
  border: 0;
  background: transparent;
  cursor: pointer;
  text-align: left;
}

.group-h:hover {
  background: var(--el-fill-color-light);
}

.gcount {
  font-size: var(--soj-fs-13);
}

.chev {
  margin-left: auto;
  color: var(--el-text-color-secondary);
  transition: transform 0.15s;
}

.chev.open {
  transform: rotate(180deg);
}

.nickname {
  color: var(--soj-navy);
  font-weight: var(--soj-fw-bold);
}

/* 经验调整弹窗：当前等级徽章 + 经验数字 */
.xp-current {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}

/* 绑定信息行（v0.16）：信息在左、root 解绑按钮在右 */
.bind-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
}

.xp-nums b {
  font-weight: var(--soj-fw-emphasis);
}

.xp-loading {
  color: var(--el-text-color-secondary);
}

/* 批量建号（v0.24 小批） */
.import-actions {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.import-summary {
  font-size: 14px;
  margin: 0 0 10px;
}

.import-summary .ok {
  color: var(--el-color-success);
}

.import-summary .bad {
  color: var(--el-color-danger);
}
</style>

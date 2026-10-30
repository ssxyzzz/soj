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
import type { AdminUser, UserImportRow, UserLevelInfo } from '@/types'
import { GRADE_OPTIONS } from '@/utils/grades'

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

// ---- 批量建号·中文花名册（v0.34 两段式）：上传识别 → 预览确认 → 建号导出 ----
const showImport = ref(false)
const importStep = ref(1) // 1 上传 / 2 预览 / 3 结果
const importing = ref(false)
const importFile = ref<UploadUserFile[]>([])
const previewRows = ref<UserImportRow[]>([])
const importResult = ref<{ total: number; success: number; failed: { row: number; username: string; reason: string }[] } | null>(null)

const importValid = computed(() => previewRows.value.filter((r) => !r.problems?.length))

// 问题行标红底（el-table row-class-name）
function importRowClass({ row }: { row: UserImportRow }) {
  return row.problems?.length ? 'import-bad-row' : ''
}

function downloadTemplate() {
  downloadFile('/admin/users/import-template', 'SOJ批量建号模板.csv')
}

function onImportFile(file: UploadUserFile) {
  importFile.value = [file]
}

async function runPreview() {
  const f = importFile.value[0]?.raw as File | undefined
  if (!f) {
    ElMessage.warning('请先选择 CSV 文件')
    return
  }
  importing.value = true
  try {
    const fd = new FormData()
    fd.append('file', f)
    const r = await adminApi.importUsersPreview(fd)
    previewRows.value = r.rows
    importStep.value = 2
    if (!r.rows.length) ElMessage.warning('表格里没有识别到任何账号行')
  } finally {
    importing.value = false
  }
}

async function runCommit() {
  if (!importValid.value.length) {
    ElMessage.warning('没有可导入的行（全部存在问题，请修正后重新上传）')
    return
  }
  try {
    await ElMessageBox.confirm(
      `确认为 ${importValid.value.length} 个账号建号？初始密码在结果页导出后发放给学生。`,
      '确认建号',
      { type: 'warning', confirmButtonText: '确认建号', cancelButtonText: '返回预览' },
    )
  } catch {
    return
  }
  importing.value = true
  try {
    importResult.value = await adminApi.importUsersCommit(importValid.value)
    importStep.value = 3
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

// 导出已建账号（用户名/ID/姓名/初始密码；只导成功的行——按行号从失败清单排除）
function exportAccounts() {
  if (!importResult.value) return
  const failedRows = new Set(importResult.value.failed.map((f) => f.row))
  const created = importValid.value.filter((r) => !failedRows.has(r.row))
  if (!created.length) {
    ElMessage.warning('本次没有成功建号的账号')
    return
  }
  const esc = (s: string | number) => {
    const v = String(s ?? '')
    return /[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v
  }
  const lines = [
    '# 初始密码仅此一次导出，请妥善保管发放；学生首次登录后可自行修改',
    'username,id,姓名,初始密码',
    ...created.map((r) => [r.username, r.id, r.name, r.password].map(esc).join(',')),
  ]
  // 带 BOM：Excel 直开不乱码
  const blob = new Blob(['\uFEFF' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `SOJ建号_${new Date().toISOString().slice(0, 10)}.csv`
  a.click()
  URL.revokeObjectURL(a.href)
}

function resetImport() {
  importFile.value = []
  previewRows.value = []
  importResult.value = null
  importStep.value = 1
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
  gender: '',
  grade: '',
  school: '',
  region: '',
})

function openEdit(row: AdminUser) {
  editForm.id = row.id
  editForm.username = row.username
  editForm.originUsername = row.username
  editForm.nickname = row.nickname ?? ''
  editForm.password = ''
  editForm.confirm = ''
  editForm.email = row.email ?? ''
  editForm.phone = row.phone ?? ''
  editForm.gender = row.gender ?? ''
  editForm.grade = row.grade ?? ''
  editForm.school = row.school ?? ''
  editForm.region = row.region ?? ''
  showEdit.value = true
  openXp(row) // 经验/玉调整并入编辑弹窗（2026-10-30 拍板）：打开即带出当前值
  openJade(row)
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
      gender: editForm.gender,
      grade: editForm.grade,
      school: editForm.school.trim(),
      region: editForm.region.trim(),
    })
    ElMessage.success('用户信息已修改')
    showEdit.value = false
    await load()
  } finally {
    editSaving.value = false
  }
}

// ---- 经验调整（并入编辑弹窗，2026-10-30）：查看当前等级/经验，正数加、负数减（流水留审计）----
const xpSaving = ref(false)
const xpTarget = reactive({ id: 0, username: '' })
const xpInfo = ref<UserLevelInfo | null>(null)
const xpForm = reactive({ delta: 100, reason: '' })

function openXp(row: AdminUser) {
  xpTarget.id = row.id
  xpTarget.username = row.username
  xpInfo.value = null
  xpForm.delta = 100
  xpForm.reason = ''
  adminApi
    .getUserLevel(row.id)
    .then((info) => (xpInfo.value = info))
    .catch(() => {
      // 拉取失败弹窗内显示占位，仍允许提交调整
    })
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

// ---- 玉调整（v0.33-①，root 专属；并入编辑弹窗，2026-10-30）：查当前余额，正加负减（流水留审计） ----
const jadeSaving = ref(false)
const jadeTarget = reactive({ id: 0, username: '' })
const jadeBalance = ref<number | null>(null)
const jadeForm = reactive({ delta: 10, reason: '' })
const canJade = userStore.role === 'root'

function openJade(row: AdminUser) {
  jadeTarget.id = row.id
  jadeTarget.username = row.username
  jadeBalance.value = null
  jadeForm.delta = 10
  jadeForm.reason = ''
  if (!canJade) return
  adminApi
    .getUserJade(row.id)
    .then((r) => (jadeBalance.value = r.balance))
    .catch(() => {
      // 拉取失败弹窗内显示占位，仍允许提交调整
    })
}

async function submitJade() {
  if (!jadeForm.delta) {
    ElMessage.warning('调整值不能为 0（正数增加、负数减少）')
    return
  }
  if (!jadeForm.reason.trim()) {
    ElMessage.warning('请填写调整原因（随流水留审计）')
    return
  }
  const verb = jadeForm.delta > 0 ? '增加' : '减少'
  try {
    await ElMessageBox.confirm(
      `确认为用户 ${jadeTarget.username} ${verb} ${Math.abs(jadeForm.delta)} 玉？`,
      '确认调整',
      { type: 'warning', confirmButtonText: '确认调整', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  jadeSaving.value = true
  try {
    const r = await adminApi.adjustUserJade(jadeTarget.id, jadeForm.delta, jadeForm.reason.trim())
    jadeBalance.value = r.balance
    ElMessage.success(`已为 ${jadeTarget.username} ${verb} ${Math.abs(jadeForm.delta)} 玉`)
    jadeForm.delta = 10
    jadeForm.reason = ''
  } catch {
    // 拦截器统一提示（余额不足等）
  } finally {
    jadeSaving.value = false
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
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="openEdit(row as AdminUser)">编辑</el-button>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <el-empty
      v-if="!loading && !filtered.length"
      :description="keyword ? `没有匹配「${keyword.trim()}」的用户` : '暂无用户'"
      :image-size="60"
    />

    <!-- 批量建号·中文花名册（v0.34 两段式）：上传识别 → 预览确认（id/用户名可改）→ 建号导出 -->
    <el-dialog v-model="showImport" title="批量导入用户" width="920" @closed="resetImport">
      <el-steps :active="importStep" align-finish style="margin-bottom: 16px">
        <el-step title="上传花名册" />
        <el-step title="预览确认" />
        <el-step title="建号完成" />
      </el-steps>

      <!-- 第一步：下载模板 → 填表 → 上传识别 -->
      <template v-if="importStep === 1">
        <el-alert
          title="下载模板 → 填写花名册（id、姓名必填；性别/年级/学校/地区/角色选填）→ 上传识别"
          type="info"
          :closable="false"
          style="margin-bottom: 14px"
        />
        <p class="dim" style="margin: 0 0 10px">
          用户名默认 = 姓名拼音 + id（张三 1001 → zhangsan1001，多音字可在预览里改）；初始密码每人随机生成，
          建号后导出表格发放；id 即站内用户 ID，可留空自动顺延分配（预览表可见可改，与已占用号冲突会标红提示）。
          支持 GBK/UTF-8 编码（Excel 直接另存的 CSV 可用）。
        </p>
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
      </template>

      <!-- 第二步：识别结果预览（问题行标红禁选；id/用户名可改，改完即刻生效于建号） -->
      <template v-else-if="importStep === 2">
        <p class="import-summary">
          共识别 {{ previewRows.length }} 行 · 可导入
          <b class="ok">{{ importValid.length }}</b> · 有问题
          <b class="bad">{{ previewRows.length - importValid.length }}</b>
          <span class="dim">（问题行不会建号；id 与用户名可直接修改纠错）</span>
        </p>
        <el-table :data="previewRows" size="small" max-height="380" :row-class-name="importRowClass">
          <el-table-column label="行号" prop="row" width="64" />
          <el-table-column label="ID" width="130">
            <template #default="{ row }">
              <el-input v-model.number="row.id" size="small" :disabled="!!row.problems?.length" style="width: 78px" />
              <el-tag v-if="row.auto_id" size="small" type="info" style="margin-left: 4px">自动</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="姓名" prop="name" width="90" show-overflow-tooltip />
          <el-table-column label="性别" prop="gender" width="56" />
          <el-table-column label="年级" prop="grade" width="70" show-overflow-tooltip />
          <el-table-column label="学校" prop="school" min-width="110" show-overflow-tooltip />
          <el-table-column label="用户名" min-width="150">
            <template #default="{ row }">
              <el-input v-if="!row.problems?.length" v-model="row.username" size="small" />
              <span v-else>{{ row.username || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="初始密码" prop="password" width="110" />
          <el-table-column label="问题" min-width="150">
            <template #default="{ row }">
              <el-tag v-for="p in row.problems" :key="p" type="danger" size="small" style="margin-right: 4px">
                {{ p }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </template>

      <!-- 第三步：建号结果 + 账号导出 -->
      <template v-else>
        <p class="import-summary">
          共 {{ importResult?.total ?? 0 }} 行 · 成功
          <b class="ok">{{ importResult?.success ?? 0 }}</b> · 失败
          <b class="bad">{{ importResult?.failed.length ?? 0 }}</b>
        </p>
        <el-button type="success" :disabled="!importResult?.success" @click="exportAccounts">
          ⬇ 导出已建账号（用户名 / ID / 姓名 / 初始密码）
        </el-button>
        <el-table v-if="importResult?.failed.length" :data="importResult.failed" size="small" max-height="240" style="margin-top: 10px">
          <el-table-column label="表格行号" prop="row" width="90" />
          <el-table-column label="用户名" prop="username" width="180" show-overflow-tooltip />
          <el-table-column label="失败原因" prop="reason" min-width="200" show-overflow-tooltip />
        </el-table>
      </template>

      <template #footer>
        <el-button @click="showImport = false">关闭</el-button>
        <el-button v-if="importStep === 2" @click="importStep = 1">上一步</el-button>
        <el-button v-if="importStep === 1" type="primary" :loading="importing" @click="runPreview">识别表格</el-button>
        <el-button v-else-if="importStep === 2" type="primary" :loading="importing" @click="runCommit">
          确认建号（{{ importValid.length }} 个）
        </el-button>
        <el-button v-else type="primary" @click="resetImport">再导一批</el-button>
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

    <!-- 编辑弹窗（2026-10-30 拍板加宽 520→720）：账号信息 + 经验调整 + 玉调整（root）三段合一 -->
    <el-dialog v-model="showEdit" :title="`编辑 · ${editForm.originUsername}`" width="720">
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
        <div class="edit-grid">
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
          <el-form-item label="性别">
            <el-select v-model="editForm.gender" clearable placeholder="未填写" style="width: 160px">
              <el-option value="男" label="男" />
              <el-option value="女" label="女" />
            </el-select>
          </el-form-item>
          <el-form-item label="年级">
            <el-select v-model="editForm.grade" clearable placeholder="未设置" style="width: 160px">
              <el-option v-for="g in GRADE_OPTIONS" :key="g" :value="g" :label="g" />
            </el-select>
          </el-form-item>
          <el-form-item label="学校">
            <el-input v-model="editForm.school" maxlength="64" placeholder="选填" />
          </el-form-item>
          <el-form-item label="地区">
            <el-input v-model="editForm.region" maxlength="32" placeholder="选填" />
          </el-form-item>
        </div>
        <p class="dim" style="margin: -6px 0 0">
          资料修改不受学生端「资料自改开关」限制；年级每年 9 月 1 日自动晋级（高一升高二…高三→已毕业）。
        </p>
      </el-form>

      <!-- 经验调整：当前等级徽章 + 正加负减 + 可选备注，独立提交 -->
      <el-divider content-position="left">经验调整</el-divider>
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
      <div class="adj-row">
        <el-input-number v-model="xpForm.delta" :step="50" :step-strictly="false" style="width: 150px" />
        <el-input
          v-model="xpForm.reason"
          maxlength="100"
          show-word-limit
          placeholder="备注（可选，随调整流水留档，如：竞赛奖励 / 违规扣减）"
        />
        <el-button type="primary" :loading="xpSaving" @click="submitXp">调整经验</el-button>
      </div>
      <p class="adj-hint dim">正数增加、负数减少；单次 ±1000000 以内</p>

      <!-- 玉调整（v0.33-①，root）：当前余额 + 正加负减 + 必填原因，独立提交 -->
      <template v-if="canJade">
        <el-divider content-position="left">玉调整</el-divider>
        <div class="xp-current">
          <span v-if="jadeBalance !== null" class="xp-nums">当前余额 <b>{{ jadeBalance }}</b> 玉</span>
          <span v-else class="xp-loading">读取余额中…</span>
        </div>
        <div class="adj-row">
          <el-input-number v-model="jadeForm.delta" :step="10" :step-strictly="false" style="width: 150px" />
          <el-input
            v-model="jadeForm.reason"
            maxlength="200"
            show-word-limit
            placeholder="原因（必填，随流水留审计，如：比赛奖励 / 兑现 / 违规扣减）"
          />
          <el-button type="success" :loading="jadeSaving" @click="submitJade">调整玉</el-button>
        </div>
        <p class="adj-hint dim">正数增加、负数减少（扣减不透支）</p>
      </template>

      <template #footer>
        <el-button @click="showEdit = false">取消</el-button>
        <el-button type="primary" :loading="editSaving" @click="submitEdit">确认修改</el-button>
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

/* 经验调整段（并入编辑弹窗，2026-10-30）：当前等级徽章 + 经验数字 */
.xp-current {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}

/* 编辑弹窗账号字段两列排（加宽后用满 720） */
.edit-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  column-gap: 18px;
}

/* 经验/玉调整行：数值 | 原因 | 提交，一行放下 */
.adj-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.adj-row .el-input {
  flex: 1;
}

.adj-hint {
  margin: 6px 0 0;
  font-size: 12px;
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

/* 两段式预览（v0.34）：问题行红底提示（scoped 需穿透 el-table 行类名） */
:deep(.import-bad-row) {
  --el-table-tr-bg-color: var(--el-color-danger-light-9);
}
</style>

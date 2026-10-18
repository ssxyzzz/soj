<script setup lang="ts">
// 角色管理面板（v0.5e ② 拆分自 AdminView）：Hydro 风格二维矩阵（权限为行、
// 角色为列，按分类插分组行），点选即保存。角色增删改后用户面板随 v-if 切换重建自然刷新。
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, Delete, Edit } from '@element-plus/icons-vue'

import { adminApi } from '@/api'
import { useAdminRolesStore } from '@/stores/adminRoles'
import type { PermInfo, RoleInfo } from '@/types'

const roleStore = useAdminRolesStore()

// 角色管理：勾选草稿（点选即保存，先调接口成功后才更新草稿，失败勾选自动回弹）
const permDraft = reactive<Record<string, string[]>>({})
const showCreateRole = ref(false)
const creatingRole = ref(false)
const createRoleForm = reactive({ id: '', name: '', perms: [] as string[] })

async function loadDraft() {
  for (const role of roleStore.roles) {
    permDraft[role.id] = [...role.perms]
  }
}

async function reload() {
  await roleStore.loadRoles()
  await loadDraft()
}

async function togglePerm(role: RoleInfo, perm: string) {
  const cur = permDraft[role.id] ?? []
  const next = cur.includes(perm) ? cur.filter((p) => p !== perm) : [...cur, perm]
  await adminApi.updateRole(role.id, { perms: next })
  permDraft[role.id] = next
  await reload()
}

async function renameRole(role: RoleInfo) {
  const { value } = await ElMessageBox.prompt('修改角色名称', '改名', {
    inputValue: role.name,
    inputPattern: /^.{1,32}$/,
    inputErrorMessage: '名称需在 1-32 字符',
  })
  await adminApi.updateRole(role.id, { name: value })
  ElMessage.success('已改名')
  await reload()
}

async function removeRole(role: RoleInfo) {
  await ElMessageBox.confirm(
    `确定删除自定义角色「${role.name}（${role.id}）」？删除后不可恢复。`,
    '删除角色',
    { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
  )
  await adminApi.deleteRole(role.id)
  ElMessage.success('角色已删除')
  await reload()
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
    await reload()
  } finally {
    creatingRole.value = false
  }
}

// ---- 角色矩阵行模型：权限分类分组行 + 权限位行（空分类直接不显示） ----
type PermRow = { type: 'group'; label: string } | { type: 'perm'; perm: PermInfo }

const permRows = computed<PermRow[]>(() => {
  const rows: PermRow[] = []
  for (const g of roleStore.permGroups) {
    const perms = roleStore.catalog.filter((p) => p.group === g.id)
    if (!perms.length) continue
    rows.push({ type: 'group', label: g.label })
    perms.forEach((p) => rows.push({ type: 'perm', perm: p }))
  }
  return rows
})

function permSpanMethod({ row, columnIndex }: { row: PermRow; columnIndex: number }) {
  if (row.type === 'group') {
    return columnIndex === 0
      ? { rowspan: 1, colspan: 1 + roleStore.roleGroups.length }
      : { rowspan: 0, colspan: 0 }
  }
  return { rowspan: 1, colspan: 1 }
}

// 新建角色弹窗：权限位按分类分块勾选（空分类不显示）
const nonEmptyPermGroups = computed(() =>
  roleStore.permGroups.filter((g) => roleStore.catalog.some((p) => p.group === g.id)),
)

function permsOfGroup(groupId: string): PermInfo[] {
  return roleStore.catalog.filter((p) => p.group === groupId)
}

onMounted(async () => {
  await roleStore.ensure()
  await loadDraft()
})
</script>

<template>
  <el-card>
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
        v-for="r in roleStore.roleGroups"
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
                  >
                    <Delete />
                  </el-icon>
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

    <el-dialog v-model="showCreateRole" title="新建自定义角色" width="560">
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
  </el-card>
</template>

<style scoped>
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
  color: var(--el-text-color-primary);
  background: var(--el-color-primary-light-9);
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
}

.col-act {
  cursor: pointer;
  color: var(--el-text-color-secondary);
}

.col-act:hover {
  color: var(--el-color-primary);
}

.col-act-disabled {
  color: var(--el-text-color-placeholder);
  cursor: not-allowed;
}

.col-act-disabled:hover {
  color: var(--el-text-color-placeholder);
}

.root-check {
  color: var(--el-color-success);
  font-size: 16px;
  font-weight: 600;
}

.perm-blocks {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}

.perm-block-title {
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 4px;
}

.perm-group {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
}
</style>

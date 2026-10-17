import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { adminApi } from '@/api'
import type { PermGroupInfo, PermInfo, RoleInfo } from '@/types'

// 管理后台角色清单（v0.5e ② 拆分自 AdminView）：用户面板（角色下拉/分组）与
// 角色面板（矩阵）共享同一份数据，避免各拉一份。无 admin.user.manage 权限位时
// 拉取会 403——ensure() 容错返回，保证仅有公告/设置/备份权限的面板正常工作。
export const useAdminRolesStore = defineStore('adminRoles', () => {
  const roles = ref<RoleInfo[]>([])
  const catalog = ref<PermInfo[]>([])
  const permGroups = ref<PermGroupInfo[]>([])
  const loaded = ref(false)

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

  function roleName(id: string): string {
    return roles.value.find((r) => r.id === id)?.name ?? id
  }

  async function loadRoles() {
    const r = await adminApi.listRoles()
    roles.value = r.list
    catalog.value = r.catalog
    // 兼容未带 groups 的旧后端响应
    permGroups.value = r.groups ?? []
    loaded.value = true
  }

  async function ensure() {
    if (!loaded.value) await loadRoles().catch(() => {})
  }

  return { roles, catalog, permGroups, roleGroups, roleName, loadRoles, ensure }
})

import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import type { UserPrefs } from '@/types'

// 登录态持久化在 localStorage（soj_token / soj_nickname / soj_role / soj_perms）。
// perms 为服务端计算的生效权限位全集（root 展开为全目录）；can() 是全站入口显隐的
// 唯一判据，不再判角色名——自定义角色与内置角色同等对待。
//
// 注意：登录态仅在新标签页创建 store 时从 localStorage 恢复一次；运行期只改内存态。
// 请求 token 与路由守卫统一走这里（每标签页一份），避免多标签页登录互相覆盖共享
// localStorage 造成的身份串号。
export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('soj_token') ?? '')
  const uid = ref(Number(localStorage.getItem('soj_uid') ?? 0) || 0)
  const nickname = ref(localStorage.getItem('soj_nickname') ?? '')
  const role = ref(localStorage.getItem('soj_role') ?? '')
  const perms = ref<string[]>(JSON.parse(localStorage.getItem('soj_perms') ?? '[]'))

  // 用户偏好（v0.4.8 个人设置）：登录后由布局层经 /me/profile 拉取，设置页保存后同步；
  // 供提交弹窗/在线编程预填默认语言与模板、MdEditor 初始编辑模式消费。不落 localStorage。
  const prefs = ref<UserPrefs>({})

  // 管理后台入口：root 或持有任一后台能力（角色管理面板仅 root 可见，面板内部再判）
  const canAdmin = computed(
    () =>
      role.value === 'root' ||
      perms.value.some((p) =>
        ['admin.user.manage', 'admin.settings', 'backup.run', 'backup.keys'].includes(p),
      ),
  )

  function can(perm: string): boolean {
    return role.value === 'root' || perms.value.includes(perm)
  }

  function setLogin(newToken: string, newNickname: string, newRole: string, newPerms: string[] = [], newUid = 0) {
    token.value = newToken
    uid.value = newUid
    nickname.value = newNickname
    role.value = newRole
    perms.value = newPerms
    prefs.value = {} // 新会话的偏好待 /me/profile 重新拉取
    localStorage.setItem('soj_token', newToken)
    localStorage.setItem('soj_uid', String(newUid))
    localStorage.setItem('soj_nickname', newNickname)
    localStorage.setItem('soj_role', newRole)
    localStorage.setItem('soj_perms', JSON.stringify(newPerms))
  }

  function setPrefs(p: UserPrefs) {
    prefs.value = p ?? {}
  }

  function logout() {
    token.value = ''
    uid.value = 0
    nickname.value = ''
    role.value = ''
    perms.value = []
    prefs.value = {}
    localStorage.removeItem('soj_token')
    localStorage.removeItem('soj_uid')
    localStorage.removeItem('soj_nickname')
    localStorage.removeItem('soj_role')
    localStorage.removeItem('soj_perms')
  }

  return { token, uid, nickname, role, perms, prefs, canAdmin, can, setLogin, setPrefs, logout }
})

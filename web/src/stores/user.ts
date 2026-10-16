import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

// 登录态持久化在 localStorage（soj_token / soj_nickname / soj_role / soj_perms）。
// perms 为服务端计算的生效权限位全集（root 展开为全目录）；can() 是全站入口显隐的
// 唯一判据，不再判角色名——自定义角色与内置角色同等对待。
export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('soj_token') ?? '')
  const nickname = ref(localStorage.getItem('soj_nickname') ?? '')
  const role = ref(localStorage.getItem('soj_role') ?? '')
  const perms = ref<string[]>(JSON.parse(localStorage.getItem('soj_perms') ?? '[]'))

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

  function setLogin(newToken: string, newNickname: string, newRole: string, newPerms: string[] = []) {
    token.value = newToken
    nickname.value = newNickname
    role.value = newRole
    perms.value = newPerms
    localStorage.setItem('soj_token', newToken)
    localStorage.setItem('soj_nickname', newNickname)
    localStorage.setItem('soj_role', newRole)
    localStorage.setItem('soj_perms', JSON.stringify(newPerms))
  }

  function logout() {
    token.value = ''
    nickname.value = ''
    role.value = ''
    perms.value = []
    localStorage.removeItem('soj_token')
    localStorage.removeItem('soj_nickname')
    localStorage.removeItem('soj_role')
    localStorage.removeItem('soj_perms')
  }

  return { token, nickname, role, perms, canAdmin, can, setLogin, logout }
})

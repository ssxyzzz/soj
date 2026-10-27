import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import type { LevelBadgeInfo, UserPrefs } from '@/types'

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
  // QQ 头像直链（v0.23）：顶栏头像用；localStorage 持久化保证首帧就有图，
  // /me/profile 返回后刷新（开关关闭/未绑定为空串回退首字头像）
  const avatarUrl = ref(localStorage.getItem('soj_avatar') ?? '')

  // 用户偏好（v0.4.8 个人设置）：登录后由布局层经 /me/profile 拉取，设置页保存后同步；
  // 供提交弹窗/在线编程预填默认语言、在线编程预填代码模板消费。不落 localStorage。
  const prefs = ref<UserPrefs>({})

  // 学生等级（v0.11）：顶栏徽章与补全门槛提示；/me/profile 随偏好一并下发
  const level = ref<LevelBadgeInfo | null>(null)

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
    level.value = null
    localStorage.setItem('soj_token', newToken)
    localStorage.setItem('soj_uid', String(newUid))
    localStorage.setItem('soj_nickname', newNickname)
    localStorage.setItem('soj_role', newRole)
    localStorage.setItem('soj_perms', JSON.stringify(newPerms))
  }

  function setPrefs(p: UserPrefs) {
    prefs.value = p ?? {}
  }

  function setLevel(l: LevelBadgeInfo | null) {
    level.value = l ?? null
  }

  // 本人改昵称后同步顶栏与本地持久化（token/权限不变，无需重新登录）
  function setNickname(n: string) {
    nickname.value = n
    localStorage.setItem('soj_nickname', n)
  }

  // QQ 头像同步（设置页保存 / 布局层 getProfile 回来后调用；空串 = 未绑定回退首字）
  function setAvatarUrl(u: string) {
    avatarUrl.value = u ?? ''
    if (u) localStorage.setItem('soj_avatar', u)
    else localStorage.removeItem('soj_avatar')
  }

  function logout() {
    token.value = ''
    uid.value = 0
    nickname.value = ''
    role.value = ''
    perms.value = []
    prefs.value = {}
    level.value = null
    avatarUrl.value = ''
    localStorage.removeItem('soj_token')
    localStorage.removeItem('soj_uid')
    localStorage.removeItem('soj_nickname')
    localStorage.removeItem('soj_role')
    localStorage.removeItem('soj_perms')
    localStorage.removeItem('soj_avatar')
  }

  return { token, uid, nickname, role, perms, prefs, level, avatarUrl, canAdmin, can, setLogin, setPrefs, setLevel, setNickname, setAvatarUrl, logout }
})

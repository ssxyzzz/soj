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
  // localStorage 读取容错（v0.41 验收实弹）：存储值损坏（如手改/异常写入 "undefined"）
  // 时 JSON.parse 会抛错导致 store setup 失败 → 整站白屏——损坏一律回退默认并清掉坏键
  function lsGet(key: string): string {
    try {
      const v = localStorage.getItem(key)
      return v === null || v === 'undefined' ? '' : v
    } catch {
      return ''
    }
  }

  function lsGetJSON(key: string, fallback: string[]): string[] {
    try {
      const v = localStorage.getItem(key)
      if (v === null || v === 'undefined') return fallback
      const parsed = JSON.parse(v)
      return Array.isArray(parsed) ? parsed.map(String) : fallback
    } catch {
      localStorage.removeItem(key) // 坏键清掉，下次登录写回干净值
      return fallback
    }
  }

  const token = ref(lsGet('soj_token'))
  const uid = ref(Number(lsGet('soj_uid') || 0))
  const nickname = ref(lsGet('soj_nickname'))
  const role = ref(lsGet('soj_role'))
  const perms = ref<string[]>(lsGetJSON('soj_perms', []))
  // QQ 头像直链（v0.23）：顶栏头像用；localStorage 持久化保证首帧就有图，
  // /me/profile 返回后刷新（开关关闭/未绑定为空串回退首字头像）
  const avatarUrl = ref(localStorage.getItem('soj_avatar') ?? '')

  // 用户偏好（v0.4.8 个人设置）：登录后由布局层经 /me/profile 拉取，设置页保存后同步；
  // 供提交弹窗/在线编程预填默认语言、在线编程预填代码模板消费。不落 localStorage。
  const prefs = ref<UserPrefs>({})

  // 学生等级（v0.11）：顶栏徽章与补全门槛提示；/me/profile 随偏好一并下发
  const level = ref<LevelBadgeInfo | null>(null)

  // ---- 等级权益阶梯（v0.40）：功能解锁门槛（教职工豁免，与后端 requireLevel 同口径） ----
  const isStaff = computed(() => ['root', 'admin', 'teacher'].includes(role.value))
  const staffLevel = computed(() => {
    if (isStaff.value) return 99
    return level.value?.level ?? 0 // 未加载按 0 档（入口先隐藏，加载后弹出——闸门在后端兜底）
  })
  const canOnline = computed(() => staffLevel.value >= 2) // 在线编程 + 自测
  const canChallenge = computed(() => staffLevel.value >= 3) // 挑战模式

  // 玉余额（v0.33-①）：顶栏展示（货币名后台可配）；登录后布局层经 /me/jade 拉取，
  // 消费/获取后各页调 jadeApi.mine() 自行刷新——不落 localStorage（服务端为准）
  const jadeBalance = ref(0)
  const jadeCurrency = ref('')

  // 首登改密提醒：登录响应与 /me/profile 各刷一次（登录后布局层挂载即拉 profile）。
  // 不落 localStorage——刷新后由 profile 恢复；跳过/改密由服务端清标志，弹窗宿主写本态关弹窗
  const pwdRemind = ref(false)

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

  // 玉余额同步（v0.33-①）：余额 + 货币名一起写；登出清零
  function setJade(balance: number, currency: string) {
    jadeBalance.value = balance
    jadeCurrency.value = currency || '玉'
  }

  // 本人改昵称后同步顶栏与本地持久化（token/权限不变，无需重新登录）
  function setNickname(n: string) {
    nickname.value = n
    localStorage.setItem('soj_nickname', n)
  }

  // 滑动续期（v0.82.7）：服务端对临期 token（剩余 < 3 天）在响应头 X-Renewed-Token
  // 下发新票，api 拦截器做同会话校验后调用——仅换 token 本体，登录态其余不动。
  // 多标签页各自持各自内存 token 独立滑动，localStorage 写最后落者（均为有效票，不串号）。
  function renewToken(newToken: string) {
    if (!newToken || newToken === token.value) return
    token.value = newToken
    localStorage.setItem('soj_token', newToken)
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
    jadeBalance.value = 0
    jadeCurrency.value = ''
    pwdRemind.value = false
    localStorage.removeItem('soj_token')
    localStorage.removeItem('soj_uid')
    localStorage.removeItem('soj_nickname')
    localStorage.removeItem('soj_role')
    localStorage.removeItem('soj_perms')
    localStorage.removeItem('soj_avatar')
  }

  return { token, uid, nickname, role, perms, prefs, level, isStaff, canOnline, canChallenge, avatarUrl, jadeBalance, jadeCurrency, pwdRemind, canAdmin, can, setLogin, setPrefs, setLevel, setJade, setNickname, setAvatarUrl, renewToken, logout }
})

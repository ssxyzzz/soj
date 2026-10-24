import { ref } from 'vue'
import { defineStore } from 'pinia'

import { siteApi } from '@/api'

// 站点公开状态（/api/site）：进程中拉一次全局共享（顶栏按钮文案、登录页页签、页脚备案号共用）。
// 注册开关由后台「系统设置」控制，关闭后登录页隐藏「注册」页签；ICP 备案号空 = 页脚不展示。
// 拉取失败静默降级（注册开关回开放、备案号回空、验证方式回 none，与后端缺省一致），不阻塞页面。
export const useSiteStore = defineStore('site', () => {
  const registerOpen = ref(true)
  const icpNumber = ref('')
  // 注册验证方式与找回开关（v0.16）：登录页注册表单出邮箱+验证码、忘记密码入口显隐
  const registerVerifyMode = ref<'none' | 'email'>('none')
  const passwordResetEnabled = ref(false)
  let fetched = false
  let fetching: Promise<void> | null = null

  async function ensure() {
    if (fetched) return
    if (!fetching) {
      fetching = siteApi
        .info()
        .then((r) => {
          registerOpen.value = r.register_open
          icpNumber.value = r.icp_number || ''
          registerVerifyMode.value = r.register_verify_mode ?? 'none'
          passwordResetEnabled.value = !!r.password_reset_enabled
          fetched = true
        })
        .catch(() => {})
        .finally(() => {
          fetching = null
        })
    }
    await fetching
  }

  // 后台改了公开设置（如备案号）后重拉一次，让页脚/页签即时生效
  function refresh() {
    fetched = false
    return ensure()
  }

  return { registerOpen, icpNumber, registerVerifyMode, passwordResetEnabled, ensure, refresh }
})

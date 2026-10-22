import { ref } from 'vue'
import { defineStore } from 'pinia'

import { authApi } from '@/api'

// 站点公开状态：进程中拉一次全局共享（顶栏按钮文案与登录页共用）。
// 注册开关由后台「系统设置」控制，关闭后登录页隐藏「注册」页签。
// 拉取失败静默降级为开放（与后端缺省一致），不阻塞页面。
export const useSiteStore = defineStore('site', () => {
  const registerOpen = ref(true)
  let fetched = false
  let fetching: Promise<void> | null = null

  async function ensure() {
    if (fetched) return
    if (!fetching) {
      fetching = authApi
        .registerOpen()
        .then((r) => {
          registerOpen.value = r.open
          fetched = true
        })
        .catch(() => {})
        .finally(() => {
          fetching = null
        })
    }
    await fetching
  }

  return { registerOpen, ensure }
})

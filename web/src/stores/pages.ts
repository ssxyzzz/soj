import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'

import { pagesApi } from '@/api'
import type { PageItem } from '@/types'
import { useUserStore } from '@/stores/user'

// 自定义页面（v0.56）：登录后拉一次可见页清单，导航/页脚入口与 /p/:slug 壳页渲染共用。
// 登录态驱动：watch token 拉取/清空（登录后自动补拉，登出清空防串号）。
// 拉取失败静默降级（无自定义入口，不阻塞页面）。
export const usePagesStore = defineStore('pages', () => {
  const pages = ref<PageItem[]>([])
  let fetched = false
  let fetching: Promise<void> | null = null

  const userStore = useUserStore()

  async function ensure() {
    if (fetched) return
    if (!fetching) {
      fetching = pagesApi
        .list()
        .then((r) => {
          pages.value = r.pages || []
          fetched = true
        })
        .catch(() => {})
        .finally(() => {
          fetching = null
        })
    }
    await fetching
  }

  // 登录态变化：登录拉取、登出清空（清单随角色不同而不同，不能跨账号复用）
  watch(
    () => userStore.token,
    (t) => {
      fetched = false
      if (t) {
        ensure()
      } else {
        pages.value = []
      }
    },
  )
  if (userStore.token) ensure()

  function refresh() {
    fetched = false
    return ensure()
  }

  // 导航入口页（服务端已按可见性+启停过滤，sort 升序下发）
  const navPages = computed(() => pages.value.filter((p) => p.nav_show))
  // 页脚入口页
  const footerPages = computed(() => pages.value.filter((p) => p.footer_show))
  // 按 slug 取页（/p/:slug 渲染用；未加载完成时 undefined）
  function bySlug(slug: string) {
    return pages.value.find((p) => p.slug === slug)
  }

  return { pages, navPages, footerPages, bySlug, ensure, refresh }
})

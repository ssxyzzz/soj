import { ref, type Ref } from 'vue'

/**
 * 「筛选 + 分页列表页」状态样板（v0.29 收敛）：page/total/loading 三件套 +
 * 筛选变动重置回第一页。load 由调用方提供（通常配合 api 层 AbortController：
 * 筛选变更取消在途旧请求，避免慢网络下旧响应覆盖新筛选结果）。
 */
export function usePagedList<T>(fetchPage: (page: number) => Promise<{ list: T[]; total: number }>, opts?: { pageSize?: number }) {
  const list: Ref<T[]> = ref([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = opts?.pageSize ?? 20
  const loading = ref(false)
  let seq = 0 // 防竞态：只认最新一次请求的结果（比 AbortController 更粗但零依赖，两层可并用）

  async function load() {
    const my = ++seq
    loading.value = true
    try {
      const r = await fetchPage(page.value)
      if (my !== seq) return // 已有更新的筛选请求在途/完成，丢弃本拍
      list.value = r.list
      total.value = r.total
    } finally {
      if (my === seq) loading.value = false
    }
  }

  /** 任何筛选条件变化后调用：回第一页并重拉（六个 onXxxChange 样板收敛为一） */
  async function reload() {
    page.value = 1
    await load()
  }

  return { list, total, page, pageSize, loading, load, reload }
}

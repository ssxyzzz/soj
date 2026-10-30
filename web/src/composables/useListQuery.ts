import { useRoute, useRouter } from 'vue-router'

/**
 * 列表状态 URL 化（v0.39「URL 即状态」）：挂载从 route.query 复原现场、
 * 变更点 router.replace 回写（不堆历史条目，翻页不产生历史噪音）。
 *
 * 实现守则（防性能回环，立项 §2.42 红线）：
 * 1. query 只作挂载复原源——变更点先改 ref 再显式调 load，绝不 watch 回读触发请求；
 * 2. 回写只保留非空/非默认键——默认态 URL 干净，且严禁把大数组塞进 query。
 */
export function useListQuery() {
  const route = useRoute()
  const router = useRouter()

  /** 挂载复原：正整数（页码等）；非法/缺失回 def */
  function num(key: string, def = 1): number {
    const n = Number(route.query[key])
    return Number.isInteger(n) && n >= 1 ? n : def
  }

  /** 挂载复原：字符串（关键词等）；缺失回空串 */
  function str(key: string): string {
    return String(route.query[key] ?? '').trim()
  }

  /** 挂载复原：逗号分隔正整数列表（标签/难度等筛选）；非法项过滤 */
  function ids(key: string): number[] {
    return String(route.query[key] ?? '')
      .split(',')
      .map((s) => Number(s))
      .filter((n) => Number.isInteger(n) && n > 0)
  }

  /**
   * 变更回写：entries 键值对，空串/0/undefined/空数组的键整体省略。
   * 空对象 = 回到默认态（清空全部 query）。
   */
  function sync(entries: Record<string, string | number | undefined>) {
    const query: Record<string, string> = {}
    for (const [k, v] of Object.entries(entries)) {
      if (v === undefined || v === '' || v === 0) continue
      query[k] = String(v)
    }
    router.replace({ query })
  }

  return { num, str, ids, sync }
}

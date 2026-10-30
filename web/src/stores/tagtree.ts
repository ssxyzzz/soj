import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { tagApi } from '@/api'
import type { TagNode } from '@/types'

// 标签树（v0.5.5）：题库筛选、列表标签列、详情信息卡、建题表单四处共享
// 同一份树与 ID→名字映射——名字永远以树为准，root 改名全站自动跟随。
// v0.29 起与来源树（metaTree store）共用 treeStoreOf 工厂（对齐 api 层 roundApiOf
// 范式），两树仅「拉取参数 + store id」不同，walk/映射/失效逻辑单份维护。
//
// v0.36 加固（生产实测一客户端 15 分钟 195 次 /api/tags/tree）：树只有 root 能改、
// 普通用户全程只读，是缓存理想目标——
//   ① 单飞：在途请求全站复用（题目详情页双组件 × 双树曾一次导航 4 连发）；
//   ② 失败负缓存：拉取失败 30s 内不再重试（失败后 loaded 恒 false、每次导航都重拉，
//      是上述 195 次循环在代码里唯一可复现的机制）；
//   ③ TTL 10 分钟：过期后旧数据先用、后台静默刷新（stale-while-revalidate）。

// 成功缓存有效期：树变更频率 = root 手改，10 分钟足够鲜
const TREE_TTL_MS = 10 * 60 * 1000
// 失败冷却：网络抖动/后端瞬时不可用时别让每次导航都撞一次
const TREE_FAIL_COOLDOWN_MS = 30 * 1000

export function treeStoreOf(kind: 'knowledge' | 'meta', id: string) {
  return defineStore(id, () => {
    const nodes = ref<TagNode[]>([])
    const loaded = ref(false)
    let fetching: Promise<void> | null = null
    let loadedAt = 0
    let failedAt = 0

    const idNames = computed(() => {
      const m = new Map<number, string>()
      const walk = (list: TagNode[]) => {
        for (const n of list) {
          m.set(n.id, n.name)
          if (n.children) walk(n.children)
        }
      }
      walk(nodes.value)
      return m
    })

    // 标签层级（1-5，更深层沿用第五级）：表格标签列与卡片流共用同一套五级配色
    //（来源树也按层级取色，组件层无需分支）
    const idLevels = computed(() => {
      const m = new Map<number, number>()
      const walk = (list: TagNode[], depth: number) => {
        for (const n of list) {
          m.set(n.id, Math.min(depth + 1, 5))
          if (n.children) walk(n.children, depth + 1)
        }
      }
      walk(nodes.value, 0)
      return m
    })

    function nameOf(tid: number): string {
      return idNames.value.get(tid) ?? `#${tid}`
    }

    function levelOf(tid: number): number {
      return idLevels.value.get(tid) ?? 1
    }

    // 无条件强拉（root 保存后的强制刷新走这里，绕过一切缓存语义）
    async function load() {
      const r = kind === 'meta' ? await tagApi.tree('meta') : await tagApi.tree()
      nodes.value = r.nodes
      loaded.value = true
      loadedAt = Date.now()
      failedAt = 0
    }

    // 按需取用（组件挂载/路由进入调用）：单飞 + TTL + 失败冷却
    function ensure(): Promise<void> {
      if (fetching) return fetching
      if (loaded.value) {
        if (Date.now() - loadedAt < TREE_TTL_MS) return Promise.resolve()
        // 过期：旧数据先用，后台静默刷新（失败不打扰，冷却计时照走）
        fetching = load()
          .catch(() => {
            failedAt = Date.now()
          })
          .finally(() => {
            fetching = null
          })
        return Promise.resolve()
      }
      if (Date.now() - failedAt < TREE_FAIL_COOLDOWN_MS) return Promise.resolve()
      fetching = load()
        .catch(() => {
          failedAt = Date.now()
        })
        .finally(() => {
          fetching = null
        })
      return fetching
    }

    // 保存后重置缓存（root 改树后其他页面的名字映射随之刷新）
    function invalidate() {
      loaded.value = false
      loadedAt = 0
    }

    return { nodes, loaded, nameOf, levelOf, load, ensure, invalidate }
  })
}

export const useTagTreeStore = treeStoreOf('knowledge', 'tagtree')

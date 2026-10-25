import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { tagApi } from '@/api'
import type { TagNode } from '@/types'

// 来源标签树（2026-10-25 双树）：地区/赛事/题库/年份/其他。与知识树（tagtree store）
// 同构同权限——题库筛选、详情来源区、建题表单来源打标共享同一份树与 ID→名字映射；
// 节点单色中性（不走知识树五级配色），root 改树全站自动跟随。
export const useMetaTreeStore = defineStore('metaTree', () => {
  const nodes = ref<TagNode[]>([])
  const loaded = ref(false)

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

  // 来源树节点也按层级取色（与知识树同一套五级配色，组件层无需分支）
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

  function nameOf(id: number): string {
    return idNames.value.get(id) ?? `#${id}`
  }

  function levelOf(id: number): number {
    return idLevels.value.get(id) ?? 1
  }

  async function load() {
    const r = await tagApi.tree('meta')
    nodes.value = r.nodes
    loaded.value = true
  }

  async function ensure() {
    if (!loaded.value) await load().catch(() => {})
  }

  // 保存后重置缓存（root 改树后其他页面的名字映射随之刷新）
  function invalidate() {
    loaded.value = false
  }

  return { nodes, loaded, nameOf, levelOf, load, ensure, invalidate }
})

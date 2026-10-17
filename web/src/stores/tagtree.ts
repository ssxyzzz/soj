import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { tagApi } from '@/api'
import type { TagNode } from '@/types'

// 标签树（v0.5f）：题库筛选、列表标签列、详情信息卡、建题表单四处共享
// 同一份树与 ID→名字映射——名字永远以树为准，root 改名全站自动跟随。
export const useTagTreeStore = defineStore('tagtree', () => {
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

  function nameOf(id: number): string {
    return idNames.value.get(id) ?? `#${id}`
  }

  async function load() {
    const r = await tagApi.tree()
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

  return { nodes, loaded, nameOf, load, ensure, invalidate }
})

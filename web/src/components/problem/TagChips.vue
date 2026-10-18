<script setup lang="ts">
// 题库筛选的标签卡片流（与后台 TagsPanel 同款样式与悬停交互，只读版）：
// 悬停到哪层就展开到哪层（path 悬停路径，移开 180ms 收起），点击卡片切换选中
// （多选并集、精确匹配）。无编辑/拖拽/新增——选中态 = 层级色实底 + ✓。
import { computed, onBeforeUnmount, ref } from 'vue'

import type { TagNode } from '@/types'

const props = defineProps<{
  nodes: TagNode[]
  selected: Set<number>
}>()

const emit = defineEmits<{ toggle: [id: number] }>()

// 悬停路径：path[i] = 第 i 层悬停的节点 id，其子级即第 i+1 行
const path = ref<number[]>([])
const search = ref('') // 标签搜索：非空时整树按名匹配平铺展示（悬停导航停用）
let hoverTimer: ReturnType<typeof setTimeout> | undefined

interface TagItem {
  node: TagNode
  depth: number
}

interface TagRow {
  items: TagItem[]
}

const rows = computed<TagRow[]>(() => {
  const q = search.value.trim().toLowerCase()
  if (q) {
    const hits: TagItem[] = []
    const walk = (list: TagNode[], depth: number) => {
      for (const n of list) {
        if (n.name.toLowerCase().includes(q)) hits.push({ node: n, depth })
        if (n.children) walk(n.children, depth + 1)
      }
    }
    walk(props.nodes, 0)
    return [{ items: hits }]
  }
  const out: TagRow[] = [{ items: props.nodes.map((n) => ({ node: n, depth: 0 })) }]
  for (let d = 0; d < path.value.length; d++) {
    const next = findNode(props.nodes, path.value[d])?.children
    if (!next || !next.length) break
    out.push({ items: next.map((n) => ({ node: n, depth: d + 1 })) })
  }
  return out
})

function findNode(list: TagNode[], id: number): TagNode | null {
  for (const n of list) {
    if (n.id === id) return n
    const hit = n.children && findNode(n.children, id)
    if (hit) return hit
  }
  return null
}

// 层级配色：与后台标签树一致，1-5 级各一色（更深层沿用第五级）
function lvClass(depth: number): string {
  return `lv-${Math.min(depth + 1, 5)}`
}

function onCardHover(row: number, node: TagNode) {
  if (search.value.trim()) return
  if (hoverTimer) {
    clearTimeout(hoverTimer)
    hoverTimer = undefined
  }
  path.value = [...path.value.slice(0, row), node.id]
}

function scheduleHoverClear() {
  if (hoverTimer) clearTimeout(hoverTimer)
  hoverTimer = setTimeout(() => {
    path.value = []
    hoverTimer = undefined
  }, 180)
}

function cancelHoverClear() {
  if (hoverTimer) {
    clearTimeout(hoverTimer)
    hoverTimer = undefined
  }
}

onBeforeUnmount(() => {
  if (hoverTimer) clearTimeout(hoverTimer)
})
</script>

<template>
  <div class="tag-flow" @mouseleave="scheduleHoverClear">
    <el-input v-model="search" class="tag-search" clearable placeholder="搜索标签…" />
    <div
      v-for="(r, i) in rows"
      :key="i"
      class="tag-row"
      @mouseenter="cancelHoverClear"
      @mouseleave="scheduleHoverClear"
    >
      <div
        v-for="item in r.items"
        :key="item.node.id"
        class="tag-card"
        :class="[lvClass(item.depth), { active: selected.has(item.node.id) }]"
        @click="emit('toggle', item.node.id)"
        @mouseenter="onCardHover(item.depth, item.node)"
      >
        {{ item.node.name }}
        <span v-if="item.node.children?.length" class="caret">▸</span>
        <span v-if="selected.has(item.node.id)" class="check">✓</span>
      </div>
      <div v-if="!r.items.length" class="dim">没有匹配的标签</div>
    </div>
  </div>
</template>

<style scoped>
.tag-flow {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tag-search {
  width: 240px;
  margin-bottom: 4px;
}

.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: flex-start;
}

/* 卡片样式与后台 TagsPanel 同款（--lv 由 .lv-1~.lv-5 按层注入），尺寸按筛选场景略收紧 */
.tag-card {
  --lv: var(--el-color-primary);
  --lv-bg: var(--el-color-primary-light-9);
  --lv-border: var(--el-color-primary-light-7);
  display: inline-flex;
  align-items: center;
  padding: 4px 12px;
  background: var(--lv-bg);
  border: 1px solid var(--lv-border);
  border-radius: 6px;
  font-size: 14px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s, border-color 0.15s;
}

.tag-card:hover {
  border-color: var(--lv);
}

/* 选中（筛选生效中）：本层级色实底 + ✓ */
.tag-card.active {
  background: var(--lv);
  border-color: var(--lv);
  color: #fff;
}

.tag-card.lv-1 {
  --lv: var(--el-color-primary);
  --lv-bg: var(--el-color-primary-light-9);
  --lv-border: var(--el-color-primary-light-7);
}

.tag-card.lv-2 {
  --lv: var(--el-color-success);
  --lv-bg: var(--el-color-success-light-9);
  --lv-border: var(--el-color-success-light-7);
}

.tag-card.lv-3 {
  --lv: var(--el-color-warning);
  --lv-bg: var(--el-color-warning-light-9);
  --lv-border: var(--el-color-warning-light-7);
}

.tag-card.lv-4 {
  --lv: var(--el-color-danger);
  --lv-bg: var(--el-color-danger-light-9);
  --lv-border: var(--el-color-danger-light-7);
}

.tag-card.lv-5 {
  --lv: var(--el-color-info);
  --lv-bg: var(--el-color-info-light-9);
  --lv-border: var(--el-color-info-light-7);
}

.caret {
  margin-left: 4px;
  font-size: 12px;
  opacity: 0.55;
}

.check {
  margin-left: 4px;
  font-weight: 600;
}
</style>

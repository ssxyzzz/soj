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

// 滚轮横移：竖向滚轮增量映射到横向滚动（当前层级的标签一览）；触摸板横扫 deltaX 优先。
// 行内有溢出时**整行接管滚轮**——滚到头也不再放行，持续滚动不会带动页面下滑
// （鼠标停在本行即浏览本行）；行本身没有溢出才不拦截，滚轮交给页面正常滚动。
function onWheel(e: WheelEvent) {
  const el = e.currentTarget as HTMLElement
  const max = el.scrollWidth - el.clientWidth
  if (max <= 0) return
  e.preventDefault()
  const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
  if (delta === 0) return
  el.scrollLeft = Math.max(0, Math.min(max, el.scrollLeft + delta))
}

onBeforeUnmount(() => {
  if (hoverTimer) clearTimeout(hoverTimer)
})
</script>

<template>
  <div class="tag-flow" @mouseleave="scheduleHoverClear">
    <div class="tag-top">
      <el-input v-model="search" class="tag-search" clearable placeholder="搜索标签…" />
      <!-- 顶行右侧插槽：TagFilter 在此挂已选展示（搜索标签框右边） -->
      <div class="tag-top-right">
        <slot name="top-right" />
      </div>
    </div>
    <!-- 每级行独立横向滚动：无滚动条，滚轮只在鼠标所在行拦截+横移；各行各自记位置，
         行内有溢出即整行接管（滚到头也不带动页面下滑） -->
    <div class="tag-canvas" @mouseenter="cancelHoverClear">
      <div v-for="(r, i) in rows" :key="i" class="tag-row" @mouseenter="cancelHoverClear" @wheel="onWheel">
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
      <!-- 底部两行缓冲：末级展开后鼠标习惯性继续下移查找，进垫片即取消收起计时（见 .tag-flow-pad） -->
      <div class="tag-flow-pad" @mouseenter="cancelHoverClear" />
    </div>
  </div>
</template>

<style scoped>
.tag-flow {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 底部两行缓冲垫片：末级展开后鼠标习惯性继续下移查找，若移出容器会触发
   mouseleave 收起——垫片在容器内且 mouseenter 取消收起计时，移动不断开 */
.tag-flow-pad {
  height: 80px;
}

/* 顶行：搜索标签框 + 右侧插槽（已选），随内容换行 */
.tag-top {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.tag-top-right {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.tag-search {
  width: 240px;
}

/* 画布列布局（行距）；行宽跟随容器，不再由最宽行撑开——溢出交给各行自己 */
.tag-canvas {
  display: flex;
  flex-direction: column;
  gap: 14px; /* 层级行距（一级/二级……之间） */
  min-width: 0;
}

/* 每级一行独立滚动：行内不换行，超出本行宽度即横移（无滚动条，滚轮驱动见 onWheel）；
   每行各持独立 scrollLeft——滚哪行动哪行，其余行不受影响 */
.tag-row {
  display: flex;
  flex-wrap: nowrap;
  gap: 8px;
  align-items: flex-start;
  width: 100%;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none; /* Firefox：隐藏滚动条 */
}

.tag-row::-webkit-scrollbar {
  display: none;
}

/* 层级色只来自全局 .lv-1~.lv-5（main.css）：此处不可写 --lv 兜底，
   scoped 特异度会压过全局，把各层级吞成一级蓝 */
.tag-card {
  flex: none; /* 行内不换行模式：卡片不压缩，超出即由本行横移 */
  white-space: nowrap;
  display: inline-flex;
  align-items: center;
  padding: 4px 12px;
  line-height: 20px;
  background: var(--lv-bg);
  border: 1px solid var(--lv-border);
  border-radius: 6px;
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
  color: var(--el-color-white);
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

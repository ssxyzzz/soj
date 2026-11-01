<script setup lang="ts">
// 题单编辑器「节点题目管理」大弹窗（2026-11-01 重构批）：
// 双栏 = 候选（搜索 + 知识点标签 + 难度徽章）/ 已选（拖拽排序 + 删除撤销）；
// ◀上一节/下一节▶ 按大纲顺序切节点（到头禁用，用户拍板），搜索词与筛选跨节点保留；
// 跨节防重（用户拍板）：已挂在本题单其他节点的题在候选区呈「已选择」置灰禁点，
// tooltip 标注挂载位置——同题多节从交互上不再产生（服务端仍容忍存量重复，仅 UI 防呆）。
// 标题/描述编辑只留大纲就地编辑（用户拍板「只留一处」），本弹窗专注题目管理。
// 草稿直接改父页面传入的 chs 引用，变更即 emit('dirty')；保存仍由大纲页整体 PATCH。
import { computed, h, onMounted, ref, watch } from 'vue'
import { ElButton, ElMessage } from 'element-plus'

import { problemApi } from '@/api'
import TagFilter from '@/components/problem/TagFilter.vue'
import TierBadge from '@/components/TierBadge.vue'
import type { ListEntryDraft, ProblemListItem } from '@/types'

const props = defineProps<{
  chs: ListEntryDraft[] // 草稿引用（直接改）
  nodeKey: string | null // 'ch:i' | 'sec:i-j'；null = 关闭
}>()

const emit = defineEmits<{
  (e: 'update:nodeKey', v: string): void
  (e: 'close'): void
  (e: 'dirty'): void
}>()

// ---- 可管理节点扁平化（prev/next 顺序 = 大纲顺序；不混挂：有子节的章无独立题目入口） ----
interface ManageableNode {
  key: string
  node: ListEntryDraft
  crumb: string // 弹窗头面包屑
  label: string // 跨节标记短标签（如 1.2 变量计算 / 第2章 分支）
}

const flat = computed<ManageableNode[]>(() => {
  const out: ManageableNode[] = []
  props.chs.forEach((c, ci) => {
    if (c.children.length) {
      c.children.forEach((s, si) => {
        out.push({
          key: `sec:${ci}-${si}`,
          node: s,
          crumb: `第${ci + 1}章 ${c.title || '未命名'} › ${ci + 1}.${si + 1} ${s.title || '未命名'}`,
          label: `${ci + 1}.${si + 1} ${s.title || '未命名'}`,
        })
      })
    } else {
      out.push({
        key: `ch:${ci}`,
        node: c,
        crumb: `第${ci + 1}章 ${c.title || '未命名'}`,
        label: `第${ci + 1}章 ${c.title || '未命名'}`,
      })
    }
  })
  return out
})

const idx = computed(() => flat.value.findIndex((n) => n.key === props.nodeKey))
const node = computed(() => (idx.value >= 0 ? flat.value[idx.value].node : null))
const crumb = computed(() => (idx.value >= 0 ? flat.value[idx.value].crumb : ''))

function step(dir: -1 | 1) {
  const j = idx.value + dir
  if (j < 0 || j >= flat.value.length) return
  emit('update:nodeKey', flat.value[j].key)
}

// ---- 全题单挂载索引：pid → 挂载位置标签（跨节防重的依据） ----
const mountIndex = computed(() => {
  const m = new Map<number, string[]>()
  for (const n of flat.value) {
    for (const p of n.node.problems) {
      const arr = m.get(p.id) ?? []
      arr.push(n.label)
      m.set(p.id, arr)
    }
  }
  return m
})

function inCurrent(pid: number): boolean {
  return !!node.value?.problems.some((p) => p.id === pid)
}

function mountedElsewhere(pid: number): string {
  const cur = flat.value[idx.value]
  if (!cur) return ''
  return (mountIndex.value.get(pid) ?? []).find((lb) => lb !== cur.label) ?? ''
}

// ---- 候选搜索（同 ProblemPicker 口径：q 走题库检索、排除隐藏/重复题；搜索词跨节点保留） ----
const q = ref('')
const tags = ref<number[]>([])
const candidates = ref<ProblemListItem[]>([])
const loading = ref(false)
let debounce: ReturnType<typeof setTimeout> | undefined

async function search() {
  loading.value = true
  try {
    const r = await problemApi.list(1, 100, q.value.trim() || undefined, tags.value)
    candidates.value = r.list.filter((p) => !p.hidden && !p.duplicate)
  } catch {
    candidates.value = []
  } finally {
    loading.value = false
  }
}

watch([q, tags], () => {
  if (debounce) clearTimeout(debounce)
  debounce = setTimeout(search, 400)
})

onMounted(search)

// ---- 候选多选 + 加入（双击行 = 单题直接加入，最高频动作一步完成） ----
const selIds = ref(new Set<number>())

function locked(pid: number): boolean {
  return inCurrent(pid) || !!mountedElsewhere(pid)
}

function toggleSel(id: number) {
  if (locked(id)) return
  const next = new Set(selIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selIds.value = next
}

function addPicked(id: number, title: string) {
  if (locked(id) || !node.value) return
  node.value.problems.push({ id, title })
  emit('dirty')
}

function addSelected() {
  if (!node.value || !selIds.value.size) return
  for (const p of candidates.value) {
    if (selIds.value.has(p.id)) node.value.problems.push({ id: p.id, title: p.title })
  }
  selIds.value = new Set()
  emit('dirty')
}

const selCount = computed(() => selIds.value.size)

// ---- 已选：拖拽排序 + 删除撤销（toast 5s 内可撤销，保存前有效） ----
const dragIndex = ref<number | null>(null)

function onDragStart(i: number) {
  dragIndex.value = i
}

function onDragOver(e: DragEvent) {
  e.preventDefault()
}

function onDrop(i: number) {
  if (dragIndex.value == null || dragIndex.value === i || !node.value) return
  const next = [...node.value.problems]
  const [moved] = next.splice(dragIndex.value, 1)
  next.splice(i, 0, moved)
  node.value.problems = next
  dragIndex.value = null
  emit('dirty')
}

function removeAt(i: number) {
  if (!node.value) return
  const removed = node.value.problems[i]
  node.value.problems.splice(i, 1)
  emit('dirty')
  const msg = ElMessage({
    message: h('span', null, [
      `已移除 #${removed.id} ${removed.title} `,
      h(
        ElButton,
        {
          link: true,
          type: 'primary',
          size: 'small',
          onClick: () => {
            node.value?.problems.splice(Math.min(i, node.value.problems.length), 0, removed)
            emit('dirty')
            msg.close()
          },
        },
        () => '撤销',
      ),
    ]),
    duration: 5000,
  })
}
</script>

<template>
  <el-dialog
    :model-value="!!nodeKey"
    :title="crumb || '管理题目'"
    width="1080px"
    top="4vh"
    @update:model-value="(v: boolean) => !v && emit('close')"
  >
    <div class="spd-nav">
      <el-button size="small" :disabled="idx <= 0" @click="step(-1)">◀ 上一节</el-button>
      <span class="spd-pos mono">{{ idx + 1 }} / {{ flat.length }}</span>
      <el-button size="small" :disabled="idx < 0 || idx >= flat.length - 1" @click="step(1)">下一节 ▶</el-button>
    </div>

    <div class="spd-search">
      <el-input v-model="q" placeholder="搜索标题、题号、知识点或标签" clearable />
      <TagFilter :selected="tags" @change="(ids: number[]) => (tags = ids)" />
    </div>

    <div class="spd-cols">
      <div class="spd-col">
        <div class="col-head spd-cand-head">
          <span>候选题目（双击加入 · 勾选后批量加入）</span>
          <el-button size="small" type="primary" plain :disabled="!selCount" @click="addSelected">
            加入所选{{ selCount ? ` (${selCount})` : '' }}
          </el-button>
        </div>
        <div v-loading="loading" class="spd-list">
          <div
            v-for="p in candidates"
            :key="p.id"
            class="spd-row"
            :class="{ picked: inCurrent(p.id), mounted: !!mountedElsewhere(p.id), sel: selIds.has(p.id) }"
            @click="toggleSel(p.id)"
            @dblclick="addPicked(p.id, p.title)"
          >
            <el-checkbox
              :model-value="locked(p.id) || selIds.has(p.id)"
              :disabled="locked(p.id)"
              @click.stop
              @change="toggleSel(p.id)"
            />
            <span class="spd-title">#{{ p.id }} {{ p.title }}</span>
            <el-tooltip
              v-if="mountedElsewhere(p.id)"
              :content="`已挂载于 ${mountedElsewhere(p.id)}，同一题单内不可重复挂载`"
              placement="top"
            >
              <span class="mounted-tag">已挂</span>
            </el-tooltip>
            <TierBadge v-if="p.difficulty" :tier="p.difficulty" kind="difficulty" />
          </div>
          <el-empty v-if="!loading && !candidates.length" description="没有匹配的题目" :image-size="40" />
        </div>
      </div>

      <div class="spd-col">
        <div class="col-head">已选 {{ node?.problems.length ?? 0 }} 题（拖动 ⠿ 排序）</div>
        <div class="spd-list">
          <div
            v-for="(p, i) in node?.problems ?? []"
            :key="p.id"
            class="spd-row picked"
            draggable="true"
            @dragstart="onDragStart(i)"
            @drop="onDrop(i)"
            @dragover="onDragOver"
          >
            <span class="drag">⠿</span>
            <span class="t-no mono">T{{ i + 1 }}</span>
            <span class="spd-title">#{{ p.id }} {{ p.title }}</span>
            <el-tag v-if="p.hidden" size="small" type="danger">隐藏题</el-tag>
            <el-button class="del" size="small" type="danger" link @click="removeAt(i)">删除</el-button>
          </div>
          <el-empty v-if="node && !node.problems.length" description="还没有题目，从左侧候选添加" :image-size="40" />
        </div>
      </div>
    </div>

    <template #footer>
      <span class="spd-foot-hint">已挂其他章节/小节的题不可重复挂载 · 删除可撤销</span>
      <el-button type="primary" @click="emit('close')">完成</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.spd-nav {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}

.spd-pos {
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-13);
}

.spd-search {
  margin-bottom: 10px;
}

.spd-cols {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  min-width: 0;
}

.spd-col {
  min-width: 0;
}

.col-head {
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-small);
  margin-bottom: 6px;
}

.spd-cand-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.spd-list {
  height: 460px;
  overflow-y: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
  background: var(--el-bg-color);
}

.spd-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  cursor: pointer;
  border-bottom: 1px solid var(--el-fill-color);
}

.spd-row:hover {
  background: var(--el-fill-color-light);
}

.spd-row.picked {
  background: var(--el-color-primary-light-9);
}

.spd-row.mounted {
  cursor: not-allowed;
  opacity: 0.72;
}

.spd-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mounted-tag {
  flex: none;
  font-size: var(--soj-fs-12);
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  border-radius: 999px;
  padding: 1px 8px;
}

.drag {
  color: var(--el-text-color-placeholder);
  cursor: grab;
}

.t-no {
  width: 32px;
  color: var(--el-text-color-secondary);
}

.del {
  flex: none;
}

.spd-foot-hint {
  float: left;
  color: var(--el-text-color-placeholder);
  font-size: var(--soj-fs-13);
  line-height: 32px;
}
</style>

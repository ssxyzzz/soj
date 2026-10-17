<script setup lang="ts">
// 标签树管理（v0.5f 建面板，卡片化交互重构于编辑器体验小批同期，仅 root）：
// 层级下钻卡片流——点卡片展开下一级、双击改名、长按浮起（拖动同级换位 / 点 × 删除）、
// 行尾白卡 + 新增；右上角常驻角标 = 被引用题数（GET /admin/tagtree 附带）。
// 草稿为本地状态，保存仍是整树覆盖 + 二次确认 + 服务端被引用拦截；
// 身份按名字对齐（改名 = 删除＋新增），被引用节点的删除/改名在交互层前置拦截。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { tagApi } from '@/api'
import type { TagNode, TagRefProblem } from '@/types'
import { useTagTreeStore } from '@/stores/tagtree'

const emit = defineEmits<{ (e: 'update:dirty', v: boolean): void }>()

const tagStore = useTagTreeStore()

// 草稿节点：children 恒为数组（序列化稳定，脏检查用 JSON 快照对比）。
// 新节点用临时负数 id（仅作 DOM key），保存载荷映射回 0 由服务端取号。
interface DraftNode {
  id: number
  name: string
  children: DraftNode[]
}

const draft = ref<DraftNode[]>([])
const savedJson = ref('[]')
const counts = ref<Record<string, number>>({})
const loading = ref(true)
const saving = ref(false)
const blockedProblems = ref<TagRefProblem[] | null>(null)
const selectedPath = ref<number[]>([]) // selectedPath[i] = 第 i 级行选中的节点 id
const floating = ref<number | null>(null) // 长按浮动态中的节点 id
const draggingId = ref<number | null>(null)
const editing = ref<{ id: number; name: string } | null>(null)
const adding = ref<{ parentId: number; name: string } | null>(null) // parentId 0 = 根行

let tempId = 0 // 新节点临时 id 发生器（-1, -2, ...）

const dirty = computed(() => JSON.stringify(draft.value) !== savedJson.value)
watch(dirty, (v) => emit('update:dirty', v), { immediate: true })

const total = computed(() => {
  const walk = (list: DraftNode[]): number => list.reduce((s, n) => s + 1 + walk(n.children), 0)
  return walk(draft.value)
})

// 下钻行：第 0 行 = 根；第 i+1 行 = 第 i 行选中节点的子级（无子级则止）
const rows = computed<DraftNode[][]>(() => {
  const out: DraftNode[][] = [draft.value]
  let list = draft.value
  for (const id of selectedPath.value) {
    const next = findNode(list, id)?.children
    if (!next || !next.length) break
    out.push(next)
    list = next
  }
  return out
})

function findNode(list: DraftNode[], id: number): DraftNode | null {
  for (const n of list) {
    if (n.id === id) return n
    const hit = findNode(n.children, id)
    if (hit) return hit
  }
  return null
}

// 节点所在的兄弟数组（含自身的层级）
function findList(list: DraftNode[], id: number): DraftNode[] | null {
  if (list.some((n) => n.id === id)) return list
  for (const n of list) {
    const hit = findList(n.children, id)
    if (hit) return hit
  }
  return null
}

function countOf(id: number): number {
  return id > 0 ? counts.value[id] ?? 0 : 0
}

function sumSubtree(n: DraftNode): number {
  return countOf(n.id) + n.children.reduce((s, c) => s + sumSubtree(c), 0)
}

function validateName(raw: string, siblings: DraftNode[], self?: DraftNode): string | null {
  const name = raw.trim()
  if (!name) return '标签名不能为空'
  if ([...name].length > 32) return '标签名超过 32 字'
  if (siblings.some((s) => s !== self && s.name === name)) return `同级已存在「${name}」`
  return null
}

// ---- 点击下钻 / 双击改名 / 长按拖拽 ----

function onCardClick(row: number, node: DraftNode) {
  if (suppressClick) {
    suppressClick = false
    return
  }
  if (floating.value !== null) {
    floating.value = null // 点其他卡片先退出浮动态，本次点击消费掉
    return
  }
  if (editing.value || adding.value) return
  selectedPath.value = [...selectedPath.value.slice(0, row), node.id]
}

// 层级配色：同级同色、不同级不同色，1-5 级各一色（更深层沿用第五级）
function lvClass(row: number): string {
  return `lv-${Math.min(row + 1, 5)}`
}

// 悬停弹层里点子级 = 直接下钻（与点击卡片同一选中语义）
function peekDrill(row: number, child: DraftNode) {
  if (editing.value || adding.value) return
  selectedPath.value = [...selectedPath.value.slice(0, row), child.id]
}

function startRename(node: DraftNode) {
  if (floating.value !== null || editing.value || adding.value) return
  editing.value = { id: node.id, name: node.name }
}

function commitRename(fromBlur = false) {
  const ed = editing.value
  if (!ed) return
  const name = ed.name.trim()
  if (!name && fromBlur) {
    editing.value = null
    return
  }
  const list = findList(draft.value, ed.id)
  const node = list ? findNode(list, ed.id) : null
  if (!list || !node) {
    editing.value = null
    return
  }
  const err = validateName(ed.name, list, node)
  if (err) {
    ElMessage.warning(err)
    return
  }
  // 改名语义 = 删除旧名 + 新增新名：被引用时前置拦截（与保存时服务端拦截同款文案逻辑）
  const used = sumSubtree(node)
  if (node.id > 0 && used > 0) {
    ElMessage.warning(`「${node.name}」被 ${used} 道题使用，改名（=删除＋新增）需先在题目上解绑`)
    editing.value = null
    return
  }
  node.name = name
  editing.value = null
}

function removeNode(node: DraftNode) {
  const used = sumSubtree(node)
  if (used > 0) {
    ElMessage.warning(`「${node.name}」及其子级被 ${used} 道题使用，请先在题目上解绑`)
    return
  }
  const list = findList(draft.value, node.id)
  const idx = list ? list.findIndex((n) => n.id === node.id) : -1
  if (list && idx >= 0) list.splice(idx, 1)
  const pi = selectedPath.value.indexOf(node.id)
  if (pi >= 0) selectedPath.value = selectedPath.value.slice(0, pi)
  floating.value = null
}

function addParentId(row: number): number {
  return row === 0 ? 0 : selectedPath.value[row - 1]
}

function startAdd(row: number) {
  if (editing.value || adding.value) return
  adding.value = { parentId: addParentId(row), name: '' }
}

// 空标签（无子级）卡片下方的白色 ＋ 小卡：直接以该标签为父级新建子标签
function startAddUnder(node: DraftNode) {
  if (editing.value || adding.value) return
  adding.value = { parentId: node.id, name: '' }
}

function commitAdd(fromBlur = false) {
  const ad = adding.value
  if (!ad) return
  const name = ad.name.trim()
  if (!name && fromBlur) {
    adding.value = null
    return
  }
  const list = ad.parentId === 0 ? draft.value : findNode(draft.value, ad.parentId)?.children
  if (!list) {
    adding.value = null
    return
  }
  const err = validateName(ad.name, list)
  if (err) {
    ElMessage.warning(err)
    return
  }
  if (total.value >= 500) {
    ElMessage.warning('标签总数已达上限 500')
    return
  }
  tempId -= 1
  list.push({ id: tempId, name, children: [] })
  adding.value = null
  // 建完即下钻到父级展开新行（mini-add 首子级场景立即可见；行尾 + 与根行场景为幂等不变）
  if (ad.parentId !== 0) {
    const prow = rows.value.findIndex((l) => l.some((n) => n.id === ad.parentId))
    if (prow >= 0) selectedPath.value = [...selectedPath.value.slice(0, prow), ad.parentId]
  }
}

// 长按（500ms）进浮动态：卡片抬升、出现 ×、可直接拖动换位（同级）；
// 阈值前松手 = 普通点击下钻，移动超 8px 视为滚动意图取消长按。
let pressTimer: ReturnType<typeof setTimeout> | undefined
let suppressClick = false
let pressStart = { x: 0, y: 0 }
let dragCleanup: (() => void) | null = null

function onCardPointerDown(e: PointerEvent, row: number, node: DraftNode) {
  if (e.button !== 0 || editing.value || adding.value) return
  cancelPress()
  pressStart = { x: e.clientX, y: e.clientY }
  const rowEl = (e.currentTarget as HTMLElement).closest('.tag-row')
  if (floating.value === node.id) {
    // 浮动态中再次按住：免等待直接拖
    suppressClick = true
    beginDrag(rowEl, node)
    return
  }
  const earlyMove = (ev: PointerEvent) => {
    if (Math.hypot(ev.clientX - pressStart.x, ev.clientY - pressStart.y) > 8) cancelPress()
  }
  const up = () => cancelPress()
  window.addEventListener('pointermove', earlyMove)
  window.addEventListener('pointerup', up)
  pressTimer = setTimeout(() => {
    window.removeEventListener('pointermove', earlyMove)
    window.removeEventListener('pointerup', up)
    pressTimer = undefined
    floating.value = node.id
    suppressClick = true
    beginDrag(rowEl, node)
  }, 500)
}

function cancelPress() {
  if (pressTimer) {
    clearTimeout(pressTimer)
    pressTimer = undefined
  }
}

function beginDrag(rowEl: Element | null, node: DraftNode) {
  draggingId.value = node.id
  const move = (ev: PointerEvent) => dragMove(ev, rowEl, node)
  const up = () => {
    draggingId.value = null
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
    dragCleanup = null
  }
  dragCleanup = up
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
}

// 拖动换位：按指针 x 与兄弟卡片中点比较，实时在兄弟数组内 splice（草稿即视即所得）
function dragMove(ev: PointerEvent, rowEl: Element | null, node: DraftNode) {
  const cards = Array.from(rowEl?.querySelectorAll<HTMLElement>('.tag-card:not(.add-card)') ?? [])
  const list = findList(draft.value, node.id)
  if (!list) return
  const from = list.findIndex((n) => n.id === node.id)
  if (from < 0) return
  let ins = cards.length
  for (let i = 0; i < cards.length; i++) {
    const el = cards[i]
    if (Number(el.dataset.id) === node.id) continue
    const r = el.getBoundingClientRect()
    if (ev.clientX < r.left + r.width / 2) {
      ins = i
      break
    }
  }
  const to = ins > from ? ins - 1 : ins
  if (to !== from) {
    list.splice(from, 1)
    list.splice(to, 0, node)
  }
}

function exitFloating() {
  floating.value = null
}

// 行内输入框挂载即聚焦（模板里写 TS 断言解析不了，收敛到 script）
function focusRef(el: unknown) {
  if (el) (el as { focus: () => void }).focus()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && floating.value !== null && !editing.value && !adding.value) {
    exitFloating()
  }
}

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) {
    e.preventDefault()
    e.returnValue = ''
  }
}

// ---- 载入与保存 ----

function fromServer(nodes: TagNode[]): DraftNode[] {
  return nodes.map((n) => ({ id: n.id, name: n.name, children: fromServer(n.children ?? []) }))
}

function toPayload(list: DraftNode[]): TagNode[] {
  return list.map((n) => ({
    id: n.id > 0 ? n.id : 0, // 新节点由服务端按名字取号；已有 ID 回传仅备查，身份以名字对齐为准
    name: n.name,
    children: toPayload(n.children),
  }))
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('beforeunload', onBeforeUnload)
  try {
    const r = await tagApi.adminTree()
    draft.value = fromServer(r.nodes)
    counts.value = r.counts
    savedJson.value = JSON.stringify(draft.value)
  } catch {
    // 错误提示已由 axios 拦截器统一弹出
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('beforeunload', onBeforeUnload)
  cancelPress()
  dragCleanup?.()
})

async function onSave() {
  if (!dirty.value || saving.value) return
  try {
    await ElMessageBox.confirm(
      `将整树覆盖为 ${total.value} 个标签：同名标签保留引用不变，被删除或改名的标签若仍被题目引用会拦截。确定保存？`,
      '保存标签树',
      { type: 'warning', confirmButtonText: '保存', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  saving.value = true
  blockedProblems.value = null
  try {
    const r = await tagApi.save(toPayload(draft.value))
    ElMessage.success(`标签树已保存（${r.count} 个标签）`)
    // 保存后新节点已取号，重拉面板数据（树 + 引用数），名字映射广播刷新
    const fresh = await tagApi.adminTree()
    draft.value = fromServer(fresh.nodes)
    counts.value = fresh.counts
    savedJson.value = JSON.stringify(draft.value)
    selectedPath.value = []
    await tagStore.load()
  } catch (e) {
    // 拦截器已 toast 文案；这里取 failData 附带的引用题目明细做表格展示
    blockedProblems.value =
      (e as Error & { data?: { problems?: TagRefProblem[] } }).data?.problems ?? []
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-card v-loading="loading">
    <template #header>
      <div class="panel-header">
        <span>标签树</span>
        <div class="header-actions">
          <el-tag v-if="dirty" type="warning" size="small">未保存</el-tag>
          <span class="dim">{{ total }} 个标签</span>
          <el-button type="primary" :loading="saving" :disabled="!dirty" @click="onSave">
            保存标签树
          </el-button>
        </div>
      </div>
    </template>
    <el-alert type="info" :closable="false" class="howto">
      <p>
        点击卡片展开下一级；双击改名；长按卡片浮起后可拖动换位（同级）、点 × 删除；行尾白卡 ＋
        新增该级标签；无子级的标签下方有白色 ＋ 小卡，点它直接新建子标签。卡片右上角数字 =
        被题目引用数；颜色区分层级（一至五级各一色，更深层沿用第五级色）。
      </p>
      <p>
        身份按名字对齐：同名标签保留 ID、题目引用不断；被引用的标签删除/改名会被拦截（改名 =
        删除＋新增），需先在题目上解绑。保存为整树覆盖。
      </p>
    </el-alert>

    <div class="tag-board" @click.self="exitFloating">
      <div v-for="(list, row) in rows" :key="row" class="tag-row" @click.self="exitFloating">
        <el-popover
          v-for="node in list"
          :key="node.id"
          placement="bottom-start"
          trigger="hover"
          :show-after="150"
          :hide-after="60"
          :disabled="!node.children.length || floating !== null || draggingId !== null"
          popper-class="tag-peek-popper"
        >
          <template #reference>
            <div class="tag-cell">
            <div
              :data-id="node.id"
              class="tag-card"
              :class="[
                {
                  selected: selectedPath[row] === node.id,
                  floating: floating === node.id,
                  dragging: draggingId === node.id,
                },
                lvClass(row),
              ]"
              @click="onCardClick(row, node)"
              @dblclick="startRename(node)"
              @pointerdown="onCardPointerDown($event, row, node)"
            >
            <span v-if="node.id > 0" class="count-badge" :class="{ hot: countOf(node.id) > 0 }">
              {{ countOf(node.id) }}
            </span>
            <el-input
              v-if="editing && editing.id === node.id"
              v-model="editing.name"
              size="small"
              class="card-input"
              :ref="focusRef"
              @click.stop
              @dblclick.stop
              @keydown.enter.prevent="commitRename()"
              @keydown.esc.stop="editing = null"
              @blur="commitRename(true)"
            />
            <template v-else>{{ node.name }}</template>
            <span v-if="node.children.length" class="caret">▸</span>
            <span
              v-if="floating === node.id"
              class="del-x"
              title="删除（含子级）"
              @click.stop="removeNode(node)"
            >
              ×
            </span>
            </div>
            <div
              v-if="!node.children.length"
              class="mini-add"
              title="新增子标签"
              @click.stop="startAddUnder(node)"
            >
              <el-input
                v-if="adding && adding.parentId === node.id"
                v-model="adding.name"
                size="small"
                class="card-input"
                :ref="focusRef"
                @click.stop
                @keydown.enter.prevent="commitAdd()"
                @keydown.esc.stop="adding = null"
                @blur="commitAdd(true)"
              />
              <template v-else>＋</template>
            </div>
            </div>
          </template>
          <div class="peek-list">
            <div
              v-for="c in node.children"
              :key="c.id"
              class="peek-item"
              :class="lvClass(row + 1)"
              @click.stop="peekDrill(row, c)"
            >
              <span>{{ c.name }}</span>
              <span class="peek-count">{{ countOf(c.id) }}</span>
            </div>
          </div>
        </el-popover>
        <div class="tag-card add-card" title="新增标签" @click.stop="startAdd(row)">
          <el-input
            v-if="adding && adding.parentId === addParentId(row)"
            v-model="adding.name"
            size="small"
            class="card-input"
            :ref="focusRef"
            @click.stop
            @dblclick.stop
            @keydown.enter.prevent="commitAdd()"
            @keydown.esc.stop="adding = null"
            @blur="commitAdd(true)"
          />
          <template v-else>＋</template>
        </div>
      </div>
      <div v-if="!rows[0].length" class="dim empty-tip">（空树：用上面的 ＋ 建第一个标签）</div>
    </div>

    <template v-if="blockedProblems">
      <el-alert
        type="error"
        :closable="false"
        class="blocked-alert"
        title="保存被拒：以下题目仍在使用将被删除的标签，请先在题目上解绑（编辑题目 → 标签）"
      />
      <el-table :data="blockedProblems" size="small">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="title" label="题目" min-width="240" />
        <el-table-column label="使用的标签" min-width="260">
          <template #default="{ row }">
            <el-tag v-for="t in row.tags" :key="t" size="small" class="tag-gap">{{ t }}</el-tag>
            <span v-if="!row.tags?.length" class="dim">（无）</span>
          </template>
        </el-table-column>
      </el-table>
    </template>
  </el-card>
</template>

<style scoped>
.caret {
  margin-left: 4px;
  font-size: 12px;
  opacity: 0.55;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.howto {
  margin-bottom: 12px;
}

.howto p {
  margin: 2px 0;
}

.tag-board {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: flex-start;
}

/* 纵向 cell：卡片 + 空标签的 mini-add 小卡 */
.tag-cell {
  display: inline-flex;
  flex-direction: column;
  align-items: stretch;
  gap: 4px;
}

.mini-add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 6px;
  background: #fff;
  border: 1px dashed var(--el-border-color);
  border-radius: 4px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 18px;
  cursor: pointer;
  user-select: none;
  touch-action: none;
}

.mini-add:hover {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}

.mini-add .card-input {
  width: 100%;
}

.tag-card {
  position: relative;
  display: inline-flex;
  align-items: center;
  padding: 6px 14px;
  /* 层级色变量：由 .lv-1~.lv-5 按行注入，默认一级（primary）兜底 */
  --lv: var(--el-color-primary);
  --lv-bg: var(--el-color-primary-light-9);
  --lv-border: var(--el-color-primary-light-7);
  background: var(--lv-bg);
  border: 1px solid var(--lv-border);
  border-radius: 6px;
  cursor: pointer;
  user-select: none;
  touch-action: none;
  transition: box-shadow 0.15s, transform 0.15s, background 0.15s;
}

.tag-card:hover {
  border-color: var(--lv);
}

/* 选中（已下钻）：本层级色实底，与未选浅色区分 */
.tag-card.selected {
  background: var(--lv);
  border-color: var(--lv);
  color: #fff;
}

/* 五级各占一款基色：1 蓝 / 2 绿 / 3 橙 / 4 红 / 5 灰 */
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

/* 长按浮起：抬升 + 阴影，可拖可删 */
.tag-card.floating {
  box-shadow: var(--el-box-shadow-light);
  transform: translateY(-2px);
  cursor: grabbing;
  z-index: 2;
}

.tag-card.dragging {
  opacity: 0.85;
}

.count-badge {
  position: absolute;
  top: -7px;
  right: -7px;
  min-width: 16px;
  height: 16px;
  line-height: 16px;
  padding: 0 4px;
  border-radius: 8px;
  background: var(--el-color-info);
  color: #fff;
  font-size: 12px;
  text-align: center;
}

.count-badge.hot {
  background: var(--el-color-primary);
}

.tag-card.selected .count-badge.hot {
  background: var(--el-color-primary-light-3);
}

.del-x {
  margin-left: 8px;
  width: 18px;
  height: 18px;
  line-height: 16px;
  text-align: center;
  border-radius: 50%;
  background: var(--el-color-danger);
  color: #fff;
  font-size: 12px;
}

.del-x:hover {
  background: var(--el-color-danger-dark-2);
}

.add-card {
  min-width: 40px;
  justify-content: center;
  background: #fff;
  border: 1px dashed var(--el-border-color);
  color: var(--el-text-color-secondary);
  font-size: 16px;
}

.add-card:hover {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}

.card-input {
  width: 120px;
}

.empty-tip {
  padding: 4px 0;
}

.blocked-alert {
  margin-top: 12px;
}

.tag-gap {
  margin: 0 6px 4px 0;
}
</style>

<style>
/* 悬停预览弹层内容 teleport 到 body，脱离组件作用域——全局类以 popper-class 收敛命名 */
.tag-peek-popper .peek-list {
  display: flex;
  flex-direction: column;
  max-width: 280px;
  max-height: 260px;
  overflow: auto;
}

.tag-peek-popper .peek-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 14px;
  cursor: pointer;
}

.tag-peek-popper .peek-item:hover {
  background: var(--el-fill-color-light);
}

/* 弹层子级按其层级着色（与卡片五级色一致） */
.tag-peek-popper .peek-item.lv-1 {
  color: var(--el-color-primary);
}

.tag-peek-popper .peek-item.lv-2 {
  color: var(--el-color-success);
}

.tag-peek-popper .peek-item.lv-3 {
  color: var(--el-color-warning);
}

.tag-peek-popper .peek-item.lv-4 {
  color: var(--el-color-danger);
}

.tag-peek-popper .peek-item.lv-5 {
  color: var(--el-color-info);
}

.tag-peek-popper .peek-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>

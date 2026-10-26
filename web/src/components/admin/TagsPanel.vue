<script setup lang="ts">
// 标签树管理（v0.5.5 建面板，卡片化交互 + 悬停导航，仅 root）：
// 悬停到哪层展开到哪层（导航全靠悬停，移开即收起）；单击卡片在下方列出只挂此标签的题目，
// 双击进编辑态（原地改名 + × 删除）；长按拖动 = 纯同级换位（与删除解绑）；＋ 默认隐藏、悬停行才显示。
// 草稿为本地状态，保存仍是整树覆盖 + 二次确认 + 服务端被引用拦截；
// 身份按名字对齐（改名 = 删除＋新增），被引用节点的删除/改名在交互层前置拦截。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { problemApi, tagApi } from '@/api'
import type { ProblemListItem, TagNode, TagRefProblem } from '@/types'
import { useMetaTreeStore } from '@/stores/metaTree'
import { useTagTreeStore } from '@/stores/tagtree'

// tree（2026-10-25 双树）：knowledge = 知识树（默认），meta = 来源标签树。
// 面板交互完全同构，仅数据源/接口参数/角标字段不同。
const props = withDefaults(defineProps<{ tree?: 'knowledge' | 'meta' }>(), { tree: 'knowledge' })

const emit = defineEmits<{ (e: 'update:dirty', v: boolean): void }>()

const tagStore = props.tree === 'meta' ? useMetaTreeStore() : useTagTreeStore()

// 文案按树切换（2026-10-25）：知识树说「知识点」，来源标签树说「标签」
const noun = computed(() => (props.tree === 'meta' ? '标签' : '知识点'))
const treeName = computed(() => (props.tree === 'meta' ? '标签树' : '知识树'))

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
// 本次保存待删且仍被引用的标签 ID（failData.removed_ids）——逐行/一键解绑的目标
const removedTagIds = ref<number[]>([])
const unbinding = ref(false)
const path = ref<number[]>([]) // 悬停路径：path[i] = 第 i 层悬停的节点 id，其子级即第 i+1 行——悬停即「当前在哪」
const search = ref('') // 标签搜索：非空时整树按名匹配平铺展示（悬停导航/＋/拖拽停用）
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

// 展示行：第 0 行 = 根；第 i+1 行 = 悬停路径上第 i 个节点的子级——
// 悬停到哪就展开到哪（depth 为层级语义），移开整块后收起。
// 搜索时整体替换为「全部命中标签」的平铺行（每个节点按其在树中的真实层级着色）。
interface TagItem {
  node: DraftNode
  depth: number
}

interface TagRow {
  items: TagItem[]
  depth: number
}

const rows = computed<TagRow[]>(() => {
  const q = search.value.trim().toLowerCase()
  if (q) {
    const hits: TagItem[] = []
    const walk = (list: DraftNode[], depth: number) => {
      for (const n of list) {
        if (n.name.toLowerCase().includes(q)) hits.push({ node: n, depth })
        walk(n.children, depth + 1)
      }
    }
    walk(draft.value, 0)
    return [{ items: hits, depth: 0 }]
  }
  const out: TagRow[] = [{ items: draft.value.map((n) => ({ node: n, depth: 0 })), depth: 0 }]
  for (let d = 0; d < path.value.length; d++) {
    const next = findNode(draft.value, path.value[d])?.children
    if (!next || !next.length) break
    out.push({ items: next.map((n) => ({ node: n, depth: d + 1 })), depth: d + 1 })
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

// 节点在树中的真实层级（1 起；根下一层 = 1）——v0.20 标签树限 5 级，新增入口按它放行
function depthOf(id: number): number {
  const walk = (list: DraftNode[], depth: number): number => {
    for (const n of list) {
      if (n.id === id) return depth
      const hit = walk(n.children, depth + 1)
      if (hit) return hit
    }
    return 0
  }
  return walk(draft.value, 1) || 1
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
  if (!name) return `${noun.value}名不能为空`
  if ([...name].length > 32) return `${noun.value}名超过 32 字`
  if (siblings.some((s) => s !== self && s.name === name)) return `同级已存在「${name}」`
  return null
}

// ---- 点击下钻 / 双击改名 / 长按拖拽 ----

// 单击 = 在面板下方列出只挂此标签的题目（题库接口 tags 精确匹配，与角标同口径；20 条/页可翻页）
const PAGE_SIZE = 20
const tagProblems = ref<{ id: number; name: string; total: number; list: ProblemListItem[] } | null>(null)
const problemsPage = ref(1)
const problemsLoading = ref(false)
let problemsSeq = 0

function onCardClick(node: DraftNode) {
  if (suppressClick) {
    suppressClick = false
    return
  }
  if (floating.value !== null) {
    floating.value = null // 点其他卡片先退出浮动态，本次点击消费掉
    return
  }
  if (editing.value || adding.value) return
  void fetchTagProblems(node.id, node.name, 1)
}

async function fetchTagProblems(id: number, name: string, page: number) {
  const seq = ++problemsSeq
  if (id <= 0) {
    // 新建未保存的标签还没有 ID，不可能被引用
    tagProblems.value = { id, name, total: 0, list: [] }
    return
  }
  problemsLoading.value = true
  try {
    // 来源树走 meta= 子树展开筛选（选父节点含区县），知识树维持 tags= 精确匹配
    const r =
      props.tree === 'meta'
        ? await problemApi.list(page, PAGE_SIZE, undefined, undefined, undefined, [id])
        : await problemApi.list(page, PAGE_SIZE, undefined, [id])
    if (seq !== problemsSeq) return
    tagProblems.value = { id, name, total: r.total, list: r.list }
    problemsPage.value = page
  } catch {
    // 错误提示已由 axios 拦截器统一弹出
  } finally {
    if (seq === problemsSeq) problemsLoading.value = false
  }
}

function onProblemsPage(page: number) {
  if (!tagProblems.value) return
  void fetchTagProblems(tagProblems.value.id, tagProblems.value.name, page)
}

// 双击 = 原地改名（输入框 + 右侧 ×）
function startRename(node: DraftNode) {
  if (floating.value !== null || editing.value || adding.value) return
  editing.value = { id: node.id, name: node.name }
}

// 层级配色：同级同色、不同级不同色，1-5 级各一色（更深层沿用第五级）
function lvClass(row: number): string {
  return `lv-${Math.min(row + 1, 5)}`
}

// 悬停导航：移到某卡上，路径截断到该层并追加它（子级行随即展开），移开 180ms 后
// 整体收起（延时桥接跨行间隙）；编辑/输入/拖拽期间冻结——行结构就是当前编辑上下文。
let hoverTimer: ReturnType<typeof setTimeout> | undefined

function hoverLocked(): boolean {
  return !!(editing.value || adding.value || draggingId.value !== null)
}

function onCardHover(row: number, node: DraftNode) {
  if (hoverLocked() || search.value.trim()) return
  if (hoverTimer) {
    clearTimeout(hoverTimer)
    hoverTimer = undefined
  }
  path.value = [...path.value.slice(0, row), node.id]
}

function scheduleHoverClear() {
  if (hoverLocked()) return
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
// 与 TagChips 同款（题库筛选/建题表单/后台管理三处标签树行为一致）。
function onWheel(e: WheelEvent) {
  const el = e.currentTarget as HTMLElement
  const max = el.scrollWidth - el.clientWidth
  if (max <= 0) return
  e.preventDefault()
  const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
  if (delta === 0) return
  el.scrollLeft = Math.max(0, Math.min(max, el.scrollLeft + delta))
}

// ＋ 默认全隐藏：显示悬停行（路径最深处）的行尾 ＋ 与该空标签下的 mini-add；
// 无悬停时回退到根行（保证任何时候都能建一级标签）。
function showRowAdd(depth: number): boolean {
  if (search.value.trim()) return false
  if (depth >= 5) return false // 标签树限 5 级：第 5 级行不再出「加子级」入口
  if (adding.value && addParentId(depth) === adding.value.parentId) return true
  if (path.value.length) return depth === path.value.length - 1
  return depth === 0
}

function showMini(node: DraftNode): boolean {
  if (search.value.trim() || node.children.length) return false
  if (depthOf(node.id) >= 5) return false // 已是第 5 级：不能再建子级
  if (adding.value?.parentId === node.id) return true
  const last = path.value[path.value.length - 1]
  return last !== undefined && node.id === last
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
  const pi = path.value.indexOf(node.id)
  if (pi >= 0) path.value = path.value.slice(0, pi)
  if (editing.value?.id === node.id) editing.value = null
  floating.value = null
}

function addParentId(row: number): number {
  return row === 0 ? 0 : path.value[row - 1]
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
  if (total.value >= 10000) {
    ElMessage.warning(`${noun.value}总数已达上限 10000`)
    return
  }
  tempId -= 1
  list.push({ id: tempId, name, children: [] })
  adding.value = null
  // 建完即下钻到父级展开新行（mini-add 首子级场景立即可见；行尾 + 与根行场景为幂等不变）
  if (ad.parentId !== 0) {
    const prow = rows.value.findIndex((l) => l.items.some((it) => it.node.id === ad.parentId))
    if (prow >= 0) path.value = [...path.value.slice(0, prow), ad.parentId]
  }
}

// 长按（500ms）进浮动态：卡片抬升、出现 ×、可直接拖动换位（同级）；
// 阈值前松手 = 普通点击下钻，移动超 8px 视为滚动意图取消长按。
let pressTimer: ReturnType<typeof setTimeout> | undefined
let suppressClick = false
let pressStart = { x: 0, y: 0 }
let dragCleanup: (() => void) | null = null

function onCardPointerDown(e: PointerEvent, row: number, node: DraftNode) {
  if (e.button !== 0 || editing.value || adding.value || search.value.trim()) return
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
    const r = await tagApi.adminTree(props.tree)
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
  if (hoverTimer) clearTimeout(hoverTimer)
})

// ---- 模板下载 / CSV 导入（批量维护标签树）----
// 模板 = 当前草稿树导出为五列表格 CSV（一级~五级，每行一条完整路径；空树给示例行），
// 带 UTF-8 BOM 保证 Excel 直接打开不乱码；导入解析同格式 CSV 覆盖当前草稿
// （保存仍走既有「二次确认 + 整树覆盖 + 被引用禁删」管道，导入本身不落库）。
const IMPORT_COLUMNS = ['一级', '二级', '三级', '四级', '五级']
const importInput = ref<HTMLInputElement>()

function csvEscape(v: string): string {
  return /[",\r\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v
}

function downloadTemplate() {
  const rows: string[][] = [IMPORT_COLUMNS]
  const walk = (list: DraftNode[], path: string[]) => {
    for (const n of list) {
      const p = [...path, n.name]
      rows.push([...p, '', '', '', ''].slice(0, 5))
      walk(n.children, p)
    }
  }
  walk(draft.value, [])
  if (rows.length === 1) rows.push(['示例一级', '示例二级', '', '', ''], ['示例一级', '', '', '', ''])
  const blob = new Blob(['\uFEFF' + rows.map((r) => r.map(csvEscape).join(',')).join('\r\n')], {
    type: 'text/csv;charset=utf-8',
  })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `${treeName.value}模板.csv`
  a.click()
  URL.revokeObjectURL(a.href)
}

function pickImport() {
  importInput.value?.click()
}

// 最小 CSV 解析：支持引号包裹（字段内逗号/换行/双引号转义）与 \r\n、\n 两种行尾
function parseCSV(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let cur = ''
  let inQ = false
  for (let i = 0; i < text.length; i++) {
    const ch = text[i]
    if (inQ) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          cur += '"'
          i++
        } else inQ = false
      } else cur += ch
    } else if (ch === '"') inQ = true
    else if (ch === ',') {
      row.push(cur)
      cur = ''
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++
      row.push(cur)
      cur = ''
      if (row.some((c) => c.trim() !== '')) rows.push(row)
      row = []
    } else cur += ch
  }
  row.push(cur)
  if (row.some((c) => c.trim() !== '')) rows.push(row)
  return rows
}

function countNodes(list: DraftNode[]): number {
  return list.reduce((s, n) => s + 1 + countNodes(n.children), 0)
}

// CSV 编码探测解码：模板自带 UTF-8 BOM；但 Excel「另存为 CSV（默认）」产出的是 GBK/ANSI
// 且无 BOM——按 BOM → 严格 UTF-8 → GBK 链式试解（GBK 文件几乎必然不是合法 UTF-8，顺序可靠）
function decodeCsvText(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  if (bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf) {
    return new TextDecoder('utf-8').decode(bytes.subarray(3))
  }
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch {
    try {
      return new TextDecoder('gbk', { fatal: true }).decode(bytes)
    } catch {
      return new TextDecoder('utf-8').decode(bytes)
    }
  }
}

async function onImportFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 允许重复选择同一文件
  if (!file) return
  const rows = parseCSV(decodeCsvText(await file.arrayBuffer()))
  const body = rows[0] && rows[0].map((c) => c.trim()).join(',') === IMPORT_COLUMNS.join(',') ? rows.slice(1) : rows
  if (!body.length) {
    ElMessage.warning(`文件里没有${noun.value}数据`)
    return
  }
  // 重建树：同名同级自动合并；新节点用负数临时 id（保存时由服务端按名取号）
  const roots: DraftNode[] = []
  for (const cells of body) {
    const names = cells.slice(0, 5).map((c) => c.trim()).filter(Boolean)
    let list = roots
    for (const name of names) {
      let hit = list.find((n) => n.name === name)
      if (!hit) {
        tempId -= 1
        hit = { id: tempId, name, children: [] }
        list.push(hit)
      }
      list = hit.children
    }
  }
  const totalImported = countNodes(roots)
  if (!totalImported) {
    ElMessage.warning(`文件里没有有效的${noun.value}（列：一级~五级，每行一条路径）`)
    return
  }
  if (totalImported > 10000) {
    ElMessage.warning(`导入的${noun.value}共 ${totalImported} 个，超出上限 10000`)
    return
  }
  const tooLong = roots.flatMap(function walk(n: DraftNode): string[] {
    return [n.name, ...n.children.flatMap(walk)].filter((x) => x.length > 32)
  })
  if (tooLong.length) {
    ElMessage.warning(`存在超过 32 字的${noun.value}名（如「${tooLong[0]}」），请改短后再导入`)
    return
  }
  try {
    await ElMessageBox.confirm(
      `导入 ${totalImported} 个${noun.value}将覆盖当前草稿（含未保存的修改）；导入后需再点「保存${treeName.value}」才会生效。确定导入？`,
      `导入${treeName.value}` + '',
      { type: 'warning', confirmButtonText: '导入', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  draft.value = roots
  path.value = []
  ElMessage.success(`已导入 ${totalImported} 个${noun.value}到草稿，检查无误后点「保存${treeName.value}」生效`)
}

async function onSave() {
  if (!dirty.value || saving.value) return
  try {
    await ElMessageBox.confirm(
      `将整树覆盖为 ${total.value} 个${noun.value}：同名标签保留引用不变，被删除或改名的${noun.value}若仍被题目引用会拦截。确定保存？`,
      `保存${treeName.value}` + '',
      { type: 'warning', confirmButtonText: '保存', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  await doSave()
}

// doSave 实际保存（onSave 确认后与解绑后自动重试共用；重试不再弹确认——
// 用户已确认过本次草稿，解绑只是清除了拦截原因）
async function doSave() {
  saving.value = true
  blockedProblems.value = null
  removedTagIds.value = []
  try {
    const r = await tagApi.save(toPayload(draft.value), props.tree)
    ElMessage.success(`${treeName.value}已保存（${r.count} 个${noun.value}）`)
    // 保存后新节点已取号，重拉面板数据（树 + 引用数），名字映射广播刷新
    const fresh = await tagApi.adminTree(props.tree)
    draft.value = fromServer(fresh.nodes)
    counts.value = fresh.counts
    savedJson.value = JSON.stringify(draft.value)
    path.value = []
    await tagStore.load()
  } catch (e) {
    // 拦截器已 toast 文案；这里取 failData 附带的引用题目明细做表格展示，
    // removed_ids = 本次保存待删且仍被引用的标签 ID（逐行/一键解绑的目标）
    const data = (e as Error & { data?: { problems?: TagRefProblem[]; removed_ids?: number[] } }).data
    blockedProblems.value = data?.problems ?? []
    removedTagIds.value = data?.removed_ids ?? []
  } finally {
    saving.value = false
  }
}

// 解绑（unbindAll=true = 拦截明细全部题目；否则只解 row 那一道）：
// 从题目上摘除本次待删标签（其余标签不动），成功后清掉对应行；
// 明细清空即拦截原因消失，自动重试保存。一键在动手前二次确认（批量改题目）。
async function unbind(row?: TagRefProblem) {
  const problems = blockedProblems.value ?? []
  const ids = row ? [row.id] : problems.map((p) => p.id)
  if (!ids.length || !removedTagIds.value.length) return
  if (!row) {
    try {
      await ElMessageBox.confirm(
        `将从 ${ids.length} 道题上移除待删除的标签（其余标签保留），随后自动重试保存。确定？`,
        '一键解绑',
        { type: 'warning', confirmButtonText: '解绑并重试', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
  }
  unbinding.value = true
  try {
    await tagApi.unbind(ids, removedTagIds.value, props.tree)
    if (row) {
      blockedProblems.value = problems.filter((p) => p.id !== row.id)
      ElMessage.success(`已从「#${row.id} ${row.title}」解绑`)
    } else {
      ElMessage.success(`已从 ${ids.length} 道题解绑`)
    }
    if (!blockedProblems.value?.length) {
      blockedProblems.value = null
      await doSave()
    }
  } finally {
    unbinding.value = false
  }
}
</script>

<template>
  <el-card v-loading="loading">
    <template #header>
      <div class="panel-header">
        <span>{{ treeName }}</span>
        <div class="header-actions">
          <el-tag v-if="dirty" type="warning" size="small">未保存</el-tag>
          <span class="dim">{{ total }} 个{{ noun }}</span>
          <el-button @click="downloadTemplate">下载{{ treeName }}</el-button>
          <el-button @click="pickImport">导入{{ treeName }}</el-button>
          <el-button type="primary" :loading="saving" :disabled="!dirty" @click="onSave">
            保存{{ treeName }}
          </el-button>
          <input ref="importInput" type="file" accept=".csv" class="import-input" @change="onImportFile" />
        </div>
      </div>
    </template>
    <el-alert type="info" :closable="false" class="howto">
      <p>
        悬停展开下一级；单击列出包含此{{ noun }}的题目；双击改名（回车确认）；点右侧 × 删除；长按拖动换位；
        悬停行尾或空{{ noun }}下方的 ＋ 新增。修改后点右上角「保存{{ treeName }}」生效，被题目使用的{{ noun }}需先解绑才能删除。
      </p>
    </el-alert>

    <el-input v-model="search" class="tag-search" clearable :placeholder="`搜索${noun}…`" />
    <div class="tag-board" @click.self="exitFloating" @mouseleave="scheduleHoverClear">
      <!-- 每级行独立横向滚动：无滚动条，滚轮只在鼠标所在行拦截+横移；各行各自记位置，
           行内有溢出即整行接管（滚到头也不带动页面下滑） -->
      <div class="tag-canvas" @click.self="exitFloating" @mouseenter="cancelHoverClear">
      <div
        v-for="(r, i) in rows"
        :key="i"
        class="tag-row"
        @click.self="exitFloating"
        @mouseenter="cancelHoverClear"
        @wheel="onWheel"
      >
        <TransitionGroup name="tag">
        <div
          v-for="item in r.items"
          :key="item.node.id"
          class="tag-cell"
          @mouseenter="onCardHover(item.depth, item.node)"
        >
          <div
            :data-id="item.node.id"
            class="tag-card"
            :class="[
              {
                active: path[item.depth] === item.node.id,
                floating: floating === item.node.id,
                dragging: draggingId === item.node.id,
              },
              lvClass(item.depth),
            ]"
            @click="onCardClick(item.node)"
            @dblclick="startRename(item.node)"
            @pointerdown="onCardPointerDown($event, item.depth, item.node)"
          >
            <span v-if="item.node.id > 0" class="count-badge" :class="{ hot: countOf(item.node.id) > 0 }">
              {{ countOf(item.node.id) }}
            </span>
            <el-input
              v-if="editing && editing.id === item.node.id"
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
            <template v-else>{{ item.node.name }}</template>
            <span v-if="item.node.children.length" class="caret">▸</span>
            <span
              v-if="editing && editing.id === item.node.id"
              class="del-x"
              title="删除（含子级）"
              @mousedown.prevent
              @click.stop="removeNode(item.node)"
            >
              ×
            </span>
          </div>
          <div
            v-if="showMini(item.node)"
            class="tag-card add-card"
            :title="`新增子级${noun}`"
            @click.stop="startAddUnder(item.node)"
          >
            <el-input
              v-if="adding && adding.parentId === item.node.id"
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
        </TransitionGroup>
        <div
          v-if="showRowAdd(r.depth)"
          class="tag-card add-card"
          :title="`新增${noun}`"
          @click.stop="startAdd(r.depth)"
        >
          <el-input
            v-if="adding && adding.parentId === addParentId(r.depth)"
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
      <div v-if="!rows[0].items.length" class="dim empty-tip">
        {{ search.trim() ? `没有匹配的${noun}` : `（空树：用上面的 ＋ 建第一个${noun}）` }}
      </div>
      <!-- 底部两行缓冲：末级展开后鼠标继续下移不出容器（进垫片取消收起计时） -->
      <div class="tag-board-pad" @mouseenter="cancelHoverClear" />
      </div>
    </div>

    <div v-if="tagProblems" class="tag-problems">
      <div class="tp-head">
        <span>包含{{ noun }}「{{ tagProblems.name }}」的题目：{{ tagProblems.total }} 道</span>
        <el-pagination
          v-if="tagProblems.total > PAGE_SIZE"
          v-model:current-page="problemsPage"
          :page-size="PAGE_SIZE"
          :total="tagProblems.total"
          layout="prev, pager, next"
          size="small"
          @current-change="onProblemsPage"
        />
        <el-button link size="small" @click="tagProblems = null">收起</el-button>
      </div>
      <div v-if="tagProblems.list.length" class="tp-links">
        <router-link v-for="p in tagProblems.list" :key="p.id" :to="`/problems/${p.id}`" target="_blank">
          #{{ p.id }} {{ p.title }}
        </router-link>
      </div>
      <div v-else class="dim">（还没有题目使用此{{ noun }}）</div>
    </div>

    <template v-if="blockedProblems">
      <div class="blocked-row">
        <el-alert
          type="error"
          :closable="false"
          class="blocked-alert"
          title="保存被拒：以下题目仍在使用将被删除的标签，解绑后自动重试保存"
        />
        <el-button
          type="warning"
          size="small"
          :loading="unbinding"
          @click="unbind()"
        >
          一键解绑全部
        </el-button>
      </div>
      <el-table stripe :data="blockedProblems" size="small">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="title" label="题目" min-width="240" />
        <el-table-column :label="`使用的${noun}`" min-width="260">
          <template #default="{ row }">
            <el-tag v-for="t in row.tags" :key="t" size="small" class="tag-gap">{{ t }}</el-tag>
            <span v-if="!row.tags?.length" class="dim">（无）</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button
              link
              type="warning"
              size="small"
              :disabled="unbinding"
              @click="unbind(row as TagRefProblem)"
            >
              解绑
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </template>
  </el-card>
</template>

<style scoped>
.caret {
  margin-left: 4px;
  font-size: var(--soj-fs-small);
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

.import-input {
  display: none; /* 隐藏的 CSV 选择器，由「导入标签树」按钮触发 */
}

.howto {
  margin-bottom: 12px;
}

.howto p {
  margin: 2px 0;
}

.tag-search {
  width: 240px;
  margin-bottom: 16px; /* 与首行标签留出空隙（原 10px 比行距 14 还小，显挤） */
}

.tag-board {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 底部一行缓冲垫片：末级展开后鼠标习惯性继续下移查找，若移出容器会触发
   mouseleave 收起——垫片在容器内且 mouseenter 取消收起计时，移动不断开 */
.tag-board-pad {
  height: 20px;
}

/* 每级一行独立滚动：行内不换行，超出本行宽度即横移（无滚动条，滚轮驱动见 onWheel）；
   每行各持独立 scrollLeft——滚哪行动哪行，其余行不受影响。
   padding/margin 成对：把裁剪盒上下各扩 6px（拖起卡片的放大与阴影不被行盒裁掉），
   视觉行距保持 gap 的 14px */
.tag-row {
  display: flex;
  flex-wrap: nowrap;
  gap: 8px;
  align-items: flex-start;
  width: 100%;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none; /* Firefox：隐藏滚动条 */
  padding-block: 6px;
  margin-block: -6px;
}

.tag-row::-webkit-scrollbar {
  display: none;
}

/* 画布列布局：行宽跟随容器，不再由最宽行撑开——溢出交给各行自己 */
.tag-canvas {
  display: flex;
  flex-direction: column;
  gap: 14px; /* 层级行距（一级/二级……之间） */
  min-width: 0;
  padding: 8px 12px; /* 与题库筛选区 .tag-row 同内距；顶部同时保留拖起卡片余量 */
  background: var(--el-fill-color-light); /* #f5f5f5 */
  border-radius: var(--el-border-radius-base);
}

/* 纵向 cell：卡片 + 空标签的 mini-add 小卡 */
.tag-cell {
  flex: none; /* 行内不换行模式：卡片+mini-add 纵向格不压缩 */
  display: inline-flex;
  flex-direction: column;
  align-items: stretch;
  gap: 4px;
}

.tag-card {
  position: relative;
  flex: none; /* 行内不换行模式：卡片不压缩，超出即由本行横移 */
  white-space: nowrap;
  display: inline-flex;
  align-items: center;
  padding: 4px 12px;
  line-height: 20px; /* 显式行高，防宿主容器（EP 表单 32px）渗入 */
  /* 层级色只来自全局 .lv-1~.lv-5（main.css）：不可写 --lv 兜底，scoped 特异度
     会压过全局把层级吞成一级蓝；新增白卡（add-card）不用 --lv，自带显式底色/边框 */
  background: var(--lv-bg);
  border: 1px solid var(--lv-border);
  border-radius: 6px;
  color: var(--lv);
  cursor: pointer;
  user-select: none;
  touch-action: none;
  transition: box-shadow 0.15s, transform 0.15s, background 0.15s;
}

.tag-card:hover {
  border-color: var(--lv);
}

/* 悬停路径上的卡（「当前在哪」）：本层级色实底 */
.tag-card.active {
  background: var(--lv);
  border-color: var(--lv);
  color: var(--el-color-white);
}

/* 长按浮起：抬升 + 阴影，可拖可删 */
.tag-card.floating {
  box-shadow: var(--el-box-shadow-light);
  transform: translateY(-2px);
  cursor: grabbing;
  z-index: 2;
}

.tag-card.dragging {
  transform: scale(1.05);
  box-shadow: var(--el-box-shadow-dark);
  opacity: 0.92;
}

/* 拖拽换位的 FLIP 动效：被挤开的卡片滑到新位置，而不是瞬间跳位 */
.tag-move {
  transition: transform 0.25s cubic-bezier(0.22, 1, 0.36, 1);
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
  color: var(--el-color-white);
  font-size: var(--soj-fs-small);
  text-align: center;
}

/* 被引用角标跟随标签自身层级色（--lv 由卡片上的全局 .lv-N 注入），不再一律蓝 */
.count-badge.hot {
  background: var(--lv);
}

/* 卡片激活（悬停路径层级色实底）时徽标反转为白底层级色字——
   原 lv-bg 反转是 8% 透明底，叠在实底卡上与卡面同色致角标看不清（2026-10-25 用户反馈）；
   描边用 lv-border，兜住激活卡渲染为浅底的场景；计数 0 的灰角标同样被本规则覆盖 */
.tag-card.active .count-badge {
  background: var(--el-bg-color, #fff);
  color: var(--lv);
  box-shadow: 0 0 0 1px var(--lv-border);
}

.del-x {
  margin-left: 8px;
  width: 18px;
  height: 18px;
  line-height: 16px;
  text-align: center;
  border-radius: 50%;
  background: var(--el-color-danger);
  color: var(--el-color-white);
  font-size: var(--soj-fs-small);
}

.del-x:hover {
  background: var(--el-color-danger-dark-2);
}

.add-card {
  min-width: 40px;
  justify-content: center;
  background: var(--el-bg-color);
  border: 1px dashed var(--el-border-color);
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-section);
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

.tag-problems {
  margin-top: 14px;
  padding-top: 10px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.tp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
}

.tp-links {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
}

.tp-links a {
  color: var(--el-color-primary);
  text-decoration: none;
}

.tp-links a:hover {
  text-decoration: underline;
}

/* 拦截提示行：alert 占满剩余宽度，「一键解绑全部」按钮右侧同行 */
.blocked-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-top: 12px;
}

.blocked-row .blocked-alert {
  flex: 1 1 auto;
  margin-top: 0;
}

.tag-gap {
  margin: 0 6px 4px 0;
}
</style>


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
    const r = await problemApi.list(page, PAGE_SIZE, undefined, [id])
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
// 已滚到头（或本就无溢出）时不拦截，滚轮自然下放滚页面——不在标签树上困住滚动。
// 与 TagChips 同款（题库筛选/建题表单/后台管理三处标签树行为一致）。
function onWheel(e: WheelEvent) {
  const el = e.currentTarget as HTMLElement
  const max = el.scrollWidth - el.clientWidth
  if (max <= 0) return
  const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
  if (delta === 0) return
  if ((delta > 0 && el.scrollLeft >= max - 0.5) || (delta < 0 && el.scrollLeft <= 0.5)) return
  e.preventDefault()
  el.scrollLeft = Math.max(0, Math.min(max, el.scrollLeft + delta))
}

// ＋ 默认全隐藏：显示悬停行（路径最深处）的行尾 ＋ 与该空标签下的 mini-add；
// 无悬停时回退到根行（保证任何时候都能建一级标签）。
function showRowAdd(depth: number): boolean {
  if (search.value.trim()) return false
  if (adding.value && addParentId(depth) === adding.value.parentId) return true
  if (path.value.length) return depth === path.value.length - 1
  return depth === 0
}

function showMini(node: DraftNode): boolean {
  if (search.value.trim() || node.children.length) return false
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
  if (total.value >= 500) {
    ElMessage.warning('标签总数已达上限 500')
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
  a.download = '标签树模板.csv'
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
    ElMessage.warning('文件里没有标签数据')
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
    ElMessage.warning('文件里没有有效的标签（列：一级~五级，每行一条路径）')
    return
  }
  if (totalImported > 500) {
    ElMessage.warning(`导入的标签共 ${totalImported} 个，超出上限 500`)
    return
  }
  const tooLong = roots.flatMap(function walk(n: DraftNode): string[] {
    return [n.name, ...n.children.flatMap(walk)].filter((x) => x.length > 32)
  })
  if (tooLong.length) {
    ElMessage.warning(`存在超过 32 字的标签名（如「${tooLong[0]}」），请改短后再导入`)
    return
  }
  try {
    await ElMessageBox.confirm(
      `导入 ${totalImported} 个标签将覆盖当前草稿（含未保存的修改）；导入后需再点「保存标签树」才会生效。确定导入？`,
      '导入标签树',
      { type: 'warning', confirmButtonText: '导入', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  draft.value = roots
  path.value = []
  ElMessage.success(`已导入 ${totalImported} 个标签到草稿，检查无误后点「保存标签树」生效`)
}

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
    path.value = []
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
          <el-button @click="downloadTemplate">下载标签树</el-button>
          <el-button @click="pickImport">导入标签树</el-button>
          <el-button type="primary" :loading="saving" :disabled="!dirty" @click="onSave">
            保存标签树
          </el-button>
          <input ref="importInput" type="file" accept=".csv" class="import-input" @change="onImportFile" />
        </div>
      </div>
    </template>
    <el-alert type="info" :closable="false" class="howto">
      <p>
        悬停展开下一级；单击列出包含此标签的题目；双击改名（回车确认）；点右侧 × 删除；长按拖动换位；
        悬停行尾或空标签下方的 ＋ 新增。修改后点右上角「保存标签树」生效，被题目使用的标签需先解绑才能删除。
      </p>
    </el-alert>

    <el-input v-model="search" class="tag-search" clearable placeholder="搜索标签…" />
    <div class="tag-board" @click.self="exitFloating" @mouseleave="scheduleHoverClear">
      <!-- 单一横向滚动容器：无滚动条，鼠标滚轮横移浏览当前层级 -->
      <div class="tag-scroll" @mouseenter="cancelHoverClear" @click.self="exitFloating" @wheel="onWheel">
        <div class="tag-canvas" @click.self="exitFloating">
      <div
        v-for="(r, i) in rows"
        :key="i"
        class="tag-row"
        @click.self="exitFloating"
        @mouseenter="cancelHoverClear"
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
            title="新增子标签"
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
          title="新增标签"
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
        {{ search.trim() ? '没有匹配的标签' : '（空树：用上面的 ＋ 建第一个标签）' }}
      </div>
      <!-- 底部两行缓冲：末级展开后鼠标继续下移不出容器（进垫片取消收起计时） -->
      <div class="tag-board-pad" @mouseenter="cancelHoverClear" />
        </div>
      </div>
    </div>

    <div v-if="tagProblems" class="tag-problems">
      <div class="tp-head">
        <span>包含标签「{{ tagProblems.name }}」的题目：{{ tagProblems.total }} 道</span>
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
      <div v-else class="dim">（还没有题目使用此标签）</div>
    </div>

    <template v-if="blockedProblems">
      <el-alert
        type="error"
        :closable="false"
        class="blocked-alert"
        title="保存被拒：以下题目仍在使用将被删除的标签，请先在题目上解绑（编辑题目 → 标签）"
      />
      <el-table stripe :data="blockedProblems" size="small">
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

/* 底部两行缓冲垫片：末级展开后鼠标习惯性继续下移查找，若移出容器会触发
   mouseleave 收起——垫片在容器内且 mouseenter 取消收起计时，移动不断开 */
.tag-board-pad {
  height: 80px;
}

.tag-row {
  display: flex;
  flex-wrap: nowrap;
  gap: 8px;
  align-items: flex-start;
}

/* 单一横向滚动容器：不出滚动条——鼠标滚轮横移浏览当前层级（onWheel，与 TagChips 同款） */
.tag-scroll {
  overflow-x: auto;
  padding-top: 8px;
  scrollbar-width: none; /* Firefox */
}

.tag-scroll::-webkit-scrollbar {
  display: none;
}

/* 画布随最宽行伸展，垫片覆盖全部可滚区域 */
.tag-canvas {
  display: flex;
  flex-direction: column;
  gap: 14px; /* 层级行距（一级/二级……之间） */
  width: max-content;
  min-width: 100%;
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
  flex: none; /* 行内不换行模式：卡片不压缩，超出即出横向滚动条 */
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
  transform: scale(1.05) rotate(1.5deg);
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
  font-size: 12px;
  text-align: center;
}

/* 被引用角标跟随标签自身层级色（--lv 由卡片上的全局 .lv-N 注入），不再一律蓝 */
.count-badge.hot {
  background: var(--lv);
}

/* 卡片激活（层级色实底）时徽标反转为浅底深字，保持可读 */
.tag-card.active .count-badge.hot {
  background: var(--lv-bg);
  color: var(--lv);
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
  font-size: 12px;
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

.blocked-alert {
  margin-top: 12px;
}

.tag-gap {
  margin: 0 6px 4px 0;
}
</style>


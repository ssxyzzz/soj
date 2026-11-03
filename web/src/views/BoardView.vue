<script setup lang="ts">
// 手绘画板（v0.60）：自研轻量画板取代 drawio 画板（4MB 首载超带宽预算，全链下线）。
// SVG 原生图元朴素线条（2026-11-03 拍板弃 roughjs 手绘风）+ perfect-freehand(~2KB gzip)
// 自然画笔，viewBox 无限画布；懒加载独立 chunk（gzip 红线 ≤30KB）。画稿只存 localStorage
// （后端零存储、讲完即删，沿 v0.56 拍板边界）。生成器 draw.io 式可视化交互：表格拖行列、
// 树/图点选建边。引擎在 components/board/engine.ts（纯 TS 无 UI），本文件只做交互状态机与工具栏。
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import type { Component } from 'vue'

import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Circle,
  Eraser,
  Fullscreen,
  Hand,
  Maximize,
  Minimize2,
  Minus,
  MousePointer2,
  MoveUpRight,
  Pencil,
  Redo2,
  Share2,
  Sparkles,
  Square,
  Table as TableIcon,
  Trash2,
  Type,
  Undo2,
  ZoomIn,
  ZoomOut,
} from 'lucide-vue-next'

import {
  CELL_H,
  CELL_MAX,
  CELL_W,
  BOARD_FONT,
  NODE_R,
  TEXT_SIZE,
  TREE_GAP_X,
  TREE_GAP_Y,
  BoardRenderer,
  BoardModel,
  bboxesIntersect,
  bboxOf,
  buildExportSvg,
  renderShapeInto,
  resizeShape,
  textBaselineInBox,
  textWidth,
} from '@/components/board/engine'
import type {
  BBox,
  ColorSlot,
  EdgeShape,
  NodeShape,
  Palette,
  Shape,
  ShapeStyle,
  TableShape,
  TextShape,
  Tool,
} from '@/components/board/engine'

// 多画布本地存档（不上服务器）：v3 = 索引 {v,active,count} + 每画布一份 DocV1 快照；
// v2 单画布旧档首次加载迁移为画布 1 后移除（不留老账）
const CANVAS_MAX = 5
const SAVE_INDEX_KEY = 'soj.board.v3'
const LEGACY_SAVE_KEY = 'soj.board.v2'
const canvasKey = (n: number) => `soj.board.v3.c${n}`
const canvasIndex = ref(1)
const canvasCount = ref(1)

// ---------- 调色板：挂载时从 CSS 变量读入，主题切换（v5/navy）自动跟随 ----------

function readPalette(): Palette {
  const cs = getComputedStyle(document.documentElement)
  const v = (n: string) => cs.getPropertyValue(n).trim()
  return {
    ink: v('--soj-ink'),
    blue: v('--soj-blue'),
    teal: v('--soj-teal'),
    warning: v('--soj-warning'),
    danger: v('--soj-danger'),
    paper: v('--soj-white'),
  }
}

const palette = ref<Palette>(readPalette())

// ---------- 状态 ----------

const stageEl = ref<HTMLDivElement | null>(null)
const svgEl = ref<SVGSVGElement | null>(null)
const editorEl = ref<HTMLTextAreaElement | null>(null)

const model = new BoardModel()
let renderer: BoardRenderer | null = null

const tool = ref<Tool>('select')
const color = ref<ColorSlot>('ink')
const weight = ref<1 | 2 | 3>(2)
const treeMode = ref(true)
const directed = ref(false)
const selected = ref(new Set<number>())
const canUndo = ref(false)
const canRedo = ref(false)
const spacePressed = ref(false)
const isFullscreen = ref(false)

const cam = reactive({ x: 0, y: 0, scale: 1 })
const stage = reactive({ w: 800, h: 500 })
const viewBox = computed(
  () => `${cam.x.toFixed(2)} ${cam.y.toFixed(2)} ${(stage.w / cam.scale).toFixed(2)} ${(stage.h / cam.scale).toFixed(2)}`,
)
const zoomPct = computed(() => Math.round(cam.scale * 100))
const cursorClass = computed(() => {
  if (spacePressed.value || tool.value === 'hand') return drag?.mode === 'pan' ? 'cur-grabbing' : 'cur-grab'
  return tool.value === 'select' ? 'cur-move' : 'cur-draw'
})

function curStyle(): ShapeStyle {
  return { color: color.value, weight: weight.value }
}

function syncHistory(): void {
  canUndo.value = model.canUndo
  canRedo.value = model.canRedo
}

// ---------- 工具栏数据 ----------

const tools: { id: Tool; icon: Component; label: string }[] = [
  { id: 'hand', icon: Hand, label: '拖动画布（空格 / 中键拖拽同效）' },
  { id: 'select', icon: MousePointer2, label: '选择 / 移动' },
  { id: 'pen', icon: Pencil, label: '画笔' },
  { id: 'line', icon: Minus, label: '直线' },
  { id: 'arrow', icon: MoveUpRight, label: '箭头' },
  { id: 'rect', icon: Square, label: '矩形' },
  { id: 'ellipse', icon: Circle, label: '椭圆' },
  { id: 'text', icon: Type, label: '文字' },
  { id: 'table', icon: TableIcon, label: '表格' },
  { id: 'graph', icon: Share2, label: '树 / 图' },
  { id: 'eraser', icon: Eraser, label: '橡皮' }
]

const colorSlots: { slot: ColorSlot; css: string; name: string }[] = [
  { slot: 'ink', css: 'var(--soj-ink)', name: '墨黑' },
  { slot: 'blue', css: 'var(--soj-blue)', name: '主蓝' },
  { slot: 'teal', css: 'var(--soj-teal)', name: '青绿' },
  { slot: 'warning', css: 'var(--soj-warning)', name: '橙' },
  { slot: 'danger', css: 'var(--soj-danger)', name: '红' }
]
const weights: Array<1 | 2 | 3> = [1, 2, 3]

// ---------- 渲染调度：rAF 合帧更新预览层 / 选区层；主层由各操作直连 renderer ----------

let drag: DragState | null = null
let rafId = 0
let previewFlag = false

interface DragState {
  mode: 'draw' | 'move' | 'marquee' | 'pan' | 'erase' | 'link' | 'click' | 'resize'
  sx: number
  sy: number
  lx: number
  ly: number
  lpx: number
  lpy: number
  draft?: Shape
  linkFrom?: NodeShape
  linkTarget?: NodeShape
  movedIds?: Set<number>
  ax?: number
  ay?: number
  erased?: boolean
  resizeId?: number
  resizeFrom?: BBox
  // 笔迹缩放专用：拖拽开始时的原始点集快照（防逐事件复合放大）
  origPoints?: [number, number][]
  // 移动/缩放首帧才入撤销栈：纯点击不成空快照，且快照必在落点变更之前
  undoPushed?: boolean
}

function flush(): void {
  rafId = 0
  if (!renderer) return
  if (!previewFlag) return
  previewFlag = false
  const d = drag
  if (d?.mode === 'draw' && d.draft) {
    const shape = normalizeDraft(d.draft)
    renderer.setPreview((g, pal) => renderShapeInto(g, shape, pal, model.nodeById()))
  } else if (d?.mode === 'link' && d.linkFrom) {
    const from = d.linkFrom
    const target = d.linkTarget
    const tmp: NodeShape = { id: -2, type: 'node', x: d.lx, y: d.ly, r: NODE_R, label: '', style: from.style }
    renderer.setPreview((g, pal) => {
      const byId = new Map(model.nodeById())
      byId.set(-2, target ?? tmp)
      const e: EdgeShape = { id: -1, type: 'edge', from: from.id, to: -2, label: '', directed: directed.value, style: from.style }
      renderShapeInto(g, e, pal, byId)
    })
  } else {
    renderer.setPreview(null)
  }
  refreshOverlay()
}

function schedulePreview(): void {
  previewFlag = true
  if (!rafId) rafId = requestAnimationFrame(flush)
}

function curMarquee() {
  if (drag?.mode !== 'marquee') return null
  return normRect(drag.sx, drag.sy, drag.lx, drag.ly)
}

// 单选表格的元信息（浮动条与缩放手柄共用）
function singleTable(): TableShape | null {
  if (selected.value.size !== 1) return null
  const s = model.get([...selected.value][0])
  return s && s.type === 'table' ? s : null
}

// 缩放手柄锚点：单选图形（连线除外）的包围盒右下角
function resizeHandle(): { id: number; x: number; y: number } | null {
  if (selected.value.size !== 1) return null
  const s = model.get([...selected.value][0])
  if (!s || s.type === 'edge') return null
  const bb = bboxOf(s, model.nodeById())
  if (!bb) return null
  return { id: s.id, x: bb.x + bb.w, y: bb.y + bb.h }
}

// —— 表格浮动条（单选表格时浮现在左上角）：±行/±列，讲 DP 表演算到哪加到哪 ——
const tableBar = computed(() => singleTable())
const tableBarStyle = computed<Record<string, string>>((): Record<string, string> => {
  const t = tableBar.value
  if (!t) return { display: 'none' }
  return {
    left: `${((t.x - cam.x) * cam.scale).toFixed(1)}px`,
    top: `${((t.y - cam.y) * cam.scale - 34).toFixed(1)}px`,
  }
})

function tableResize(kind: 'row' | 'col', delta: 1 | -1): void {
  const t = tableBar.value
  if (!t) return
  const next = kind === 'row'
    ? Math.max(1, Math.min(CELL_MAX, t.rows + delta))
    : Math.max(1, Math.min(CELL_MAX, t.cols + delta))
  if (next === (kind === 'row' ? t.rows : t.cols)) return
  model.pushUndo()
  const cells: string[] = []
  const rows = kind === 'row' ? next : t.rows
  const cols = kind === 'col' ? next : t.cols
  // 行主序按新尺寸重建（旧内容按原位保留，新增格填空串）
  for (let r = 0; r < rows; r++)
    for (let c = 0; c < cols; c++) cells.push(t.cells[r * t.cols + c] ?? '')
  t.rows = rows
  t.cols = cols
  t.cells = cells
  renderer?.renderIds(model, [t.id])
  refreshOverlay()
  syncHistory()
  autosave()
}

function normRect(x1: number, y1: number, x2: number, y2: number) {
  return { x: Math.min(x1, x2), y: Math.min(y1, y2), w: Math.abs(x2 - x1), h: Math.abs(y2 - y1) }
}

function normalizeDraft(d: Shape): Shape {
  if (d.type === 'rect' || d.type === 'ellipse') {
    return { ...d, x: Math.min(d.x, d.x + d.w), y: Math.min(d.y, d.y + d.h), w: Math.abs(d.w), h: Math.abs(d.h) }
  }
  return d
}

function refreshOverlay(): void {
  renderer?.refreshOverlay(model, selected.value, curMarquee(), cam.scale, resizeHandle())
}

// ---------- 指针交互状态机 ----------

function toWorld(e: { clientX: number; clientY: number }) {
  const rect = svgEl.value?.getBoundingClientRect()
  return {
    x: cam.x + (e.clientX - (rect?.left ?? 0)) / cam.scale,
    y: cam.y + (e.clientY - (rect?.top ?? 0)) / cam.scale,
  }
}

// 指针捕获（拖拽出 svg 也能持续收事件）。ego 等带代理层的浏览器可能拒绝捕获并抛错——
// 捕获失败只降级为「光标不出画布即可正常绘制」，绝不让它中断绘制主流程
function capturePointer(e: PointerEvent): void {
  try {
    svgEl.value?.setPointerCapture(e.pointerId)
  } catch {
    /* 捕获被拒绝：忽略 */
  }
}

function onPointerDown(e: PointerEvent): void {
  if (editing.value) {
    commitEdit()
    return
  }
  if (!renderer) return
  // 左键/中键手势一律拦默认行为：浏览器原生文字选择一旦随拖拽启动，会触发
  // pointercancel 中断绘制（ego 等指针捕获被拒的浏览器实测「画不出」的根因；
  // Edge 捕获成功压制了选择所以能画）。选中文本的体感 bug 同源。
  if (e.button === 0 || e.button === 1 || spacePressed.value) e.preventDefault()
  const w = toWorld(e)
  const st: DragState = { mode: 'click', sx: w.x, sy: w.y, lx: w.x, ly: w.y, lpx: e.clientX, lpy: e.clientY }
  if (tool.value === 'hand' || spacePressed.value || e.button === 1) {
    // 抓手工具 / 空格 / 中键平移视口（中键需拦掉浏览器默认的自动滚动）
    e.preventDefault()
    st.mode = 'pan'
    drag = st
    capturePointer(e)
    return
  }
  if (e.button !== 0) return
  capturePointer(e)
  const tol = 8 / cam.scale
  const style = curStyle()
  switch (tool.value) {
    case 'select': {
      // 单选图形右下角缩放手柄优先于图形命中
      const h = resizeHandle()
      if (h && Math.abs(w.x - h.x) <= 10 / cam.scale && Math.abs(w.y - h.y) <= 10 / cam.scale) {
        const hs = model.get(h.id)
        const bb = hs ? bboxOf(hs, model.nodeById()) : null
        st.mode = 'resize'
        st.resizeId = h.id
        if (bb) st.resizeFrom = bb
        if (hs && hs.type === 'stroke') st.origPoints = hs.points.map((p) => [p[0], p[1]] as [number, number])
        break
      }
      const hit = model.hitTest(w.x, w.y, tol)
      if (hit) {
        if (e.shiftKey) {
          const next = new Set(selected.value)
          if (next.has(hit.id)) {
            next.delete(hit.id)
            selected.value = next
            refreshOverlay()
            return
          }
          next.add(hit.id)
          selected.value = next
        } else if (!selected.value.has(hit.id)) {
          selected.value = new Set([hit.id])
        }
        refreshOverlay()
        // 拖动集合 = 选中集 + 相连边（节点拖动连线跟随）
        const ids = new Set(selected.value)
        for (const eid of model.edgesOf(ids)) ids.add(eid)
        st.mode = 'move'
        st.movedIds = ids
        st.ax = 0
        st.ay = 0
      } else {
        selected.value = new Set()
        st.mode = 'marquee'
      }
      refreshOverlay()
      break
    }
    case 'pen':
      st.mode = 'draw'
      st.draft = { id: 0, type: 'stroke', points: [[w.x, w.y]], style }
      break
    case 'line':
    case 'arrow':
      st.mode = 'draw'
      st.draft = { id: 0, type: tool.value, x1: w.x, y1: w.y, x2: w.x, y2: w.y, style }
      break
    case 'rect':
    case 'ellipse':
      st.mode = 'draw'
      st.draft = { id: 0, type: tool.value, x: w.x, y: w.y, w: 0, h: 0, style }
      break
    case 'table':
      st.mode = 'draw'
      st.draft = { id: 0, type: 'table', x: w.x, y: w.y, rows: 1, cols: 1, cw: CELL_W, ch: CELL_H, cells: [''], style }
      break
    case 'text':
      st.mode = 'click'
      break
    case 'graph': {
      const hit = model.hitTest(w.x, w.y, tol)
      if (hit?.type === 'node') {
        st.mode = 'link'
        st.linkFrom = hit
      } else {
        createNodeAt(w.x, w.y)
      }
      break
    }
    case 'eraser':
      st.mode = 'erase'
      st.erased = false
      drag = st
      eraseAt(w)
      return
  }
  drag = st
}

// 手势世界坐标钳制在当前视口内：画/拖/缩放永远不出可见区——「拖出视野消失」与
// 「边缘缩放爆炸」（自动平移期相机位移被算进 to.w，笔迹系数可爆到 10 倍+）从根上杜绝；
// 曾试边缘自动平移（Excalidraw 式）因该爆炸问题撤回，视口内钳制更简单可预期
function clampToViewport(w: { x: number; y: number }) {
  const right = cam.x + stage.w / cam.scale
  const bottom = cam.y + stage.h / cam.scale
  w.x = Math.max(cam.x, Math.min(right, w.x))
  w.y = Math.max(cam.y, Math.min(bottom, w.y))
  return w
}

function onPointerMove(e: PointerEvent): void {
  if (!drag) return
  const w = clampToViewport(toWorld(e))
  const d = drag
  switch (d.mode) {
    case 'pan':
      cam.x -= (e.clientX - d.lpx) / cam.scale
      cam.y -= (e.clientY - d.lpy) / cam.scale
      d.lpx = e.clientX
      d.lpy = e.clientY
      return
    case 'draw': {
      const draft = d.draft
      if (!draft) return
      if (draft.type === 'stroke') draft.points.push([w.x, w.y])
      else if (draft.type === 'line' || draft.type === 'arrow') {
        draft.x2 = w.x
        draft.y2 = w.y
      } else if (draft.type === 'rect' || draft.type === 'ellipse') {
        draft.w = w.x - draft.x
        draft.h = w.y - draft.y
      } else if (draft.type === 'table') {
        const cols = Math.max(1, Math.min(CELL_MAX, Math.round((w.x - draft.x) / CELL_W)))
        const rows = Math.max(1, Math.min(CELL_MAX, Math.round((w.y - draft.y) / CELL_H)))
        if (cols !== draft.cols || rows !== draft.rows) {
          draft.cols = cols
          draft.rows = rows
          draft.cells = new Array(cols * rows).fill('')
        }
      }
      schedulePreview()
      return
    }
    case 'move': {
      // 拖动期零重渲染：只平移已渲染的 <g>（含相连边），落点后再按真实坐标重建
      if (!d.undoPushed && (w.x !== d.lx || w.y !== d.ly)) {
        model.pushUndo()
        d.undoPushed = true
        syncHistory()
      }
      d.ax = (d.ax ?? 0) + (w.x - d.lx)
      d.ay = (d.ay ?? 0) + (w.y - d.ly)
      renderer?.transformGroups(d.movedIds ?? [], d.ax, d.ay)
      d.lx = w.x
      d.ly = w.y
      return
    }
    case 'marquee':
      d.lx = w.x
      d.ly = w.y
      schedulePreview()
      return
    case 'erase':
      eraseAt(w)
      return
    case 'resize': {
      const s = model.get(d.resizeId ?? 0)
      const from = d.resizeFrom
      if (!s || !from) return
      const to: BBox = { x: from.x, y: from.y, w: Math.max(8, w.x - from.x), h: Math.max(8, w.y - from.y) }
      if (!d.undoPushed) {
        model.pushUndo()
        d.undoPushed = true
        syncHistory()
      }
      resizeShape(s, from, to, d.origPoints)
      renderer?.renderIds(model, [s.id])
      refreshOverlay()
      return
    }
    case 'link': {
      const hit = model.hitTest(w.x, w.y, 8 / cam.scale)
      d.linkTarget = hit && hit.type === 'node' && hit.id !== d.linkFrom?.id ? hit : undefined
      d.lx = w.x
      d.ly = w.y
      schedulePreview()
      return
    }
    case 'click':
      d.lx = w.x
      d.ly = w.y
      return
  }
}

function onPointerUp(e: PointerEvent): void {
  const d = drag
  if (!d) return
  drag = null
  const w = clampToViewport(toWorld(e))
  switch (d.mode) {
    case 'pan':
      return
    case 'draw':
      commitDraft(d, e)
      renderer?.setPreview(null)
      return
    case 'click':
      if (Math.hypot(e.clientX - d.lpx, e.clientY - d.lpy) < 5 && tool.value === 'text') openTextCreate(w)
      return
    case 'move': {
      const ax = d.ax ?? 0
      const ay = d.ay ?? 0
      if (ax || ay) {
        model.translate(selected.value, ax, ay)
        renderer?.clearTransforms(d.movedIds ?? [])
        renderer?.renderIds(model, d.movedIds ?? [])
        refreshOverlay()
        autosave()
      } else {
        renderer?.clearTransforms(d.movedIds ?? [])
      }
      return
    }
    case 'marquee': {
      const rect = normRect(d.sx, d.sy, w.x, w.y)
      const byId = model.nodeById()
      const next = new Set<number>()
      for (const s of model.shapes) {
        const bb = bboxOf(s, byId)
        if (bb && bboxesIntersect(rect, bb)) next.add(s.id)
      }
      selected.value = next
      renderer?.setPreview(null)
      refreshOverlay()
      return
    }
    case 'erase':
      if (d.erased) autosave()
      return
    case 'resize':
      if (d.undoPushed) autosave()
      return
    case 'link':
      commitLink(d, w)
      renderer?.setPreview(null)
      return
  }
}

function commitDraft(d: DragState, e: PointerEvent): void {
  const draft = d.draft
  if (!draft) return
  const s = normalizeDraft(draft)
  if (s.type === 'line' || s.type === 'arrow') {
    if (Math.hypot(s.x2 - s.x1, s.y2 - s.y1) < 3) return
  }
  if (s.type === 'rect' || s.type === 'ellipse') {
    if (s.w < 3 || s.h < 3) return
  }
  if (s.type === 'table' && s.rows === 1 && s.cols === 1 && Math.hypot(e.clientX - d.lpx, e.clientY - d.lpy) < CELL_W / 2) {
    return // 单格且几乎没拖：视为误触
  }
  model.pushUndo()
  model.add(s)
  renderer?.upsert(model, s.id)
  syncHistory()
  autosave()
}

function createNodeAt(x: number, y: number): void {
  model.pushUndo()
  const s: NodeShape = { id: 0, type: 'node', x, y, r: NODE_R, label: model.nextLabel(), style: curStyle() }
  model.add(s)
  renderer?.upsert(model, s.id)
  syncHistory()
  autosave()
}

function commitLink(d: DragState, w: { x: number; y: number }): void {
  const from = d.linkFrom
  if (!from) return
  if (d.linkTarget && d.linkTarget.id !== from.id) {
    // 节点 → 节点：连边
    model.pushUndo()
    const e: EdgeShape = { id: 0, type: 'edge', from: from.id, to: d.linkTarget.id, label: '', directed: directed.value, style: curStyle() }
    model.add(e)
    renderer?.upsert(model, e.id)
    syncHistory()
    autosave()
    return
  }
  // 节点 → 空白：建子节点并连线（树式布子＝落在父节点下方，自由＝落在光标处）
  if (Math.hypot(w.x - from.x, w.y - from.y) > NODE_R + 6) {
    const pos = treeMode.value
      ? { x: from.x + childCount(from.id) * TREE_GAP_X, y: from.y + TREE_GAP_Y }
      : { x: w.x, y: w.y }
    model.pushUndo()
    const n: NodeShape = { id: 0, type: 'node', x: pos.x, y: pos.y, r: NODE_R, label: model.nextLabel(), style: curStyle() }
    model.add(n)
    const e: EdgeShape = { id: 0, type: 'edge', from: from.id, to: n.id, label: '', directed: directed.value, style: curStyle() }
    model.add(e)
    renderer?.upsert(model, n.id)
    renderer?.upsert(model, e.id)
    syncHistory()
    autosave()
  }
}

function childCount(id: number): number {
  let c = 0
  for (const s of model.shapes) if (s.type === 'edge' && s.from === id) c++
  return c
}

function eraseAt(w: { x: number; y: number }): void {
  const d = drag
  if (!d || d.mode !== 'erase') return
  const hit = model.hitTest(w.x, w.y, 8 / cam.scale)
  if (!hit) return
  if (!d.erased) {
    model.pushUndo()
    d.erased = true
    syncHistory()
  }
  deleteCascade(new Set([hit.id]))
  if (selected.value.has(hit.id)) {
    const next = new Set(selected.value)
    next.delete(hit.id)
    selected.value = next
  }
  refreshOverlay()
}

// ---------- 文字 / 节点 / 单元格 / 边权重：浮层编辑 ----------

interface EditState {
  kind: 'text' | 'node' | 'cell' | 'edge'
  shapeId: number // 0 = 新建文字
  cellIndex: number
  wx: number
  wy: number
  size: number
  style: ShapeStyle
}

const editing = ref<EditState | null>(null)
const editorText = ref('')

function openEditor(ed: EditState, value: string): void {
  editing.value = ed
  editorText.value = value
  void nextTick(() => {
    editorEl.value?.focus()
    editorEl.value?.select()
  })
}

function openTextCreate(w: { x: number; y: number }): void {
  openEditor({ kind: 'text', shapeId: 0, cellIndex: -1, wx: w.x, wy: w.y, size: TEXT_SIZE, style: curStyle() }, '')
}

function openTextEdit(s: TextShape): void {
  openEditor({ kind: 'text', shapeId: s.id, cellIndex: -1, wx: s.x, wy: s.y, size: s.size, style: s.style }, s.content)
}

function openNodeEdit(s: NodeShape): void {
  openEditor({ kind: 'node', shapeId: s.id, cellIndex: -1, wx: s.x, wy: s.y, size: 14, style: s.style }, s.label)
}

function openCellEdit(t: TableShape, w: { x: number; y: number }): void {
  const col = Math.floor((w.x - t.x) / t.cw)
  const row = Math.floor((w.y - t.y) / t.ch)
  if (col < 0 || col >= t.cols || row < 0 || row >= t.rows) return
  openCellIndex(t, row * t.cols + col)
}

function openCellIndex(t: TableShape, idx: number): void {
  openEditor(
    { kind: 'cell', shapeId: t.id, cellIndex: idx, wx: t.x + (idx % t.cols) * t.cw, wy: t.y + Math.floor(idx / t.cols) * t.ch, size: 14, style: t.style },
    t.cells[idx] ?? '',
  )
}

function openEdgeEdit(s: EdgeShape): void {
  const byId = model.nodeById()
  const a = byId.get(s.from)
  const b = byId.get(s.to)
  if (!a || !b) return
  const dx = b.x - a.x
  const dy = b.y - a.y
  const dd = Math.hypot(dx, dy) || 1
  const ux = dx / dd
  const uy = dy / dd
  openEditor(
    { kind: 'edge', shapeId: s.id, cellIndex: -1, wx: (a.x + b.x) / 2 + uy * 12, wy: (a.y + b.y) / 2 - ux * 12, size: 12, style: s.style },
    s.label,
  )
}

// 浮层位置随相机（缩放/平移中编辑也粘在锚点上）
const editorStyle = computed<Record<string, string>>(() => {
  const ed = editing.value
  if (!ed) return {} as Record<string, string>
  const cs = cam.scale
  const sx = (wx: number) => (wx - cam.x) * cs
  const sy = (wy: number) => (wy - cam.y) * cs
  const base = {
    fontFamily: BOARD_FONT,
    fontSize: `${Math.max(12, ed.size * cs).toFixed(1)}px`,
    color: palette.value[ed.style.color],
    textAlign: 'left',
  }
  switch (ed.kind) {
    case 'text': {
      // 编辑框即真实包围盒（所改所得）：SVG 渲染是单行不换行，编辑框同为单行（wrap=off），
      // 宽度实时随内容测量（与 textWidth/bboxOf 同源），字号原样（去掉 12px 最小钳制），
      // 行盒=包围盒高且基线经 textBaselineInBox 对齐——编辑所见与提交渲染一致
      const fs = ed.size * cs
      const w = Math.max(textWidth(editorText.value, ed.size) * cs, ed.size * cs) + 4
      return {
        fontFamily: BOARD_FONT,
        fontSize: `${fs.toFixed(1)}px`,
        color: palette.value[ed.style.color],
        textAlign: 'left',
        left: `${(sx(ed.wx) - 1).toFixed(1)}px`,
        top: `${(sy(ed.wy) - textBaselineInBox(ed.size, fs) - 1).toFixed(1)}px`,
        width: `${w.toFixed(1)}px`,
        height: `${fs.toFixed(1)}px`,
        lineHeight: `${fs.toFixed(1)}px`,
        padding: '0',
        whiteSpace: 'pre',
      }
    }
    case 'node':
      return { ...base, textAlign: 'center', left: `${sx(ed.wx) - 70}px`, top: `${sy(ed.wy) - ed.size * cs * 0.9}px`, width: '140px' }
    case 'cell': {
      // 编辑框跟随该表格实际格子尺寸（缩放后的表格取实时 cw/ch）
      const t = model.get(ed.shapeId)
      const cw = t && t.type === 'table' ? t.cw : CELL_W
      const ch = t && t.type === 'table' ? t.ch : CELL_H
      return { ...base, textAlign: 'center', left: `${sx(ed.wx) + 3}px`, top: `${sy(ed.wy) + 3}px`, width: `${cw * cs - 8}px`, height: `${ch * cs - 8}px` }
    }
    case 'edge':
      return { ...base, textAlign: 'center', left: `${sx(ed.wx) - 50}px`, top: `${sy(ed.wy) - ed.size * cs}px`, width: '100px' }
    default:
      return {} as Record<string, string>
  }
})

function commitEdit(): void {
  const ed = editing.value
  if (!ed) return
  editing.value = null
  const val = editorText.value
  switch (ed.kind) {
    case 'text': {
      const s = ed.shapeId ? model.get(ed.shapeId) : undefined
      if (s && s.type === 'text') {
        if (!val.trim()) {
          // 清空即删（Excalidraw 同语义）
          model.pushUndo()
          model.delete(new Set([s.id]))
          renderer?.removeIds([s.id])
        } else if (val !== s.content) {
          model.pushUndo()
          s.content = val
          renderer?.renderIds(model, [s.id])
        }
      } else if (!ed.shapeId && val.trim()) {
        model.pushUndo()
        const t: TextShape = { id: 0, type: 'text', x: ed.wx, y: ed.wy, content: val, size: ed.size, style: ed.style }
        model.add(t)
        renderer?.upsert(model, t.id)
      }
      break
    }
    case 'node': {
      const s = model.get(ed.shapeId)
      const v = val.trim()
      if (s?.type === 'node' && v && v !== s.label) {
        model.pushUndo()
        s.label = v
        renderer?.renderIds(model, [s.id])
      }
      break
    }
    case 'cell': {
      const s = model.get(ed.shapeId)
      if (s?.type === 'table' && ed.cellIndex < s.cells.length && val !== s.cells[ed.cellIndex]) {
        model.pushUndo()
        s.cells[ed.cellIndex] = val
        renderer?.renderIds(model, [s.id])
      }
      break
    }
    case 'edge': {
      const s = model.get(ed.shapeId)
      const v = val.trim()
      if (s?.type === 'edge' && v !== s.label) {
        model.pushUndo()
        s.label = v
        renderer?.renderIds(model, [s.id])
      }
      break
    }
  }
  syncHistory()
  autosave()
  refreshOverlay()
}

function cancelEdit(): void {
  editing.value = null
  editorText.value = ''
}

function autosizeEditor(e: Event): void {
  if (editing.value?.kind === 'cell' || editing.value?.kind === 'text') return
  // 单元格定高；文字盒随内容实时测量（editorStyle），均不走高度自适应
  const t = e.target as HTMLTextAreaElement
  t.style.height = 'auto'
  t.style.height = `${t.scrollHeight}px`
}

function onDblClick(e: MouseEvent): void {
  if (editing.value) return
  const w = toWorld(e)
  const hit = model.hitTest(w.x, w.y, 8 / cam.scale)
  if (!hit) {
    if (tool.value === 'select') openTextCreate(w)
    return
  }
  if (hit.type === 'text') openTextEdit(hit)
  else if (hit.type === 'node') openNodeEdit(hit)
  else if (hit.type === 'table') openCellEdit(hit, w)
  else if (hit.type === 'edge') openEdgeEdit(hit)
}

// ---------- 历史与删改 ----------

function afterHistorySwap(): void {
  selected.value = new Set([...selected.value].filter((id) => model.get(id)))
  renderer?.renderAll(model)
  refreshOverlay()
  syncHistory()
  autosave()
}

function doUndo(): void {
  if (drag || !model.undo()) return
  afterHistorySwap()
}

function doRedo(): void {
  if (drag || !model.redo()) return
  afterHistorySwap()
}

function deleteSelected(): void {
  if (!selected.value.size) return
  model.pushUndo()
  deleteCascade(selected.value)
  selected.value = new Set()
  refreshOverlay()
  syncHistory()
  autosave()
}

// 级联删除：删节点时连同引用它的边一起删（否则增量渲染下连线 <g> 残留成僵尸线）；
// 同一撤销快照内，撤销一并恢复
function deleteCascade(ids: Set<number>): void {
  const all = new Set(ids)
  for (const eid of model.edgesOf(ids)) all.add(eid)
  model.delete(all)
  renderer?.removeIds(all)
}

async function clearAll(): Promise<void> {
  try {
    await ElMessageBox.confirm('清空整个画布？清空后可撤销恢复。', '清空画布', {
      type: 'warning',
      confirmButtonText: '清空',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  model.pushUndo()
  model.shapes = []
  selected.value = new Set()
  renderer?.renderAll(model)
  refreshOverlay()
  syncHistory()
  autosave()
}

function tidy(): void {
  model.pushUndo()
  if (!model.tidyTree()) {
    model.popUndo()
    ElMessage.info('画布上还没有可整理的树 / 图节点')
    return
  }
  renderer?.renderAll(model)
  refreshOverlay()
  syncHistory()
  autosave()
  ElMessage.success('已整理树布局')
}

// ---------- 视口：滚轮缩放（光标为不动点）、空格/中键平移、适应内容 ----------

function zoomAt(factor: number, px = stage.w / 2, py = stage.h / 2): void {
  const ns = Math.min(4, Math.max(0.25, cam.scale * factor))
  if (ns === cam.scale) return
  const wx = cam.x + px / cam.scale
  const wy = cam.y + py / cam.scale
  cam.scale = ns
  cam.x = wx - px / ns
  cam.y = wy - py / ns
}

function onWheel(e: WheelEvent): void {
  const rect = svgEl.value?.getBoundingClientRect()
  zoomAt(e.deltaY < 0 ? 1.1 : 1 / 1.1, e.clientX - (rect?.left ?? 0), e.clientY - (rect?.top ?? 0))
}

function resetZoom(): void {
  zoomAt(1 / cam.scale)
}

// 画布铺满模式（浏览器内，非 Fullscreen API）：根节点转 fixed 盖住导航以下整页、
// 隐藏页头，卡片吃满剩余高度；顶部站点菜单（sticky z-50）保持可见可点。
// Esc 可退出（onKeydown 分支）；舞台尺寸变化由 ResizeObserver 感知自动重算视窗
function toggleFullscreen(): void {
  isFullscreen.value = !isFullscreen.value
}

function fitView(): void {
  const bb = model.contentBBox()
  if (!bb) {
    centerOrigin()
    return
  }
  const scale = Math.min(2, Math.max(0.25, Math.min(stage.w / bb.w, stage.h / bb.h)))
  cam.scale = scale
  cam.x = bb.x + bb.w / 2 - stage.w / (2 * scale)
  cam.y = bb.y + bb.h / 2 - stage.h / (2 * scale)
}

function centerOrigin(): void {
  cam.scale = 1
  cam.x = -stage.w / 2
  cam.y = -stage.h / 2
}

// ---------- 导出（后端零存储：出图即带走）与本地暂存 ----------

function download(blob: Blob, name: string): void {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = name
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 1000)
}

function doExport(kind: 'png' | 'svg'): void {
  const svg = buildExportSvg(model, palette.value)
  if (!svg) {
    ElMessage.warning('画布还是空的，先画点什么吧')
    return
  }
  const str = new XMLSerializer().serializeToString(svg)
  const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
  const name = `soj-board-${stamp}`
  if (kind === 'svg') {
    download(new Blob([str], { type: 'image/svg+xml;charset=utf-8' }), `${name}.svg`)
    return
  }
  const img = new Image()
  img.onload = () => {
    const cv = document.createElement('canvas')
    cv.width = img.naturalWidth * 2
    cv.height = img.naturalHeight * 2
    const ctx = cv.getContext('2d')
    if (!ctx) return
    ctx.drawImage(img, 0, 0, cv.width, cv.height)
    cv.toBlob((b) => {
      if (b) download(b, `${name}.png`)
    }, 'image/png')
  }
  img.onerror = () => ElMessage.error('导出失败，请重试')
  img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(str)}`
}

let saveTimer: ReturnType<typeof setTimeout> | undefined
function autosave(): void {
  clearTimeout(saveTimer)
  // 防线二：落盘前重定基——内容量级超限即整体平移回原点附近，相机同步跟随（画面零跳变）
  // 并整层重渲染；配合防线一保证存档与渲染永远处于 float32 光栅化安全量级，杜绝复发
  const rb = model.rebaseIfNeeded()
  if (rb) {
    cam.x += rb.dx
    cam.y += rb.dy
    renderer?.renderAll(model)
    refreshOverlay()
  }
  saveTimer = setTimeout(() => {
    try {
      localStorage.setItem(canvasKey(canvasIndex.value), model.snapshot())
    } catch {
      // 存储满 / 隐私模式：忽略，画稿仍在内存与撤销栈里
    }
  }, 600)
}

function persistIndex(): void {
  try {
    localStorage.setItem(
      SAVE_INDEX_KEY,
      JSON.stringify({ v: 1, active: canvasIndex.value, count: canvasCount.value }),
    )
  } catch {
    // 存储满 / 隐私模式：容忍，画布数本次会话仍可用
  }
}

// 立即落盘当前画布（切换/治愈时用）：绕过 600ms 防抖，防止换档后防抖存档写串画布
function persistNow(): void {
  clearTimeout(saveTimer)
  try {
    localStorage.setItem(canvasKey(canvasIndex.value), model.snapshot())
  } catch {
    // 存储满 / 隐私模式：忽略，画稿仍在内存与撤销栈里
  }
}

// 装载画布 n：换档即重建编辑态（选择/撤销重做栈随档清空，快照只存文档本身）；
// 含防线一（加载即重定基），返回是否恢复了非空画稿
function loadCanvas(n: number): boolean {
  const raw = localStorage.getItem(canvasKey(n))
  const restored = raw ? model.load(raw) && model.shapes.length > 0 : false
  const rebased = model.rebaseIfNeeded()
  drag = null
  selected.value = new Set()
  if (restored && model.contentBBox()) fitView()
  else centerOrigin()
  renderer?.renderAll(model)
  refreshOverlay()
  syncHistory()
  if (rebased) persistNow() // 治愈后的快照立刻落盘（量级已安全，护栏空转）
  return restored
}

function switchCanvas(n: number): void {
  if (n === canvasIndex.value || drag) return
  persistNow() // 先冲掉旧画布防抖存档再换档
  canvasIndex.value = n
  persistIndex()
  loadCanvas(n)
}

function createCanvas(): void {
  if (canvasCount.value >= CANVAS_MAX) return
  canvasCount.value++
  switchCanvas(canvasCount.value) // 新档无存档 → 空画布居中原点
}

// ---------- 键盘 ----------

function onKeydown(e: KeyboardEvent): void {
  const t = e.target as HTMLElement | null
  if (t && (t.tagName === 'TEXTAREA' || t.tagName === 'INPUT' || t.isContentEditable)) return
  if (e.code === 'Space') {
    spacePressed.value = true
    if (t === document.body) e.preventDefault()
    return
  }
  const k = e.key.toLowerCase()
  if ((e.ctrlKey || e.metaKey) && k === 'z') {
    e.preventDefault()
    if (e.shiftKey) doRedo()
    else doUndo()
    return
  }
  if ((e.ctrlKey || e.metaKey) && k === 'y') {
    e.preventDefault()
    doRedo()
    return
  }
  if (e.key === 'Escape') {
    if (isFullscreen.value) {
      isFullscreen.value = false // Esc 退出铺满模式
      return
    }
    if (drag) {
      drag = null
      renderer?.setPreview(null)
      refreshOverlay()
    } else if (selected.value.size || tool.value !== 'select') {
      selected.value = new Set()
      tool.value = 'select'
      refreshOverlay()
    }
    return
  }
  if ((e.key === 'Delete' || e.key === 'Backspace') && selected.value.size) {
    e.preventDefault()
    deleteSelected()
  }
}

function onKeyup(e: KeyboardEvent): void {
  if (e.code === 'Space') spacePressed.value = false
}

// ---------- 主题跟随与生命周期 ----------

let themeObs: MutationObserver | null = null
let ro: ResizeObserver | null = null

onMounted(() => {
  const svg = svgEl.value
  const stEl = stageEl.value
  if (!svg || !stEl) return
  renderer = new BoardRenderer(svg, palette.value)
  stage.w = stEl.clientWidth
  stage.h = stEl.clientHeight
  ro = new ResizeObserver(() => {
    stage.w = stEl.clientWidth
    stage.h = stEl.clientHeight
  })
  ro.observe(stEl)
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('keyup', onKeyup)
  themeObs = new MutationObserver(() => {
    palette.value = readPalette()
    renderer?.setPalette(palette.value)
    renderer?.renderAll(model)
    refreshOverlay()
  })
  themeObs.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })

  // 多画布索引恢复；无 v3 索引则做 v2 → v3 迁移（旧单画布档整体成为画布 1）
  const idxRaw = localStorage.getItem(SAVE_INDEX_KEY)
  let idx: { v?: number; active?: number; count?: number } | null = null
  try {
    idx = idxRaw ? (JSON.parse(idxRaw) as { v?: number; active?: number; count?: number }) : null
  } catch {
    idx = null // 坏索引当无档，走迁移兜底
  }
  if (idx && typeof idx.active === 'number' && typeof idx.count === 'number') {
    canvasCount.value = Math.min(CANVAS_MAX, Math.max(1, Math.floor(idx.count)))
    canvasIndex.value = Math.min(canvasCount.value, Math.max(1, Math.floor(idx.active)))
    persistIndex()
  } else {
    const legacy = localStorage.getItem(LEGACY_SAVE_KEY)
    if (legacy) {
      try {
        localStorage.setItem(canvasKey(1), legacy)
        localStorage.removeItem(LEGACY_SAVE_KEY)
      } catch {
        // 迁移写失败：保留旧档，本次按空画布 1 起步
      }
    }
    canvasIndex.value = 1
    canvasCount.value = 1
    persistIndex()
  }
  const restored = loadCanvas(canvasIndex.value)
  if (restored) ElMessage.success('已恢复上次画稿')
})

onBeforeUnmount(() => {
  ro?.disconnect()
  themeObs?.disconnect()
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('keyup', onKeyup)
  if (rafId) cancelAnimationFrame(rafId)
  clearTimeout(saveTimer)
})

watch(tool, () => {
  drag = null
  renderer?.setPreview(null)
  refreshOverlay()
})
</script>

<template>
  <div class="page" :class="{ 'page-fs': isFullscreen }">
    <div class="page-head">
      <div class="kicker">BOARD</div>
      <h1 class="page-title">画板<small>课堂随手画：手绘笔迹 + 快速表格 / 树 / 图；画稿只存本机，讲完即删</small></h1>
    </div>

    <div class="card board-card">
      <div class="toolbar">
        <button
          v-for="t in tools"
          :key="t.id"
          type="button"
          class="tbtn"
          :class="{ on: tool === t.id }"
          :title="t.label"
          :aria-label="t.label"
          @click="tool = t.id"
        >
          <component :is="t.icon" class="ticon" />
        </button>
        <span class="tdiv" />
        <button
          v-for="c in colorSlots"
          :key="c.slot"
          type="button"
          class="swatch"
          :class="{ on: color === c.slot }"
          :style="{ background: c.css }"
          :title="c.name"
          :aria-label="c.name"
          @click="color = c.slot"
        />
        <span class="tdiv" />
        <button
          v-for="w in weights"
          :key="w"
          type="button"
          class="wbtn"
          :class="{ on: weight === w }"
          :title="`粗细 ${w}`"
          :aria-label="`粗细 ${w}`"
          @click="weight = w"
        >
          <span class="wdot" :style="{ width: `${3 + w * 3}px`, height: `${3 + w * 3}px` }" />
        </button>
        <span class="tdiv" />
        <button type="button" class="tbtn" :disabled="!canUndo" title="撤销 (Ctrl+Z)" aria-label="撤销" @click="doUndo">
          <Undo2 class="ticon" />
        </button>
        <button type="button" class="tbtn" :disabled="!canRedo" title="重做 (Ctrl+Shift+Z)" aria-label="重做" @click="doRedo">
          <Redo2 class="ticon" />
        </button>
        <button type="button" class="tbtn" title="清空画布" aria-label="清空画布" @click="clearAll">
          <Trash2 class="ticon" />
        </button>
        <span class="tb-spacer" />
        <el-button size="small" @click="doExport('png')">导出 PNG</el-button>
        <el-button size="small" @click="doExport('svg')">导出 SVG</el-button>
      </div>

      <div v-if="tool === 'graph'" class="ctx-bar">
        <el-switch v-model="treeMode" active-text="树式布子" />
        <el-switch v-model="directed" active-text="有向边" />
        <el-button size="small" type="primary" plain @click="tidy">
          <Sparkles class="bicon" />一键整理
        </el-button>
        <span class="ctx-hint">点击空白落节点；从节点拖到空白＝建子节点并连线，拖到另一节点＝连边；双击节点 / 连线改文字与权重</span>
      </div>
      <div v-else-if="tool === 'table'" class="ctx-bar">
        <span class="ctx-hint">在画布上按住拖出表格（跟手显示行列），松手生成；双击单元格填写——DP 表直接开讲</span>
      </div>
      <div v-else-if="tool === 'text'" class="ctx-bar">
        <span class="ctx-hint">点击画布放置文字；双击已有文字修改；选中后 Delete 删除</span>
      </div>

      <div ref="stageEl" class="board-stage">
        <svg
          ref="svgEl"
          class="board-svg"
          :class="cursorClass"
          :viewBox="viewBox"
          @pointerdown="onPointerDown"
          @pointermove="onPointerMove"
          @pointerup="onPointerUp"
          @pointercancel="onPointerUp"
          @dblclick="onDblClick"
          @wheel.prevent="onWheel"
        />
        <textarea
          v-if="editing"
          ref="editorEl"
          v-model="editorText"
          class="board-editor"
          :style="editorStyle"
          :wrap="editing.kind === 'text' ? 'off' : 'soft'"
          @keydown.stop
          @keydown.enter.exact.prevent="commitEdit"
          @keydown.esc.prevent="cancelEdit"
          @blur="commitEdit"
          @input="autosizeEditor"
        />
        <div v-if="tableBar" class="table-bar" :style="tableBarStyle">
          <button type="button" class="tbar-btn" title="末尾加一行" @click="tableResize('row', 1)">+行</button>
          <button type="button" class="tbar-btn" title="删最后一行" @click="tableResize('row', -1)">-行</button>
          <button type="button" class="tbar-btn" title="末尾加一列" @click="tableResize('col', 1)">+列</button>
          <button type="button" class="tbar-btn" title="删最后一列" @click="tableResize('col', -1)">-列</button>
        </div>
        <div class="canvas-tabs">
          <button
            v-for="n in canvasCount"
            :key="n"
            type="button"
            class="ctab"
            :class="{ on: n === canvasIndex }"
            :title="`切换到画布 ${n}`"
            :aria-label="`切换到画布 ${n}`"
            @click="switchCanvas(n)"
          >
            {{ n }}
          </button>
          <button
            v-if="canvasCount < CANVAS_MAX"
            type="button"
            class="ctab cadd"
            title="新建画布（最多 5 个，仅存本机）"
            aria-label="新建画布"
            @click="createCanvas"
          >
            +
          </button>
        </div>
        <div class="board-zoomer">
          <button type="button" class="zbtn" title="缩小" aria-label="缩小" @click="zoomAt(1 / 1.25)">
            <ZoomOut class="ticon" />
          </button>
          <span class="znum">{{ zoomPct }}%</span>
          <button type="button" class="zbtn" title="放大" aria-label="放大" @click="zoomAt(1.25)">
            <ZoomIn class="ticon" />
          </button>
          <button type="button" class="zbtn" title="适应内容" aria-label="适应内容" @click="fitView">
            <Maximize class="ticon" />
          </button>
          <button type="button" class="zbtn ztxt" title="重置 100%" aria-label="重置缩放" @click="resetZoom">1:1</button>
          <button
            type="button"
            class="zbtn"
            :title="isFullscreen ? '退出全屏' : '全屏画布'"
            :aria-label="isFullscreen ? '退出全屏' : '全屏画布'"
            @click="toggleFullscreen"
          >
            <Minimize2 v-if="isFullscreen" class="ticon" />
            <Fullscreen v-else class="ticon" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.board-card {
  padding: var(--soj-sp-16);
}

.tb-spacer {
  flex: 1;
}

.tbtn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--soj-ink-2);
  cursor: pointer;
}

.tbtn:hover {
  background: var(--soj-surface-2);
  color: var(--soj-ink);
}

.tbtn.on {
  background: var(--soj-blue-soft);
  border-color: var(--soj-blue-line);
  color: var(--soj-blue);
}

.tbtn:disabled {
  opacity: 0.45;
  cursor: default;
  background: transparent;
  color: var(--soj-ink-2);
}

.ticon {
  width: 16px;
  height: 16px;
}

.tdiv {
  width: 1px;
  height: 20px;
  background: var(--soj-line-2);
  margin: 0 var(--soj-sp-4);
}

.swatch {
  width: 20px;
  height: 20px;
  border-radius: var(--soj-radius-pill);
  border: 2px solid var(--soj-white);
  box-shadow: 0 0 0 1px var(--soj-line-2);
  cursor: pointer;
  padding: 0;
}

.swatch.on {
  box-shadow: 0 0 0 2px var(--soj-blue);
}

.wbtn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
  padding: 0;
}

.wbtn.on {
  background: var(--soj-surface-2);
  border-color: var(--soj-line);
}

.wdot {
  display: block;
  border-radius: var(--soj-radius-pill);
  background: var(--soj-ink);
}

.bicon {
  width: 14px;
  height: 14px;
  margin-right: var(--soj-sp-4);
}

.ctx-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--soj-sp-14);
  margin-bottom: var(--soj-sp-12);
}

.ctx-hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

.board-stage {
  position: relative;
  height: calc(100vh - 248px);
  min-height: 420px;
  border: 1px solid var(--soj-line);
  border-radius: 12px;
  background: var(--soj-white);
  overflow: hidden;
  /* 画布禁文字选择：拖拽手势不得触发浏览器原生选择（会 pointercancel 中断绘制） */
  user-select: none;
}

/* 浏览器内铺满模式：fixed 盖住导航以下整页（导航 sticky z-50 保持可见可点），
   隐藏页头标题区，卡片与舞台吃满剩余高度 */
.page-fs {
  position: fixed;
  inset: var(--soj-nav-h) 0 0;
  z-index: 49;
  display: flex;
  flex-direction: column;
  max-width: none;
  margin: 0;
  padding: var(--soj-sp-12) var(--soj-sp-16);
  overflow: auto;
}

.page-fs .page-head {
  display: none;
}

.page-fs .board-card {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
}

.page-fs .board-stage {
  flex: 1;
  height: auto;
  min-height: 0;
}

.board-svg {
  display: block;
  width: 100%;
  height: 100%;
  touch-action: none;
}

.cur-draw {
  cursor: crosshair;
}

.cur-move {
  cursor: default;
}

.cur-grab {
  cursor: grab;
}

.cur-grabbing {
  cursor: grabbing;
}

.board-editor {
  position: absolute;
  z-index: 5;
  background: var(--soj-white);
  border: 1px dashed var(--soj-blue);
  border-radius: 6px;
  padding: 2px 6px;
  resize: none;
  outline: none;
  line-height: 1.3;
  overflow: hidden;
  box-shadow: var(--soj-shadow-card);
}

.table-bar {
  position: absolute;
  z-index: 5;
  display: flex;
  gap: var(--soj-sp-2);
  background: var(--el-bg-color);
  border: 1px solid var(--soj-line);
  border-radius: var(--soj-radius-pill);
  padding: 2px var(--soj-sp-4);
  box-shadow: var(--soj-shadow-card);
}

.tbar-btn {
  border: none;
  background: transparent;
  color: var(--soj-ink-2);
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-medium);
  border-radius: var(--soj-radius-pill);
  padding: 2px var(--soj-sp-6);
  cursor: pointer;
}

.tbar-btn:hover {
  background: var(--soj-surface-2);
  color: var(--soj-blue);
}

.board-zoomer {
  position: absolute;
  right: var(--soj-sp-12);
  bottom: var(--soj-sp-12);
  z-index: 6;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-2);
  background: var(--el-bg-color);
  border: 1px solid var(--soj-line);
  border-radius: var(--soj-radius-pill);
  padding: 2px var(--soj-sp-6);
  box-shadow: var(--soj-shadow-card);
}

/* 左下角画布切换条：多画布（≤5）本地缓存，与右下角缩放器同款药丸 */
.canvas-tabs {
  position: absolute;
  left: var(--soj-sp-12);
  bottom: var(--soj-sp-12);
  z-index: 6;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-2);
  background: var(--el-bg-color);
  border: 1px solid var(--soj-line);
  border-radius: var(--soj-radius-pill);
  padding: 2px var(--soj-sp-6);
  box-shadow: var(--soj-shadow-card);
}

.ctab {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 26px;
  height: 26px;
  border: 1px solid transparent;
  border-radius: var(--soj-radius-pill);
  background: transparent;
  color: var(--soj-ink-2);
  font-size: var(--soj-fs-12);
  cursor: pointer;
  padding: 0 var(--soj-sp-4);
}

.ctab:hover {
  background: var(--soj-surface-2);
  color: var(--soj-blue);
}

.ctab.on {
  background: var(--soj-blue-soft);
  border-color: var(--soj-blue-line);
  color: var(--soj-blue);
  font-weight: var(--soj-fw-medium);
}

.zbtn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: var(--soj-radius-pill);
  background: transparent;
  color: var(--soj-ink-2);
  cursor: pointer;
  padding: 0;
}

.zbtn:hover {
  background: var(--soj-surface-2);
  color: var(--soj-blue);
}

.znum {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-2);
  min-width: 40px;
  text-align: center;
}

.ztxt {
  width: auto;
  padding: 0 var(--soj-sp-6);
  font-size: var(--soj-fs-12);
}

/* <992px 折断点（与 PageShell 同口径）：画布高度收窄防溢出 */
@media (max-width: 992px) {
  .board-stage {
    height: calc(100vh - 320px);
    min-height: 340px;
  }
}
</style>

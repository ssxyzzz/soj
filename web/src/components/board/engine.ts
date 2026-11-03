// 手绘画板引擎（v0.60）：纯 TS 无 UI——shape 数据模型、SVG 原生图元（朴素线条，
// 2026-11-03 用户拍板弃 roughjs 手绘风）+ perfect-freehand 画笔渲染、快照历史、
// 几何命中测试、树布局整理、导出序列化。交互状态机在 BoardView.vue，
// 本文件不监听任何 DOM 事件；主画布与导出共用同一渲染管线，保证所见即所得。
// 体积红线：perfect-freehand ~2KB（gzip），本文件只做薄封装。
import { getStroke } from 'perfect-freehand'

export const SVG_NS = 'http://www.w3.org/2000/svg'

// 调色板槽位：shape 只存槽名，渲染时解析——主题切换（v5/navy）重渲染即自动跟随，
// .vue/.ts 全程零裸色值（token 由视图层从 CSS 变量读入）
export type ColorSlot = 'ink' | 'blue' | 'teal' | 'warning' | 'danger'

export interface Palette {
  ink: string
  blue: string
  teal: string
  warning: string
  danger: string
  paper: string
}

export interface ShapeStyle {
  color: ColorSlot
  weight: 1 | 2 | 3
}

export interface StrokeShape {
  id: number
  type: 'stroke'
  points: [number, number][]
  style: ShapeStyle
}
export interface LineShape {
  id: number
  type: 'line'
  x1: number
  y1: number
  x2: number
  y2: number
  style: ShapeStyle
}
export interface ArrowShape {
  id: number
  type: 'arrow'
  x1: number
  y1: number
  x2: number
  y2: number
  style: ShapeStyle
}
export interface RectShape {
  id: number
  type: 'rect'
  x: number
  y: number
  w: number
  h: number
  style: ShapeStyle
}
export interface EllipseShape {
  id: number
  type: 'ellipse'
  x: number
  y: number
  w: number
  h: number
  style: ShapeStyle
}
export interface TextShape {
  id: number
  type: 'text'
  // x/y 为文字基线起点
  x: number
  y: number
  content: string
  size: number
  style: ShapeStyle
}
export interface TableShape {
  id: number
  type: 'table'
  x: number
  y: number
  rows: number
  cols: number
  cw: number
  ch: number
  cells: string[]
  style: ShapeStyle
}
export interface NodeShape {
  id: number
  type: 'node'
  x: number
  y: number
  r: number
  label: string
  style: ShapeStyle
}
export interface EdgeShape {
  id: number
  type: 'edge'
  // 存节点 id 引用而非坐标：端点渲染时按节点当前位置推导（拖动跟随/删点断边零维护）
  from: number
  to: number
  label: string
  directed: boolean
  style: ShapeStyle
}

export type Shape =
  | StrokeShape
  | LineShape
  | ArrowShape
  | RectShape
  | EllipseShape
  | TextShape
  | TableShape
  | NodeShape
  | EdgeShape

export type Tool =
  | 'select'
  | 'pen'
  | 'line'
  | 'arrow'
  | 'rect'
  | 'ellipse'
  | 'text'
  | 'table'
  | 'graph'
  | 'eraser'

// 画布手写字体链（有楷体则手写感，无则回退系统体）——零字节换观感
export const HAND_FONT =
  "'Kaiti SC','KaiTi','STKaiti','AR PL UKai CN','Segoe Print','Comic Sans MS',cursive"

export const TEXT_SIZE = 16
export const CELL_W = 48
export const CELL_H = 38
export const CELL_MAX = 12
export const NODE_R = 18
export const TREE_GAP_X = 64
export const TREE_GAP_Y = 92

const WEIGHT_W: Record<number, number> = { 1: 1.6, 2: 2.4, 3: 3.4 }
const HISTORY_MAX = 60

// ---------- 基础工具 ----------

export function el(name: string, attrs: Record<string, string | number>): SVGElement {
  const n = document.createElementNS(SVG_NS, name)
  for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v))
  return n
}

export function hexToRgba(hex: string, alpha: number): string {
  const m = hex.trim().match(/^#([0-9a-f]{6})$/i)
  if (!m) return hex // 非 #rrggbb（理论上 token 恒为 hex）原样返回
  const n = parseInt(m[1], 16)
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
}

export function distToSeg(px: number, py: number, x1: number, y1: number, x2: number, y2: number): number {
  const dx = x2 - x1
  const dy = y2 - y1
  const len2 = dx * dx + dy * dy
  if (len2 === 0) return Math.hypot(px - x1, py - y1)
  const t = Math.max(0, Math.min(1, ((px - x1) * dx + (py - y1) * dy) / len2))
  return Math.hypot(px - (x1 + t * dx), py - (y1 + t * dy))
}

let measureCtx: CanvasRenderingContext2D | null = null
export function textWidth(content: string, size: number): number {
  if (!content) return 0
  if (!measureCtx) measureCtx = document.createElement('canvas').getContext('2d')
  if (!measureCtx) return content.length * size
  measureCtx.font = `${size}px ${HAND_FONT}`
  return measureCtx.measureText(content).width
}

// ---------- 渲染 ----------

// perfect-freehand 输出 → SVG path d（官方 quadratic 平滑收口法）
export function strokePathD(points: [number, number][], weight: 1 | 2 | 3, last: boolean): string {
  const size = [4, 5.5, 7.5][weight - 1]
  const outline = getStroke(points as number[][], {
    size,
    thinning: 0.55,
    smoothing: 0.5,
    streamline: 0.5,
    simulatePressure: true,
    last,
  })
  if (outline.length < 2) return ''
  const d: (string | number)[] = ['M', outline[0][0].toFixed(1), outline[0][1].toFixed(1), 'Q']
  for (let i = 0; i < outline.length; i++) {
    const p = outline[i]
    const q = outline[(i + 1) % outline.length]
    d.push(p[0].toFixed(1), p[1].toFixed(1), ((p[0] + q[0]) / 2).toFixed(1), ((p[1] + q[1]) / 2).toFixed(1))
  }
  d.push('Z')
  return d.join(' ')
}

function halo(t: SVGElement, paper: string): void {
  // 文字白边（paint-order: stroke）：压在任何线条上都可读
  t.setAttribute('stroke', paper)
  t.setAttribute('stroke-width', '3')
  t.setAttribute('stroke-linejoin', 'round')
  t.setAttribute('paint-order', 'stroke')
}

// 单个 shape → 一个 <g data-id>；主画布与导出走同一入口
export function renderShapeInto(
  parent: SVGElement,
  s: Shape,
  pal: Palette,
  nodeById: Map<number, NodeShape>,
): SVGGElement {
  const g = el('g', { 'data-id': s.id }) as SVGGElement
  const color = pal[s.style.color]
  const sw = WEIGHT_W[s.style.weight]
  // 朴素线条公共描边属性
  const lineAttrs = (extra: Record<string, string | number> = {}) => ({
    stroke: color,
    'stroke-width': sw,
    'stroke-linecap': 'round',
    'stroke-linejoin': 'round',
    fill: 'none',
    ...extra,
  })
  switch (s.type) {
    case 'stroke': {
      const p = el('path', { d: strokePathD(s.points, s.style.weight, true), fill: color })
      g.appendChild(p)
      break
    }
    case 'line': {
      g.appendChild(el('line', lineAttrs({ x1: s.x1, y1: s.y1, x2: s.x2, y2: s.y2 })))
      break
    }
    case 'arrow': {
      g.appendChild(el('line', lineAttrs({ x1: s.x1, y1: s.y1, x2: s.x2, y2: s.y2 })))
      appendArrowHead(g, s.x2, s.y2, Math.atan2(s.y2 - s.y1, s.x2 - s.x1), 11 + s.style.weight * 2, 4 + sw, color)
      break
    }
    case 'rect': {
      g.appendChild(el('rect', lineAttrs({ x: s.x, y: s.y, width: s.w, height: s.h })))
      break
    }
    case 'ellipse': {
      g.appendChild(el('ellipse', lineAttrs({ cx: s.x + s.w / 2, cy: s.y + s.h / 2, rx: s.w / 2, ry: s.h / 2 })))
      break
    }
    case 'text': {
      const t = el('text', { x: s.x, y: s.y, fill: color, 'font-size': s.size, 'font-family': HAND_FONT })
      t.textContent = s.content
      halo(t, pal.paper)
      g.appendChild(t)
      break
    }
    case 'table': {
      const w = s.cols * s.cw
      const h = s.rows * s.ch
      g.appendChild(el('rect', lineAttrs({ x: s.x, y: s.y, width: w, height: h })))
      for (let c = 1; c < s.cols; c++)
        g.appendChild(el('line', lineAttrs({ x1: s.x + c * s.cw, y1: s.y, x2: s.x + c * s.cw, y2: s.y + h })))
      for (let r = 1; r < s.rows; r++)
        g.appendChild(el('line', lineAttrs({ x1: s.x, y1: s.y + r * s.ch, x2: s.x + w, y2: s.y + r * s.ch })))
      s.cells.forEach((v, i) => {
        if (!v) return
        const t = el('text', {
          x: s.x + (i % s.cols) * s.cw + s.cw / 2,
          y: s.y + Math.floor(i / s.cols) * s.ch + s.ch / 2,
          fill: color,
          'font-size': 14,
          'font-family': HAND_FONT,
          'text-anchor': 'middle',
          'dominant-baseline': 'central',
        })
        t.textContent = v
        halo(t, pal.paper)
        g.appendChild(t)
      })
      break
    }
    case 'node': {
      g.appendChild(el('circle', { cx: s.x, cy: s.y, r: s.r, fill: hexToRgba(color, 0.12), stroke: color, 'stroke-width': sw }))
      const t = el('text', {
        x: s.x,
        y: s.y,
        fill: color,
        'font-size': 14,
        'font-family': HAND_FONT,
        'text-anchor': 'middle',
        'dominant-baseline': 'central',
      })
      t.textContent = s.label
      halo(t, pal.paper)
      g.appendChild(t)
      break
    }
    case 'edge': {
      const a = nodeById.get(s.from)
      const b = nodeById.get(s.to)
      if (!a || !b) break // 端点缺失（理论不发生）：静默不画
      const dx = b.x - a.x
      const dy = b.y - a.y
      const d = Math.hypot(dx, dy) || 1
      const ux = dx / d
      const uy = dy / d
      const x1 = a.x + ux * (a.r + 3)
      const y1 = a.y + uy * (a.r + 3)
      const x2 = b.x - ux * (b.r + 3)
      const y2 = b.y - uy * (b.r + 3)
      g.appendChild(el('line', lineAttrs({ x1, y1, x2, y2 })))
      if (s.directed) appendArrowHead(g, x2, y2, Math.atan2(y2 - y1, x2 - x1), 11 + s.style.weight * 2, 4 + sw, color)
      if (s.label) {
        // 权重标在边中点沿法线上移 12，白边保证压线可读
        const t = el('text', {
          x: ((x1 + x2) / 2 + uy * 12).toFixed(1),
          y: ((y1 + y2) / 2 - ux * 12).toFixed(1),
          fill: pal.ink,
          'font-size': 12,
          'font-family': HAND_FONT,
          'text-anchor': 'middle',
          'dominant-baseline': 'central',
        })
        t.textContent = s.label
        halo(t, pal.paper)
        g.appendChild(t)
      }
      break
    }
  }
  parent.appendChild(g)
  return g
}

// 箭头：朴素实心三角
function appendArrowHead(g: SVGElement, x: number, y: number, angle: number, len: number, hw: number, color: string): void {
  const ca = Math.cos(angle)
  const sa = Math.sin(angle)
  const d = [
    `M${x.toFixed(1)} ${y.toFixed(1)}`,
    `L${(x - len * ca + hw * sa).toFixed(1)} ${(y - len * sa - hw * ca).toFixed(1)}`,
    `L${(x - len * ca - hw * sa).toFixed(1)} ${(y - len * sa + hw * ca).toFixed(1)}`,
    'Z',
  ].join(' ')
  g.appendChild(el('path', { d, fill: color }))
}

export interface BBox {
  x: number
  y: number
  w: number
  h: number
}

export function bboxOf(s: Shape, nodeById: Map<number, NodeShape>): BBox | null {
  switch (s.type) {
    case 'stroke': {
      let minX = Infinity
      let minY = Infinity
      let maxX = -Infinity
      let maxY = -Infinity
      for (const [x, y] of s.points) {
        minX = Math.min(minX, x)
        minY = Math.min(minY, y)
        maxX = Math.max(maxX, x)
        maxY = Math.max(maxY, y)
      }
      if (!isFinite(minX)) return null
      return pad({ x: minX, y: minY, w: maxX - minX, h: maxY - minY }, 6)
    }
    case 'line':
    case 'arrow':
      return pad({
        x: Math.min(s.x1, s.x2),
        y: Math.min(s.y1, s.y2),
        w: Math.abs(s.x2 - s.x1),
        h: Math.abs(s.y2 - s.y1),
      }, 4)
    case 'rect':
      return { x: s.x, y: s.y, w: s.w, h: s.h }
    case 'ellipse':
      return { x: s.x, y: s.y, w: s.w, h: s.h }
    case 'text': {
      const w = textWidth(s.content, s.size)
      return { x: s.x, y: s.y - s.size, w: Math.max(w, s.size), h: s.size * 1.35 }
    }
    case 'table':
      return { x: s.x, y: s.y, w: s.cols * s.cw, h: s.rows * s.ch }
    case 'node':
      return { x: s.x - s.r, y: s.y - s.r, w: s.r * 2, h: s.r * 2 }
    case 'edge': {
      const a = nodeById.get(s.from)
      const b = nodeById.get(s.to)
      if (!a || !b) return null
      return pad({
        x: Math.min(a.x, b.x),
        y: Math.min(a.y, b.y),
        w: Math.abs(b.x - a.x),
        h: Math.abs(b.y - a.y),
      }, 10)
    }
  }
}

function pad(b: BBox, p: number): BBox {
  return { x: b.x - p, y: b.y - p, w: b.w + p * 2, h: b.h + p * 2 }
}

export function bboxesIntersect(a: BBox, b: BBox): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
}

// —— 单选手柄缩放：把 shape 几何从旧包围盒线性映射到新包围盒（锚定左上角）——
// 连线(edge)由节点推导，不参与；文字缩放的是字号；表格缩放的是格子尺寸
export function resizeShape(s: Shape, from: BBox, to: BBox): void {
  const sx = from.w > 0.5 ? to.w / from.w : 1
  const sy = from.h > 0.5 ? to.h / from.h : 1
  const rx = (x: number) => to.x + (x - from.x) * sx
  const ry = (y: number) => to.y + (y - from.y) * sy
  switch (s.type) {
    case 'stroke':
      for (const p of s.points) {
        p[0] = rx(p[0])
        p[1] = ry(p[1])
      }
      break
    case 'line':
    case 'arrow':
      s.x1 = rx(s.x1)
      s.y1 = ry(s.y1)
      s.x2 = rx(s.x2)
      s.y2 = ry(s.y2)
      break
    case 'rect':
    case 'ellipse':
      s.x = to.x
      s.y = to.y
      s.w = to.w
      s.h = to.h
      break
    case 'text': {
      const size = Math.max(10, Math.min(72, Math.round(s.size * sy)))
      s.x = to.x
      s.y = to.y + size // 基线锚在新包围盒顶（bbox.y 即基线减字号）
      s.size = size
      break
    }
    case 'table':
      s.cw = Math.max(20, Math.min(220, to.w / s.cols))
      s.ch = Math.max(16, Math.min(180, to.h / s.rows))
      break
    case 'node':
      s.r = Math.max(8, Math.min(80, Math.min(to.w, to.h) / 2))
      s.x = to.x + to.w / 2
      s.y = to.y + to.h / 2
      break
    case 'edge':
      break
  }
}

// ---------- 渲染器：分层（图形/预览/选区覆盖），按 id 增量更新 ----------

export class BoardRenderer {
  private layer: SVGGElement
  private previewLayer: SVGGElement
  private overlayLayer: SVGGElement
  private groups = new Map<number, SVGGElement>()
  private pal: Palette

  constructor(svg: SVGSVGElement, pal: Palette) {
    this.pal = pal
    this.layer = el('g', { 'data-role': 'shapes' }) as SVGGElement
    this.previewLayer = el('g', { 'data-role': 'preview' }) as SVGGElement
    this.overlayLayer = el('g', { 'data-role': 'overlay', 'pointer-events': 'none' }) as SVGGElement
    svg.append(this.layer, this.previewLayer, this.overlayLayer)
  }

  setPalette(pal: Palette): void {
    this.pal = pal
  }

  renderAll(model: BoardModel): void {
    this.layer.replaceChildren()
    this.groups.clear()
    const byId = model.nodeById()
    for (const s of model.shapes) this.groups.set(s.id, renderShapeInto(this.layer, s, this.pal, byId))
  }

  // 按 id 原位重建（保持 z 序）
  renderIds(model: BoardModel, ids: Iterable<number>): void {
    const byId = model.nodeById()
    for (const id of ids) {
      const s = model.get(id)
      const old = this.groups.get(id)
      if (!s || !old) continue
      const next = old.nextSibling
      old.remove()
      const g = renderShapeInto(this.layer, s, this.pal, byId)
      if (next) this.layer.insertBefore(g, next)
      this.groups.set(id, g)
    }
  }

  removeIds(ids: Iterable<number>): void {
    for (const id of ids) {
      this.groups.get(id)?.remove()
      this.groups.delete(id)
    }
  }

  upsert(model: BoardModel, id: number): void {
    const s = model.get(id)
    if (!s) return
    const g = renderShapeInto(this.layer, s, this.pal, model.nodeById())
    this.groups.set(id, g)
  }

  // 拖动期零重渲染：仅平移已渲染的 <g>（含被拖节点的连线），落点后再 renderIds 校正
  transformGroups(ids: Iterable<number>, dx: number, dy: number): void {
    for (const id of ids) this.groups.get(id)?.setAttribute('transform', `translate(${dx} ${dy})`)
  }

  clearTransforms(ids: Iterable<number>): void {
    for (const id of ids) this.groups.get(id)?.removeAttribute('transform')
  }

  // 预览层：绘制中的草稿 / 连线预览
  setPreview(fn: ((g: SVGGElement, pal: Palette) => void) | null): void {
    this.previewLayer.replaceChildren()
    if (fn) fn(this.previewLayer, this.pal)
  }

  // 选区覆盖层：选中框 + 框选矩形 + 表格缩放手柄（虚线 non-scaling-stroke 随缩放恒定线宽；
  // 手柄为恒定屏幕尺寸的白底方块，锚在表格右下角）
  refreshOverlay(
    model: BoardModel,
    selected: Set<number>,
    marquee: BBox | null = null,
    scale = 1,
    handle: { x: number; y: number } | null = null,
  ): void {
    this.overlayLayer.replaceChildren()
    const byId = model.nodeById()
    for (const id of selected) {
      const s = model.get(id)
      if (!s) continue
      const bb = bboxOf(s, byId)
      if (!bb) continue
      this.overlayLayer.appendChild(this.dashRect(bb.x - 4, bb.y - 4, bb.w + 8, bb.h + 8))
    }
    if (marquee) this.overlayLayer.appendChild(this.dashRect(marquee.x, marquee.y, marquee.w, marquee.h))
    if (handle) {
      const hs = 12 / scale
      this.overlayLayer.appendChild(
        el('rect', {
          x: (handle.x - hs / 2).toFixed(1),
          y: (handle.y - hs / 2).toFixed(1),
          width: hs.toFixed(1),
          height: hs.toFixed(1),
          fill: this.pal.paper,
          stroke: this.pal.blue,
          'stroke-width': 1.5,
          'vector-effect': 'non-scaling-stroke',
        }),
      )
    }
  }

  private dashRect(x: number, y: number, w: number, h: number): SVGElement {
    return el('rect', {
      x: x.toFixed(1),
      y: y.toFixed(1),
      width: Math.max(w, 1).toFixed(1),
      height: Math.max(h, 1).toFixed(1),
      rx: 6,
      fill: 'none',
      stroke: this.pal.blue,
      'stroke-width': 1.5,
      'stroke-dasharray': '7 5',
      'vector-effect': 'non-scaling-stroke',
    })
  }
}

// ---------- 数据模型：shapes + 快照历史 + 命中测试 + 树布局 ----------

interface DocV1 {
  v: 1
  idSeq: number
  labelSeq: number
  shapes: Shape[]
}

export class BoardModel {
  shapes: Shape[] = []
  idSeq = 1
  labelSeq = 1
  private undoStack: string[] = []
  private redoStack: string[] = []

  nextId(): number {
    return this.idSeq++
  }

  nextLabel(): string {
    return String(this.labelSeq++)
  }

  get(id: number): Shape | undefined {
    return this.shapes.find((s) => s.id === id)
  }

  add(s: Shape): void {
    s.id = this.nextId()
    this.shapes.push(s)
  }

  delete(ids: Set<number>): void {
    this.shapes = this.shapes.filter((s) => !ids.has(s.id))
  }

  nodeById(): Map<number, NodeShape> {
    const m = new Map<number, NodeShape>()
    for (const s of this.shapes) if (s.type === 'node') m.set(s.id, s)
    return m
  }

  // 与选中节点相连的边（拖动节点时跟随平移的渲染组）
  edgesOf(ids: Set<number>): number[] {
    return this.shapes.filter((s) => s.type === 'edge' && (ids.has(s.from) || ids.has(s.to))).map((s) => s.id)
  }

  translate(ids: Set<number>, dx: number, dy: number): void {
    for (const s of this.shapes) {
      if (!ids.has(s.id)) continue
      switch (s.type) {
        case 'stroke':
          for (const p of s.points) {
            p[0] += dx
            p[1] += dy
          }
          break
        case 'line':
        case 'arrow':
          s.x1 += dx
          s.y1 += dy
          s.x2 += dx
          s.y2 += dy
          break
        case 'rect':
        case 'ellipse':
        case 'text':
        case 'table':
        case 'node':
          s.x += dx
          s.y += dy
          break
        case 'edge':
          break // 端点由节点推导，不参与平移
      }
    }
  }

  // —— 快照历史：整体 JSON 深拷贝，课堂规模毫秒级，代码量比命令模式小一个量级 ——
  snapshot(): string {
    const d: DocV1 = { v: 1, idSeq: this.idSeq, labelSeq: this.labelSeq, shapes: this.shapes }
    return JSON.stringify(d)
  }

  private restore(raw: string): void {
    const d = JSON.parse(raw) as DocV1
    this.shapes = d.shapes
    this.idSeq = d.idSeq
    this.labelSeq = d.labelSeq
  }

  pushUndo(): void {
    this.undoStack.push(this.snapshot())
    if (this.undoStack.length > HISTORY_MAX) this.undoStack.shift()
    this.redoStack = []
  }

  // 误推的空快照回退（如一键整理发现无可整理时）
  popUndo(): void {
    this.undoStack.pop()
  }

  undo(): boolean {
    const prev = this.undoStack.pop()
    if (!prev) return false
    this.redoStack.push(this.snapshot())
    this.restore(prev)
    return true
  }

  redo(): boolean {
    const next = this.redoStack.pop()
    if (!next) return false
    this.undoStack.push(this.snapshot())
    this.restore(next)
    return true
  }

  get canUndo(): boolean {
    return this.undoStack.length > 0
  }

  get canRedo(): boolean {
    return this.redoStack.length > 0
  }

  load(raw: string): boolean {
    try {
      const d = JSON.parse(raw) as DocV1
      if (d?.v === 1 && Array.isArray(d.shapes) && typeof d.idSeq === 'number') {
        this.shapes = d.shapes
        this.idSeq = d.idSeq
        this.labelSeq = typeof d.labelSeq === 'number' ? d.labelSeq : 1
        return true
      }
    } catch {
      // 画稿仅本地暂存，坏档直接当无存档处理
    }
    return false
  }

  // —— 命中测试：自顶向下首个命中；闭合形按包围盒内命中（后画的在上层优先）——
  hitTest(x: number, y: number, tol: number): Shape | null {
    const byId = this.nodeById()
    for (let i = this.shapes.length - 1; i >= 0; i--) {
      if (this.hitShape(this.shapes[i], x, y, tol, byId)) return this.shapes[i]
    }
    return null
  }

  private hitShape(s: Shape, x: number, y: number, tol: number, byId: Map<number, NodeShape>): boolean {
    switch (s.type) {
      case 'stroke': {
        if (s.points.length === 1) return Math.hypot(x - s.points[0][0], y - s.points[0][1]) <= tol
        for (let i = 1; i < s.points.length; i++) {
          if (distToSeg(x, y, s.points[i - 1][0], s.points[i - 1][1], s.points[i][0], s.points[i][1]) <= tol) return true
        }
        return false
      }
      case 'line':
      case 'arrow':
        return distToSeg(x, y, s.x1, s.y1, s.x2, s.y2) <= tol + WEIGHT_W[s.style.weight] * 1.5
      case 'rect':
        return x >= s.x && x <= s.x + s.w && y >= s.y && y <= s.y + s.h
      case 'ellipse': {
        const rx = s.w / 2 || 1
        const ry = s.h / 2 || 1
        const cx = s.x + rx
        const cy = s.y + ry
        return ((x - cx) / rx) ** 2 + ((y - cy) / ry) ** 2 <= 1
      }
      case 'text': {
        const bb = bboxOf(s, byId)
        return !!bb && x >= bb.x && x <= bb.x + bb.w && y >= bb.y && y <= bb.y + bb.h
      }
      case 'table':
        return x >= s.x && x <= s.x + s.cols * s.cw && y >= s.y && y <= s.y + s.rows * s.ch
      case 'node':
        return Math.hypot(x - s.x, y - s.y) <= s.r + tol / 2
      case 'edge': {
        const a = byId.get(s.from)
        const b = byId.get(s.to)
        if (!a || !b) return false
        const dx = b.x - a.x
        const dy = b.y - a.y
        const d = Math.hypot(dx, dy) || 1
        const ux = dx / d
        const uy = dy / d
        return (
          distToSeg(x, y, a.x + ux * (a.r + 3), a.y + uy * (a.r + 3), b.x - ux * (b.r + 3), b.y - uy * (b.r + 3)) <=
          tol + WEIGHT_W[s.style.weight]
        )
      }
    }
  }

  contentBBox(): BBox | null {
    const byId = this.nodeById()
    let minX = Infinity
    let minY = Infinity
    let maxX = -Infinity
    let maxY = -Infinity
    for (const s of this.shapes) {
      const bb = bboxOf(s, byId)
      if (!bb) continue
      minX = Math.min(minX, bb.x)
      minY = Math.min(minY, bb.y)
      maxX = Math.max(maxX, bb.x + bb.w)
      maxY = Math.max(maxY, bb.y + bb.h)
    }
    if (!isFinite(minX)) return null
    return pad({ x: minX, y: minY, w: maxX - minX, h: maxY - minY }, 24)
  }

  // —— 一键整理：对每棵连通树做「叶序槽位 + 父=子均值」的整洁布局，
  //    布局结果平移回组件原包围盒左上角，画完乱拖一键归位 ——
  tidyTree(): boolean {
    const nodes = this.shapes.filter((s): s is NodeShape => s.type === 'node')
    if (!nodes.length) return false
    const byId = this.nodeById()
    const children = new Map<number, number[]>()
    const hasIncoming = new Set<number>()
    for (const s of this.shapes) {
      if (s.type !== 'edge') continue
      if (!byId.has(s.from) || !byId.has(s.to)) continue
      const list = children.get(s.from) ?? []
      list.push(s.to)
      children.set(s.from, list)
      hasIncoming.add(s.to)
    }
    const orig = new Map<number, { x: number; y: number }>()
    for (const n of nodes) orig.set(n.id, { x: n.x, y: n.y })
    const pos = new Map<number, { x: number; y: number }>()
    const visited = new Set<number>()

    for (const n of nodes) {
      if (visited.has(n.id)) continue
      // 组件连通集
      const comp: number[] = []
      const stack = [n.id]
      const seen = new Set<number>([n.id])
      while (stack.length) {
        const cur = stack.pop() as number
        comp.push(cur)
        for (const nb of this.nodeNeighbors(cur, children)) {
          if (!seen.has(nb)) {
            seen.add(nb)
            stack.push(nb)
          }
        }
      }
      // 根 = 组件内无入边的最小 id；全有入边（环）取最小 id
      let root = comp[0]
      for (const id of comp) {
        if (!hasIncoming.has(id)) {
          root = id
          break
        }
        if (id < root) root = id
      }
      let leafSlot = 0
      const assign = (id: number, depth: number): number => {
        visited.add(id)
        const kids = (children.get(id) ?? []).filter((k) => byId.has(k) && !visited.has(k))
        let x: number
        if (!kids.length) {
          x = leafSlot++ * TREE_GAP_X
        } else {
          const xs = kids.map((k) => assign(k, depth + 1))
          x = xs.reduce((a, b) => a + b, 0) / xs.length
        }
        pos.set(id, { x, y: depth * TREE_GAP_Y })
        return x
      }
      assign(root, 0)
      // 平移回原包围盒左上角
      let minX = Infinity
      let minY = Infinity
      let oMinX = Infinity
      let oMinY = Infinity
      for (const id of comp) {
        const p = pos.get(id)
        const o = orig.get(id)
        if (!p || !o) continue
        minX = Math.min(minX, p.x)
        minY = Math.min(minY, p.y)
        oMinX = Math.min(oMinX, o.x)
        oMinY = Math.min(oMinY, o.y)
      }
      for (const id of comp) {
        const p = pos.get(id)
        const o = orig.get(id)
        const node = byId.get(id)
        if (!p || !o || !node) continue
        node.x = p.x - minX + oMinX
        node.y = p.y - minY + oMinY
      }
    }
    // 是否真的移动了
    return nodes.some((n) => {
      const o = orig.get(n.id)
      return !!o && (Math.abs(o.x - n.x) > 0.5 || Math.abs(o.y - n.y) > 0.5)
    })
  }

  private nodeNeighbors(id: number, children: Map<number, number[]>): number[] {
    const out: number[] = []
    for (const [from, list] of children) {
      if (from === id) out.push(...list)
      else if (list.includes(id)) out.push(from)
    }
    return out
  }
}

// ---------- 导出：按内容 bbox 组独立 SVG 文档（主画布同管线） ----------

export function buildExportSvg(model: BoardModel, pal: Palette): SVGSVGElement | null {
  const bb = model.contentBBox()
  if (!bb) return null
  const svg = document.createElementNS(SVG_NS, 'svg') as SVGSVGElement
  svg.setAttribute('xmlns', SVG_NS)
  svg.setAttribute('viewBox', `${bb.x.toFixed(1)} ${bb.y.toFixed(1)} ${bb.w.toFixed(1)} ${bb.h.toFixed(1)}`)
  svg.setAttribute('width', String(Math.round(bb.w)))
  svg.setAttribute('height', String(Math.round(bb.h)))
  svg.appendChild(el('rect', { x: bb.x, y: bb.y, width: bb.w, height: bb.h, fill: pal.paper }))
  const g = el('g', {}) as SVGGElement
  const byId = model.nodeById()
  for (const s of model.shapes) renderShapeInto(g, s, pal, byId)
  svg.appendChild(g)
  return svg
}

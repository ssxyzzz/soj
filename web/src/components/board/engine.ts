// 手绘画板引擎（v0.60）：纯 TS 无 UI——shape 数据模型、SVG 原生图元（朴素线条，
// 2026-11-03 用户拍板弃 roughjs 手绘风）+ perfect-freehand 画笔渲染、快照历史、
// 几何命中测试、树布局整理、导出序列化。交互状态机在 BoardView.vue，
// 本文件不监听任何 DOM 事件；主画布与导出共用同一渲染管线，保证所见即所得。
// 体积红线：perfect-freehand ~2KB（gzip），本文件只做薄封装；immer（v0.64 撤销栈
// patch 化引入，~5KB gzip）承担正/逆补丁生成，免手写每种操作的逆变换。
import { getStroke } from 'perfect-freehand'
import { applyPatches, enablePatches, produceWithPatches, setAutoFreeze, type Patch } from 'immer'

enablePatches()
// 文档保持普通对象：手势期（拖动/缩放）对 model 的直接改实时可见，
// 冻结会在 produce 之外的字段写入上抛错
setAutoFreeze(false)

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
  | 'hand'
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

// 画板字体（v0.60 用户拍板）：字母数字走 monaco 系等宽（Monaco 首选，未装依次回落
// JetBrains Mono/Consolas，与全站 --font-mono 同族），栈内等宽字体均无汉字，中文自然
// 回落 SOJ 默认 CJK 栈（Noto Sans SC → PingFang → 雅黑），与全站正文同源
export const BOARD_FONT =
  "Monaco, 'JetBrains Mono', Consolas, 'Courier New', 'Noto Sans SC', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', '微软雅黑', sans-serif"

export const TEXT_SIZE = 16
export const CELL_W = 48
export const CELL_H = 38
export const CELL_MAX = 12
export const NODE_R = 18
export const TREE_GAP_X = 64
export const TREE_GAP_Y = 92
// 文字行距系数（v0.76 多行唯一来源）：SVG tspan 逐行与编辑浮层 textarea line-height
// 共用此值，改一处两侧同步——所见即所得的根基
export const TEXT_LINE_H = 1.4
// 坐标重定基阈值：Skia 光栅化 SVG 几何用 float32，实测视口 2644 时内容 |坐标|≳1e8
// 即画成全空（1e11 处 float32 最小刻度 8192 > 视口宽），1e6 留两个量级安全余量
export const REBASE_LIMIT = 1e6

const WEIGHT_W: Record<number, number> = { 1: 1.6, 2: 2.4, 3: 3.4 }

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
  measureCtx.font = `${size}px ${BOARD_FONT}`
  return measureCtx.measureText(content).width
}

// 多行文字（v0.76）：按 \n 分行的块宽（最长行）与块高（行数×行距）。
// SVG tspan 逐行与编辑浮层 textarea 同源排布，二者所见即所得
export function textBlockWidth(content: string, size: number): number {
  const lines = content.split('\n')
  return Math.max(...lines.map((l) => textWidth(l, size)))
}

export function textBlockHeight(content: string, size: number): number {
  return content.split('\n').length * size * TEXT_LINE_H
}

// 多行 <text>：tspan 逐行排布（x 恒锚点，dy 步进行距）。baseline==='first' 时首行
// 基线在锚点（自由文字，与旧单行坐标完全兼容）；'center' 时整块沿锚点垂直居中
//（节点/单元格/边权，dominant-baseline:central 逐行继承）。单行退化后与旧渲染逐像素一致
function appendMultiLineText(
  parent: SVGElement,
  content: string,
  paper: string,
  attrs: Record<string, string | number>,
  baseline: 'first' | 'center',
): void {
  const lines = content.split('\n')
  const lh = Number(attrs['font-size']) * TEXT_LINE_H
  const n = lines.length
  let dy = baseline === 'first' ? 0 : (-(n - 1) / 2) * lh
  const t = el('text', attrs)
  for (const line of lines) {
    const ts = el('tspan', { x: attrs.x, dy: dy.toFixed(1) })
    ts.textContent = line
    t.appendChild(ts)
    dy = lh
  }
  halo(t, paper)
  parent.appendChild(t)
}

// 行盒内基线偏移 = half-leading + ascent（编辑浮层 textarea 与 SVG 文字基线对齐用）。
// 探针必须纯 ASCII（'Ag'）：textarea 行盒 strut 由栈首字体（Monaco 系等宽）决定，
// 混入 CJK 探针会取到更大的 fallback 字体度量，编辑框整体下坠 ~0.2em；
// fontBoundingBox* 缺失的浏览器按 0.8/0.2em 兜底
export function textBaselineInBox(size: number, lineHeight: number): number {
  const fallback = (lineHeight - size) / 2 + size * 0.8
  if (!measureCtx) measureCtx = document.createElement('canvas').getContext('2d')
  if (!measureCtx) return fallback
  measureCtx.font = `${size}px ${BOARD_FONT}`
  const m = measureCtx.measureText('Ag')
  const a = m.fontBoundingBoxAscent || size * 0.8
  const d = m.fontBoundingBoxDescent || size * 0.2
  return (lineHeight - (a + d)) / 2 + a
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
      // 多行（v0.76）：tspan 逐行，首行基线仍在 s.y（单行坐标不变）
      appendMultiLineText(
        g,
        s.content,
        pal.paper,
        { x: s.x, y: s.y, fill: color, 'font-size': s.size, 'font-family': BOARD_FONT },
        'first',
      )
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
        appendMultiLineText(
          g,
          v,
          pal.paper,
          {
            x: s.x + (i % s.cols) * s.cw + s.cw / 2,
            y: s.y + Math.floor(i / s.cols) * s.ch + s.ch / 2,
            fill: color,
            'font-size': 14,
            'font-family': BOARD_FONT,
            'text-anchor': 'middle',
            'dominant-baseline': 'central',
          },
          'center',
        )
      })
      break
    }
    case 'node': {
      g.appendChild(el('circle', { cx: s.x, cy: s.y, r: s.r, fill: hexToRgba(color, 0.12), stroke: color, 'stroke-width': sw }))
      appendMultiLineText(
        g,
        s.label,
        pal.paper,
        {
          x: s.x,
          y: s.y,
          fill: color,
          'font-size': 14,
          'font-family': BOARD_FONT,
          'text-anchor': 'middle',
          'dominant-baseline': 'central',
        },
        'center',
      )
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
        // 权重标在边中点沿法线上移 12，白边保证压线可读；多行整块居中（v0.76）
        appendMultiLineText(
          g,
          s.label,
          pal.paper,
          {
            x: ((x1 + x2) / 2 + uy * 12).toFixed(1),
            y: ((y1 + y2) / 2 - ux * 12).toFixed(1),
            fill: pal.ink,
            'font-size': 12,
            'font-family': BOARD_FONT,
            'text-anchor': 'middle',
            'dominant-baseline': 'central',
          },
          'center',
        )
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
      // 多行（v0.76）：块宽=最长行，块高=首行字高+多行行距；首行底=基线（y 锚点不变），
      // 单行时与旧坐标逐像素一致。行高只吃 1 倍字号的教训仍在：包围盒高一旦多算行距，
      // 缩放手柄钉光标 1:1 与字形 1:1 不能同时成立（曾用 1.35 倍行高致文字拖拽脱手）
      const w = textBlockWidth(s.content, s.size)
      return { x: s.x, y: s.y - s.size, w: Math.max(w, s.size), h: s.size + (s.content.split('\n').length - 1) * s.size * TEXT_LINE_H }
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
// 连线(edge)由节点推导，不参与；文字缩放的是字号；表格缩放的是格子尺寸；
// 直线/箭头=手柄所在端点直接跟手（零乘性，杜绝退化轴放大乱飞）；
// 笔迹必须传拖拽开始时的原始点集（origPoints）：映射若用「当前已缩放点」相对原始
// 包围盒重算，每个 pointermove 复合乘一次系数（6.3^14≈天文数字，实测一拖爆到 3.8e9）
export function resizeShape(s: Shape, from: BBox, to: BBox, origPoints?: [number, number][]): void {
  const sx = from.w > 0.5 ? to.w / from.w : 1
  const sy = from.h > 0.5 ? to.h / from.h : 1
  const rx = (x: number) => to.x + (x - from.x) * sx
  const ry = (y: number) => to.y + (y - from.y) * sy
  switch (s.type) {
    case 'stroke': {
      const pts = origPoints ?? s.points
      const mapped = pts.map((p) => {
        if (from.w < 8 || from.h < 8) {
          const fromDiag = Math.hypot(from.w, from.h) || 1
          const k = Math.hypot(to.w, to.h) / fromDiag
          return [to.x + (p[0] - from.x) * k, to.y + (p[1] - from.y) * k] as [number, number]
        }
        return [rx(p[0]), ry(p[1])] as [number, number]
      })
      s.points = mapped
      break
    }
    case 'line':
    case 'arrow': {
      // 手柄锚在包围盒右下角=距角最近的端点；拖拽即把该端点移到光标处（另一端不动）
      const cx = from.x + from.w
      const cy = from.y + from.h
      const d1 = Math.hypot(s.x1 - cx, s.y1 - cy)
      const d2 = Math.hypot(s.x2 - cx, s.y2 - cy)
      const nx = from.x + to.w
      const ny = from.y + to.h
      if (d2 <= d1) {
        s.x2 = nx
        s.y2 = ny
      } else {
        s.x1 = nx
        s.y1 = ny
      }
      break
    }
    case 'rect':
    case 'ellipse':
      s.x = to.x
      s.y = to.y
      s.w = to.w
      s.h = to.h
      break
    case 'text': {
      // 包围盒高=字号（见 bboxOf），to.h 即光标相对原盒顶的位移：字号直接取 to.h，
      // 手柄（盒底=基线）精确钉在光标下，字形同速 1:1，拖多少长多少
      const size = Math.max(10, Math.min(72, Math.round(to.h)))
      s.x = to.x
      s.y = to.y + size // 基线锚在新包围盒底（bbox.y 即基线减字号）
      s.size = size
      break
    }
    case 'table':
      s.cw = Math.max(20, Math.min(220, to.w / s.cols))
      s.ch = Math.max(16, Math.min(180, to.h / s.rows))
      break
    case 'node': {
      // 圆心固定不滑移，半径随光标位移线性增长（直径增量=光标位移，边缘跟手）
      const cx = from.x + from.w / 2
      const cy = from.y + from.h / 2
      s.r = Math.max(8, Math.min(80, from.w / 2 + (Math.min(to.w, to.h) - Math.min(from.w, from.h)) / 2))
      s.x = cx
      s.y = cy
      break
    }
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
  shapes: Shape[]
}

// —— patch 历史（v0.64 病根③收口）：undo/redo 栈存 immer 正/逆 patch（体积
// O(变更)），取代整文档 JSON 快照 ×60（体积 O(文档)，数百笔后单快照 MB 级 =
// 数十 MB 字符串常驻 + 每次落笔同步 stringify 停顿）。栈容量 = 条数上限 ×
// 字节预算双闸（内存有界）；无实际变更的操作自动不入史。snapshot/load 保留
// 用于存档与跨画布持久化，不再参与历史。
interface HistoryEntry {
  undo: Patch[]
  redo: Patch[]
  bytes: number
}

// 条数上限（小操作贴着它走）；字节预算管大笔迹条目——超预算丢最旧，撤销
// 深度换内存有界（旧口径 60 份全量快照最坏数十 MB，这里 1.5MB 封顶）
const HISTORY_MAX = 60
const HISTORY_BYTE_BUDGET = 1.5 * 1024 * 1024

/** 平移一批 shape（纯函数）：draft 与现行数组通用（move 手势收尾重放用）。 */
export function translateIn(shapes: Shape[], ids: Set<number>, dx: number, dy: number): void {
  for (const s of shapes) {
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

export class BoardModel {
  shapes: Shape[] = []
  idSeq = 1
  private undoStack: HistoryEntry[] = []
  private redoStack: HistoryEntry[] = []
  private historyBytes = 0

  // —— 历史三入口 ——

  /** 当前文档的不可变视角（历史与存档共用形状）。 */
  private doc(): DocV1 {
    return { v: 1, idSeq: this.idSeq, shapes: this.shapes }
  }

  private adopt(d: DocV1): void {
    this.shapes = d.shapes
    this.invalidateIndexes()
    // id 单调递增不复用：撤销「建图形」后重建不与旧 DOM/引用撞号
    this.idSeq = Math.max(this.idSeq, d.idSeq)
  }

  private pushHistory(undoP: Patch[], redoP: Patch[]): void {
    if (!redoP.length) return // 无实际变更不入史（空整理/误触自动无害化）
    const bytes = JSON.stringify(undoP).length + JSON.stringify(redoP).length
    this.undoStack.push({ undo: undoP, redo: redoP, bytes })
    this.historyBytes += bytes
    this.redoStack = [] // 新变更截断重做分支（与快照时代同语义）
    while (
      this.undoStack.length > HISTORY_MAX ||
      (this.historyBytes > HISTORY_BYTE_BUDGET && this.undoStack.length > 1)
    ) {
      this.historyBytes -= this.undoStack.shift()!.bytes
    }
  }

  /** 原子变更唯一入口：在 draft 上直接改（现有改法照搬进回调），正/逆 patch 自动入史。 */
  mutate(fn: (d: DocV1) => void): void {
    const [next, redoP, undoP] = produceWithPatches(this.doc(), fn)
    this.adopt(next)
    this.pushHistory(undoP, redoP)
  }

  /**
   * 手势收尾入口：拖动/缩放/擦除期间为实时视觉反馈已直接改过 model，结束时以
   * 「手势前基线」为 base 重放终态（fn 为确定性变换），把 基线→终态 固化为一条
   * patch——手势期间不入史、一个手势一条历史。
   */
  mutateFrom(base: DocV1, fn: (d: DocV1) => void): void {
    const [next, redoP, undoP] = produceWithPatches(base, fn)
    this.adopt(next)
    this.pushHistory(undoP, redoP)
  }

  /** 手势基线：给定 shape 集合换回手势前拷贝，其余换引用拷贝（move/resize/文字编辑用）。
   * 空 origs 也必须返回数组拷贝而非 this.doc() 活引用：文字编辑实时同步会在手势期
   * push 进 this.shapes，别名基线会被污染，收尾重放即出重影（v0.76 实测修复）。 */
  docWithOverrides(origs: Shape[]): DocV1 {
    const m = new Map(origs.map((o) => [o.id, o]))
    return { v: 1, idSeq: this.idSeq, shapes: this.shapes.map((s) => m.get(s.id) ?? s) }
  }

  /** 手势基线：被删 shape 按删除逆序插回各自记录位置（erase 用；批内记录须按
   * index 升序 push——倒序 splice 即精确复原：每次插入撤销最近一次删除）。 */
  docWithShapesReinstated(origs: { shape: Shape; index: number }[]): DocV1 {
    const shapes = [...this.shapes]
    for (let i = origs.length - 1; i >= 0; i--) shapes.splice(origs[i].index, 0, origs[i].shape)
    return { v: 1, idSeq: this.idSeq, shapes }
  }

  resetHistory(): void {
    this.undoStack = []
    this.redoStack = []
    this.historyBytes = 0
  }

  // 节点标签即时派生 = 当前画布最大节点标签 + 1（非数字标签按 0 计）：
  // 清空画布自动从 1 重新编号，撤销恢复后也不与残留标签冲突（无状态可失步）
  nextLabel(): string {
    let max = 0
    for (const s of this.shapes) {
      if (s.type !== 'node') continue
      const n = Number.parseInt(s.label, 10)
      if (Number.isInteger(n) && n > max) max = n
    }
    return String(max + 1)
  }

  // —— id 索引（v0.65）：get/hitTest/渲染锚点高频路径的 O(1) 查询——数组结构变更
  //（adopt/delete/load）时整表失效重建，坐标变更不动结构、Map 存引用无需失效
  private byId: Map<number, Shape> | null = null
  private nodeIdx: Map<number, NodeShape> | null = null

  private invalidateIndexes(): void {
    this.byId = null
    this.nodeIdx = null
  }

  get(id: number): Shape | undefined {
    if (!this.byId) {
      this.byId = new Map()
      for (const s of this.shapes) this.byId.set(s.id, s)
    }
    return this.byId.get(id)
  }

  // 图形增删一律走 mutate（入史）；delete/insertLive 仅剩手势期即时增删路径使用
  //（erase 手势收尾 mutateFrom 固化；insertLive 供文字编辑实时同步首字建形，
  //BoardView 编辑会话收尾同样 mutateFrom 固化一条历史）
  delete(ids: Set<number>): void {
    this.shapes = this.shapes.filter((s) => !ids.has(s.id))
    this.invalidateIndexes()
  }

  /** 手势期即时插入（不入史）：文字编辑实时同步建形用，id 已由调用方取自 idSeq。 */
  insertLive(s: Shape): void {
    this.shapes.push(s)
    this.invalidateIndexes()
  }

  nodeById(): Map<number, NodeShape> {
    if (!this.nodeIdx) {
      this.nodeIdx = new Map()
      for (const s of this.shapes) if (s.type === 'node') this.nodeIdx.set(s.id, s)
    }
    return this.nodeIdx
  }

  // 与选中节点相连的边（拖动节点时跟随平移的渲染组）
  edgesOf(ids: Set<number>): number[] {
    return this.shapes.filter((s) => s.type === 'edge' && (ids.has(s.from) || ids.has(s.to))).map((s) => s.id)
  }

  translate(ids: Set<number>, dx: number, dy: number): void {
    translateIn(this.shapes, ids, dx, dy)
  }

  // —— 存档序列化（v0.64 起与历史解耦：只服务 localStorage 存档/跨画布持久化） ——
  snapshot(): string {
    const d: DocV1 = { v: 1, idSeq: this.idSeq, shapes: this.shapes }
    return JSON.stringify(d)
  }

  undo(): boolean {
    const e = this.undoStack.pop()
    if (!e) return false
    this.historyBytes -= e.bytes
    this.redoStack.push(e)
    this.adopt(applyPatches(this.doc(), e.undo))
    return true
  }

  redo(): boolean {
    const e = this.redoStack.pop()
    if (!e) return false
    this.undoStack.push(e)
    this.historyBytes += e.bytes
    this.adopt(applyPatches(this.doc(), e.redo))
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
        this.invalidateIndexes()
        this.resetHistory() // 换档历史随档清空（旧档 patch 路径对新档无意义）
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
        // 盒高=字号（底=基线），tol 外扩覆盖下伸部（g/p/y 尾与汉字底缘）保证点选/双击命中
        return (
          !!bb && x >= bb.x - tol && x <= bb.x + bb.w + tol && y >= bb.y - tol && y <= bb.y + bb.h + tol
        )
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

  // 坐标重定基：内容离原点超过 REBASE_LIMIT 时整体平移回原点附近，返回实际位移供调用方
  // 同步移动相机（内容与相机同移则画面零跳变）；空画布 / 量级安全返回 null。
  // 根因背景：历次几何 bug 曾把画稿坐标滚到 1e11 并随 localStorage 复活，Skia float32
  // 在该量级无法光栅化几何（画成全空），呈「事件正常、笔画落库、屏幕全白」的失灵态
  rebaseIfNeeded(limit: number = REBASE_LIMIT): { dx: number; dy: number } | null {
    const bb = this.contentBBox()
    if (!bb) return null
    const far = Math.max(Math.abs(bb.x), Math.abs(bb.y), Math.abs(bb.x + bb.w), Math.abs(bb.y + bb.h))
    if (far <= limit) return null
    const dx = -bb.x
    const dy = -bb.y
    this.translate(new Set(this.shapes.map((s) => s.id)), dx, dy)
    return { dx, dy }
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

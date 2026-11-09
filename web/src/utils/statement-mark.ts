// 题面选中高亮标注（v0.84，纯前端不持久化）：用户在题面渲染 DOM（.md-view-body）
// 选中文字后由工具条（MdMarkBar）下发颜色，这里负责把选区包成 <mark class="md-hl">
// 荧光标注 / 换色 / 擦除。标注只活在当前 DOM 里——切题重渲染（innerHTML 重写）与
// 刷新即失，符合「草稿式标注」定位。语义（2026-11-09 二次拍板）：只动选中的文字——
// 整段被选整段处理，部分命中拆段处理、未选中部分保留原色。公式（KaTeX）与普通
// 文字同一套拆段逻辑：标注落在 .katex-html 的字形文本节点上（按选中粒度局部标
// 色，所见即所得），KaTeX 排版骨架（strut/vlist 等 span）不动；隐藏 MathML 副本
// （.katex-mathml）为守卫区不碰。其余守卫区：button（复制按钮）、.md-io-head
// （样例格头 UI 文案）。

export type MarkSlot = 'ink' | 'blue' | 'teal' | 'warning' | 'danger'

/** 五色槽：与画板 BoardView colorSlots 同源同名同序（2026-11-09 用户拍板） */
export const MARK_SLOTS: { slot: MarkSlot; name: string }[] = [
  { slot: 'ink', name: '墨黑' },
  { slot: 'blue', name: '主蓝' },
  { slot: 'teal', name: '青绿' },
  { slot: 'warning', name: '橙' },
  { slot: 'danger', name: '红' },
]

const SLOT_CLASS_RE = /md-hl-(ink|blue|teal|warning|danger)/
const GUARD_SELECTOR = '.katex-mathml, button, .md-io-head'

function slotOf(mark: Element): MarkSlot | null {
  return (mark.className.match(SLOT_CLASS_RE)?.[1] as MarkSlot | undefined) ?? null
}

/** 不可标注的守卫区（公式渲染/按钮/样例格头） */
function guarded(node: Node): boolean {
  return !!node.parentElement?.closest(GUARD_SELECTOR)
}

/** 当前选区可标注时返回它（非折叠且首尾都在 root 内），否则 null */
export function selectableRange(root: HTMLElement): Range | null {
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return null
  const range = sel.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null
  return range
}

/** 把整个 text 节点包进 <mark> */
function wrapWhole(node: Text, slot: MarkSlot): void {
  const mark = document.createElement('mark')
  mark.className = `md-hl md-hl-${slot}`
  node.before(mark)
  mark.appendChild(node)
}

/** 把 node 的 [start, end) 段包进 <mark>（先拆尾段再拆头段，mid 恒为命中段） */
function wrapSlice(node: Text, start: number, end: number, slot: MarkSlot): void {
  if (end < node.length) node.splitText(end) // 尾段 [end..) 拆走留在原位
  let mid = node // start=0 时命中段就是拆完尾段后的头部原节点
  if (start > 0) mid = node.splitText(start) // start>0 再拆，splitText 返回段即 [start..end)
  wrapWhole(mid, slot)
}

/** 收集 range 命中的文本节点及其命中区间 [start, end)（有界：只走 root 子树）；
    KaTeX 字形文本节点与普通文本同权重——按选中粒度拆段实现公式局部标色 */
function collectNodes(root: HTMLElement, range: Range): { node: Text; start: number; end: number }[] {
  const out: { node: Text; start: number; end: number }[] = []
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  let cur = walker.nextNode()
  while (cur) {
    const node = cur as Text
    cur = walker.nextNode()
    if (!node.data.trim() || guarded(node)) continue
    if (!range.intersectsNode(node)) continue
    const start = node === range.startContainer ? range.startOffset : 0
    const end = node === range.endContainer ? range.endOffset : node.length
    if (start < end) out.push({ node, start, end })
  }
  return out
}

/** 给选区加标注：逐文本节点在 range 边界处拆分包 <mark>，跨元素选区天然支持 */
export function applyMark(root: HTMLElement, range: Range, slot: MarkSlot): void {
  // 先收集完再动 DOM，避开 live 遍历失效
  collectNodes(root, range).forEach(({ node, start, end }) => wrapSlice(node, start, end, slot))
}

/** 选区两端锚点（持久文本节点 + 偏移），随 splitText 同步调整，见 splitAnchored */
type Anchor = { node: Text; offset: number }

/** node.splitText(k)：锚点落在拆点右侧时挪到尾段，保证拆分后锚点仍指同一字符位 */
function splitAnchored(anchors: (Anchor | null)[], node: Text, k: number): Text {
  const tail = node.splitText(k)
  anchors.forEach((a) => {
    if (a && a.node === node && a.offset > k) {
      a.node = tail
      a.offset -= k
    }
  })
  return tail
}

/**
 * 部分拆段擦除：选区触碰的标注逐个解除——整段被选整段解除；部分命中解除后
 * 未选中部分按原色重包。先按 pre-mutation 选区边界逐 mark 算好命中区间再动
 * DOM（各 mark 文本互不重叠，逐个拆分互不干扰）；anchors 同步跟随拆分，
 * 抹完后仍精确指向选区首尾字符位。
 */
function eraseSplitting(root: HTMLElement, range: Range, anchors: (Anchor | null)[]): void {
  const marks: Element[] = []
  root.querySelectorAll('mark.md-hl').forEach((m) => {
    if (range.intersectsNode(m)) marks.push(m)
  })
  marks.forEach((mark) => {
    const slot = slotOf(mark)
    const first = mark.firstChild
    const isText = !!first && first.nodeType === Node.TEXT_NODE
    // 公式原子段（子节点是 .language-math）：不拆分，整段解除即可（换色由
    // applyMark 的公式单元按新色整段重建）；isText=false 走下方 early-return
    // 命中区间在该 mark 文本内的 [s, e)；整段包含时 s=0、e=len（pre-mutation 边界）
    let s = 0
    let e = isText ? (first as Text).length : 0
    if (isText && first === range.startContainer) s = range.startOffset
    if (isText && first === range.endContainer) e = range.endOffset
    // 整段包含判定：range 首尾都在 mark 两端之外
    const probe = document.createRange()
    if (isText) probe.selectNodeContents(first)
    else probe.selectNode(mark)
    const fully =
      range.compareBoundaryPoints(Range.START_TO_START, probe) <= 0 &&
      range.compareBoundaryPoints(Range.END_TO_END, probe) >= 0
    // 解除包覆（children 原位落回，文本节点引用不变）
    mark.replaceWith(...Array.from(mark.childNodes))
    if (fully || !slot || !isText) return
    // 部分命中：先拆尾段再拆头段，未选中部分按原色重包，中段 [s..e) 留裸文本
    const t = first as Text
    if (e < t.length) wrapWhole(splitAnchored(anchors, t, e), slot)
    if (s > 0) {
      splitAnchored(anchors, t, s) // 中段 [s..e) 拆出留在原位
      wrapWhole(t, slot) // 头部 [0..s)
    }
  })
}

/** 把 Range 边界锚定到持久文本节点：文本容器原样返回；元素容器（选区落在元素
    边缘）按方向取文档序最近文本节点（起点取其后首个、终点取其前末个）。解包
    只挪文本节点的父链、不灭节点，锚定在擦除前后都精确 */
function textAnchor(
  root: HTMLElement,
  container: Node,
  offset: number,
  forward: boolean,
): Anchor | null {
  if (container.nodeType === Node.TEXT_NODE) return { node: container as Text, offset }
  const probe = document.createRange()
  probe.setStart(container, offset)
  probe.collapse(true)
  let found: Text | null = null
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  let cur = walker.nextNode() as Text | null
  while (cur) {
    // comparePoint：-1 在边界前 / 0 重合 / 1 在边界后
    const pos = probe.comparePoint(cur, 0)
    if (forward && pos >= 0) return { node: cur, offset: 0 }
    if (!forward && pos <= 0) found = cur
    cur = walker.nextNode() as Text | null
  }
  return found ? { node: found, offset: found.length } : null
}

/**
 * 换色：只把选中文字换成新色，未选中部分保留原色（部分命中拆段语义）。流程 =
 * 锚定两端 → 部分拆段擦除（旧色未选段原样保留）→ 用调整后的锚点重建精确
 * Range 再包新色。之所以不直接续用 live range：解包标注会把其文本边界塌成元素
 * 边界（父链移除点），按节点收集会漏掉 range 尾段（2026-11-09 真机实锤）。
 */
export function recolorRange(root: HTMLElement, range: Range, slot: MarkSlot): void {
  const startA = textAnchor(root, range.startContainer, range.startOffset, true)
  const endA = textAnchor(root, range.endContainer, range.endOffset, false)
  if (!startA || !endA) return
  const anchors: (Anchor | null)[] = [startA, endA]
  eraseSplitting(root, range, anchors)
  const fresh = document.createRange()
  fresh.setStart(startA.node, Math.min(startA.offset, startA.node.length))
  fresh.setEnd(endA.node, Math.min(endA.offset, endA.node.length))
  applyMark(root, fresh, slot)
}

/** 擦除：只擦选中文字的颜色，未选中部分保留原色（部分命中拆段语义） */
export function eraseRange(root: HTMLElement, range: Range): void {
  const startA = textAnchor(root, range.startContainer, range.startOffset, true)
  const endA = textAnchor(root, range.endContainer, range.endOffset, false)
  eraseSplitting(root, range, [startA, endA])
}

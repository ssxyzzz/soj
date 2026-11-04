// FitTags 量宽调度器（v0.62 挂载冻结根治批）：把「每实例各自 measure + 各自
// ResizeObserver + fonts.ready 各自二波」收编为全站单波批调度——同一 rAF 帧内先
// 逐实例读宽计算、再统一提交（读写分离，避免与 Vue DOM 更新交错产生多轮强制布局）；
// ResizeObserver 全站共享一个；fonts.ready 只补一波。病理背景：题库列表 50 行挂载期
// 多波交错测量 + 每 el-tooltip 实例脚手架是刷新卡的实测根源之一（详见 plan §2.1）。
export interface MeasureSink {
  /** 只读阶段：读 DOM 量宽，算出目标值存进实例内部，不写任何响应式状态 */
  read(): void
  /** 写阶段：把 read 算出的目标值提交到响应式状态（触发 DOM 更新） */
  write(): void
}

const live = new Set<MeasureSink>()
const pending = new Set<MeasureSink>()
const byEl = new Map<HTMLElement, MeasureSink>()
let rafId = 0

function flush() {
  rafId = 0
  const batch = Array.from(pending)
  pending.clear()
  for (const s of batch) s.read()
  for (const s of batch) s.write()
}

// 归并到下一帧：同一实例重复调度天然去重，同帧多实例合并成一波
export function scheduleMeasure(sink: MeasureSink) {
  pending.add(sink)
  if (!rafId) rafId = requestAnimationFrame(flush)
}

// 共享 ResizeObserver：同帧多 entry 也只归并出一次批调度
const ro =
  typeof ResizeObserver !== 'undefined'
    ? new ResizeObserver((entries) => {
        for (const e of entries) {
          const s = byEl.get(e.target as HTMLElement)
          if (s) scheduleMeasure(s)
        }
      })
    : undefined

export function registerSink(el: HTMLElement, sink: MeasureSink) {
  live.add(sink)
  byEl.set(el, sink)
  ro?.observe(el)
  scheduleMeasure(sink) // 挂载即测（与其他实例合并到同一波）
}

export function unregisterSink(el: HTMLElement, sink: MeasureSink) {
  live.delete(sink)
  pending.delete(sink)
  byEl.delete(el)
  ro?.unobserve(el)
}

// 字体就绪补一波（字体换宽是真实变化；全站只此一波，替代每实例各自监听 fonts.ready）
if (typeof document !== 'undefined' && document.fonts) {
  document.fonts.ready.then(() => {
    for (const s of live) scheduleMeasure(s)
  })
}

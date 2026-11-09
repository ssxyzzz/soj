// 题面选中标注工具条状态（v0.84）：document selectionchange（rAF 去抖）判定选区
// 非折叠且首尾都在题面容器内 → 快照 live Range 并把工具条 fixed 定位在选区上方
// （上方放不下翻到下方、水平 clamp 出视口，UserSuggestTextarea 弹层同款翻转范式）；
// 滚动/缩放实时跟位。点色 = 只换选中文字的颜色（部分拆段语义，recolorRange）；
// 橡皮擦 = 只擦选中文字的颜色（eraseRange）；应用/擦除后 removeAllRanges，
// selectionchange 收敛即自动隐藏。监听全走 useListener（声明即绑定、作用域卸载
// 自动摘除）。
// 时序（2026-11-09 真机实锤修复）：工具条只在松手后出现——拖选进行中始终隐藏。
// 此前选区变化即弹出，反向（上/左）拖选的路径恰好穿过悬在选区上方的工具条，
// 命中测试把选区终点甩到 body 尾部（页脚备案号/SW 提示条）→ 甩出拉回狂闪、
// 连带右栏整片全选。mousedown（工具条自身除外）即收起，mouseup/touchend 松手
// 按最终选区重新收敛。
import { reactive, type Ref } from 'vue'

import { useListener } from '@/composables/useWindowListener'
import { eraseRange, recolorRange, selectableRange, type MarkSlot } from '@/utils/statement-mark'

/** 工具条占位尺寸（与 MdMarkBar 实渲染尺寸一致，供翻转/居中估算） */
const BAR_W = 184
const BAR_H = 36

export function useStatementMark(root: Ref<HTMLElement | undefined>) {
  const state = reactive<{ visible: boolean; style: Record<string, string> }>({
    visible: false,
    style: {},
  })

  let range: Range | null = null
  let raf = 0

  function place() {
    if (!range) return
    const rect = range.getBoundingClientRect()
    if (!rect.width && !rect.height) return hide()
    // 默认选区上方居中；上方放不下且下方有空间翻到下方
    const above = rect.top >= BAR_H + 6
    const top = above ? rect.top - BAR_H - 6 : rect.bottom + 6
    const left = Math.min(
      Math.max(rect.left + rect.width / 2 - BAR_W / 2, 8),
      window.innerWidth - BAR_W - 8,
    )
    state.style = { left: `${Math.round(left)}px`, top: `${Math.round(top)}px` }
  }

  function hide() {
    state.visible = false
    range = null
    state.style = {}
  }

  function sync() {
    // 拖选进行中不出工具条（防反向拖选路径穿过工具条被命中测试甩飞选区）
    if (dragging) return hide()
    const r = root.value ? selectableRange(root.value) : null
    if (!r) return hide()
    range = r
    state.visible = true
    place()
  }

  function onSelectionChange() {
    if (raf) return
    raf = requestAnimationFrame(() => {
      raf = 0
      sync()
    })
  }

  function apply(slot: MarkSlot) {
    const el = root.value
    if (!el || !range) return
    // 只换选中文字的颜色，未选中部分保留原色（部分拆段语义见 recolorRange）
    recolorRange(el, range, slot)
    window.getSelection()?.removeAllRanges()
    hide()
  }

  function erase() {
    const el = root.value
    if (!el || !range) return
    eraseRange(el, range)
    window.getSelection()?.removeAllRanges()
    hide()
  }

  // 拖选进行中标记（主键按下即收工具条、松手重新收敛）；工具条自身的按下除外
  let dragging = false
  function onDown(e: Event) {
    if ((e as MouseEvent).button !== undefined && (e as MouseEvent).button !== 0) return
    if ((e.target as Element | null)?.closest?.('.md-mark-bar')) return
    dragging = true
    hide()
  }
  function onUp() {
    if (!dragging) return
    dragging = false
    sync()
  }
  useListener(document, 'mousedown', onDown, true)
  useListener(document, 'mouseup', onUp, true)
  useListener(document, 'touchstart', onDown, true)
  useListener(document, 'touchend', onUp, true)
  useListener(window, 'blur', () => {
    dragging = false
  })

  useListener(document, 'selectionchange', onSelectionChange)
  useListener(window, 'scroll', place, true) // capture：容器内滚动也跟位
  useListener(window, 'resize', place)

  return { state, apply, erase, hide }
}

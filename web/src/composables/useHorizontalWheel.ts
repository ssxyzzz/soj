// 横向滚轮接管（v0.64 病根①收口）：全站唯一实现——TagChips 与 TagsPanel 此前
// 两套手写（其一无条件吞掉纵向滚轮，成了「页面滚不动」的交互死区）归一于此。
// 口径（2026-11-04 拍板，取代 2026-10-23 的全吞口径）：容器有横向溢出才接管——
// deltaX 优先（触摸板横扫）、竖向增量映射横滚，滚到头不再放行（鼠标停在本行
// 即浏览本行）；没有横向溢出不拦截，滚轮交给页面正常滚动。
function scrollByWheel(el: HTMLElement, e: WheelEvent): void {
  const max = el.scrollWidth - el.clientWidth
  if (max <= 0) return
  e.preventDefault()
  const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
  if (delta === 0) return
  el.scrollLeft = Math.max(0, Math.min(max, el.scrollLeft + delta))
}

/** 行元素自身接管：挂在可横滚行上（@wheel="onRowWheel"），e.currentTarget 即行 */
export function onRowWheel(e: WheelEvent): void {
  scrollByWheel(e.currentTarget as HTMLElement, e)
}

/** 容器路由版：挂在多行画布层上（@wheel="onRowsWheel"），增量路由到指针所在行 */
export function onRowsWheel(e: WheelEvent): void {
  const row = (e.target as HTMLElement).closest<HTMLElement>('.tag-row')
  if (row) scrollByWheel(row, e)
}

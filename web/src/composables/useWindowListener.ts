import { getCurrentScope, onScopeDispose } from 'vue'

/**
 * window/document 级监听唯一入口（v0.64 病根②收口，useEventListener 模式）：
 * 声明即绑定、返回幂等 stop——作用域（组件 setup / effectScope）卸载时自动摘除，
 * 让泄漏「写不出来」。此前手写 add/remove 配对靠人肉记忆：TagsPanel 长按的单击
 * 路径漏摘一组 window pointermove/pointerup，每点一次永久泄漏一组，越用越卡。
 *
 * 两种用法：
 * - 常驻监听：setup 里调用，随组件卸载自动摘（如 UserSuggestTextarea 弹层期）。
 * - 手势/瞬时监听：回调里调用拿 stop，手势结束时 stop()；即使忘了调，作用域
 *   卸载兜底摘除（如 TagsPanel 长按拖拽、MdEditorInner 高度拖拽）。
 */
export function useListener<E extends Event = Event>(
  target: Window | Document | HTMLElement,
  type: string,
  listener: (ev: E) => void,
  options?: boolean | AddEventListenerOptions,
): () => void {
  const bound = listener as EventListener
  target.addEventListener(type, bound, options)
  let disposed = false
  const stop = () => {
    if (disposed) return
    disposed = true
    target.removeEventListener(type, bound, options)
  }
  if (getCurrentScope()) onScopeDispose(stop)
  return stop
}

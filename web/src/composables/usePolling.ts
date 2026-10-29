import { onScopeDispose } from 'vue'

/**
 * 轮询循环（v0.29 横切收敛）：收敛全站 10+ 处手写 setInterval。
 * 三件套行为统一：后台标签页自动暂停（visibilitychange，回前台立即补一拍）、
 * 组件/作用域卸载自动停、fn 返回 false 主动终态停轮。
 * 业务失败不中断轮询（错误提示由 api 拦截器负责），网络层失败通常是暂态。
 */
export function usePolling(
  fn: () => boolean | void | Promise<boolean | void>,
  intervalMs: number,
  opts?: { immediate?: boolean },
) {
  let timer: ReturnType<typeof setTimeout> | null = null
  let stopped = false
  let running = false

  async function tick() {
    if (stopped || running || document.hidden) return
    running = true
    let keep = true
    try {
      keep = (await fn()) !== false
    } catch {
      keep = true
    }
    running = false
    if (!stopped && keep) schedule()
  }

  function schedule() {
    timer = setTimeout(tick, intervalMs)
  }

  function start() {
    if (stopped || timer != null) return
    if (opts?.immediate) void tick()
    else schedule()
  }

  function stop() {
    stopped = true
    if (timer != null) clearTimeout(timer)
    timer = null
  }

  function onVisible() {
    if (!document.hidden && !stopped && timer == null && !running) void tick()
  }
  document.addEventListener('visibilitychange', onVisible)
  onScopeDispose(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisible)
  })

  return { start, stop }
}

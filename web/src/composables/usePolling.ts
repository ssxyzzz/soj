import { onScopeDispose } from 'vue'

/**
 * 轮询循环（v0.29 横切收敛）：收敛全站 10+ 处手写 setInterval。
 * 三件套行为统一：后台标签页自动暂停（visibilitychange，回前台立即补一拍）、
 * 组件/作用域卸载自动停、fn 返回 false 主动终态停轮。
 * 业务失败不中断轮询（错误提示由 api 拦截器负责），网络层失败通常是暂态。
 *
 * v0.37 加 maxDurationMs：等待型轮询的上限——后端卡死（判题队列僵死、任务状态
 * 永远 running）时不再无限转圈，到限自停并回调 onExpire（终态提示由调用方给）。
 * 默认 0 = 不限（通知徽标等常驻轮询不受影响）。
 */
export function usePolling(
  fn: () => boolean | void | Promise<boolean | void>,
  intervalMs: number,
  opts?: { immediate?: boolean; maxDurationMs?: number; onExpire?: () => void },
) {
  let timer: ReturnType<typeof setTimeout> | null = null
  let stopped = false
  let running = false
  let startedAt = 0

  function expired(): boolean {
    return !!opts?.maxDurationMs && Date.now() - startedAt >= opts.maxDurationMs
  }

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
    if (expired()) {
      stop()
      opts?.onExpire?.()
      return
    }
    timer = setTimeout(tick, intervalMs)
  }

  function start() {
    if (stopped || timer != null) return
    startedAt = Date.now()
    if (opts?.immediate) void tick()
    else schedule()
  }

  /** 重新起轮（v0.37）：stop 后的实例可复用——提交后每轮判题等待都需要重置计时 */
  function restart() {
    stop()
    stopped = false
    start()
  }

  function stop() {
    stopped = true
    if (timer != null) clearTimeout(timer)
    timer = null
  }

  function onVisible() {
    if (!document.hidden && !stopped && timer == null && !running && !expired()) void tick()
  }
  document.addEventListener('visibilitychange', onVisible)
  onScopeDispose(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisible)
  })

  return { start, stop, restart }
}

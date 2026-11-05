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
 *
 * v0.71 加 background:true（生产实锤）：后台标签页的主线程定时器被 Chrome 钳到
 * 约 1 次/分钟（intensive throttling，隐藏约 5 分钟后生效）——整理类任务进度轮询
 * 在用户切走后变成 1 分钟一拍，进度条看似卡死（服务端 aiCalls 实测批次 0-10s/批
 * 全程不慢，纯显示层问题）。开启后：①定时器搬进 Web Worker（Worker 计时不受
 * 后台标签节流），②tick 不再因 document.hidden 熄火——切走后仍按真实间隔拉进度，
 * 回来即最新值。仅任务进度类轮询开启；默认行为不变（后台暂停，省服务端）。
 * blob: worker 被 CSP 拦或环境不支持时自动回退主线程定时器（退化回节流，不报错）。
 */

// ---- 共享轮询 worker（模块级单例：全站 background 轮询共用一条连接开销） ----
let pollWorker: Worker | null = null
const workerTicks = new Map<number, () => void>()
let workerSeq = 0

function ensurePollWorker(): Worker | null {
  if (pollWorker) return pollWorker
  if (typeof window === 'undefined' || typeof Worker === 'undefined') return null
  try {
    // 每个 key 一个 interval；到点 postMessage(key)，经 Map 派发回所属实例的 tick
    const src =
      'let t={};onmessage=e=>{const d=e.data;' +
      "if(d.act==='start'){clearInterval(t[d.key]);t[d.key]=setInterval(()=>postMessage(d.key),d.ms)}" +
      "else{clearInterval(t[d.key]);delete t[d.key]}}"
    pollWorker = new Worker(URL.createObjectURL(new Blob([src], { type: 'text/javascript' })))
    pollWorker.onmessage = (e) => workerTicks.get(Number(e.data))?.()
  } catch {
    pollWorker = null
  }
  return pollWorker
}

export function usePolling(
  fn: () => boolean | void | Promise<boolean | void>,
  intervalMs: number,
  opts?: { immediate?: boolean; maxDurationMs?: number; onExpire?: () => void; background?: boolean },
) {
  let timer: ReturnType<typeof setTimeout> | null = null
  let stopped = false
  let running = false
  let startedAt = 0
  const key = ++workerSeq

  function expired(): boolean {
    return !!opts?.maxDurationMs && Date.now() - startedAt >= opts.maxDurationMs
  }

  async function tick() {
    if (stopped || running) return
    if (document.hidden && !opts?.background) return
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
    const w = opts?.background ? ensurePollWorker() : null
    if (w) {
      workerTicks.set(key, tick)
      w.postMessage({ act: 'start', key, ms: intervalMs })
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
    if (pollWorker) {
      pollWorker.postMessage({ act: 'stop', key })
      workerTicks.delete(key)
    }
  }

  function onVisible() {
    // background 模式下 worker 定时器持续在拍，回前台无需补拍（timer 恒 null 也无妨，
    // 多补一拍有 running 防重入兜底）
    if (!document.hidden && !stopped && timer == null && !running && !expired()) void tick()
  }
  document.addEventListener('visibilitychange', onVisible)
  onScopeDispose(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisible)
  })

  return { start, stop, restart }
}

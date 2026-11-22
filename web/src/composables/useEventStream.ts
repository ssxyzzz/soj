import { onScopeDispose, ref, watch } from 'vue'
import { useUserStore } from '@/stores/user'

/**
 * SSE 事件流（v0.98 全站后端推送）：fetch 流式读取 /api/events——原生
 * EventSource 不能带 Authorization 头（token 走 URL 有泄露面），自写帧解析
 * （跨 chunk 断行安全）；连接整会话单例（MainLayout 的 notify 订阅常驻）。
 *
 * 重连纪律：指数退避 1/2/4/8s 封顶、成功归零；服务端 event: bye（8min 寿命轮转）
 * 与 visibility 回前台立即重连；401 停止（登出流程接管，不抢戏）。
 * X-Renewed-Token 换存守卫与 axios 拦截器同款（仅当连接时 token 仍是当前会话票）。
 *
 * 轮询降级契约：pushActive=false 时消费方回退 usePolling 原间隔——最坏情况
 * 等价推送前行为，轮询代码不删。
 */

// ---- 模块级单例状态 ----
export const pushActive = ref(false)

type TopicHandler = (data: Record<string, unknown>) => void

const handlers = new Map<string, Set<TopicHandler>>()
let connecting = false
let abort: AbortController | null = null
let retryTimer: ReturnType<typeof setTimeout> | null = null
let backoff = 1000
let closedByUs = false

function dispatch(topic: string, data: Record<string, unknown>) {
  handlers.get(topic)?.forEach((h) => {
    try {
      h(data)
    } catch {
      // 消费方异常不断流（与 usePolling 的业务失败不中断轮询同口径）
    }
  })
}

/** 解析 SSE 帧：buf 跨 chunk 累积，按 \n\n 切帧，返回剩余半帧。可测纯函数。 */
export function parseSSEChunk(
  buf: string,
  onEvent: (event: string, data: string) => void,
): string {
  buf = buf.replace(/\r\n/g, '\n') // SSE 规范行尾 \r\n/\n 双容忍（防中间链改写）
  let start = 0
  for (;;) {
    const end = buf.indexOf('\n\n', start)
    if (end < 0) break
    let event = ''
    const dataLines: string[] = []
    for (const line of buf.slice(start, end).split('\n')) {
      if (line.startsWith(':')) continue // 注释帧（心跳 :ping）
      if (line.startsWith('event:')) event = line.slice(6).trim()
      else if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart())
    }
    if (dataLines.length) onEvent(event || 'message', dataLines.join('\n'))
    start = end + 2
  }
  return buf.slice(start)
}

async function connect() {
  const token = useUserStore().token
  if (!token || connecting) return
  connecting = true
  closedByUs = false
  abort = new AbortController()
  try {
    const resp = await fetch('/api/events', {
      headers: { Authorization: `Bearer ${token}` },
      signal: abort.signal,
      // SSE 长连接：不缓存（中间件/代理链禁缓冲由响应头声明，这里双保险）
      cache: 'no-store',
    })
    if (resp.status === 401) {
      stop() // 登录态失效：停止重连，登出流程接管
      return
    }
    if (!resp.ok || !resp.body) throw new Error(`SSE ${resp.status}`)
    // 滑动续期：连接建立响应头换票（守卫口径与 axios 拦截器一致——仅当连接时
    // 所带 token 仍是当前会话票才换存，登出/换号后迟到的续期票不回写）
    const store = useUserStore()
    const renewed = resp.headers.get('x-renewed-token')
    if (renewed && token === store.token) {
      store.renewToken(renewed)
    }
    pushActive.value = true
    backoff = 1000
    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buf = parseSSEChunk(buf + decoder.decode(value, { stream: true }), (event, data) => {
        if (event === 'bye') {
          closedByUs = true // 服务端寿命轮转：正常关流，立即重连
          return
        }
        if (event === 'resync') {
          dispatch('resync', {})
          return
        }
        if (event === 'hello') return
        try {
          dispatch(event, JSON.parse(data) as Record<string, unknown>)
        } catch {
          // 非 JSON 载荷忽略
        }
      })
      if (closedByUs) break
    }
  } catch {
    // 网络层失败：走退避重连
  } finally {
    connecting = false
    pushActive.value = false
    if (!closedByUs && useUserStore().token) scheduleRetry()
    else if (closedByUs && useUserStore().token) connect()
  }
}

function scheduleRetry() {
  if (retryTimer != null) return
  retryTimer = setTimeout(
    () => {
      retryTimer = null
      if (useUserStore().token) void connect()
    },
    backoff,
  )
  backoff = Math.min(backoff * 2, 8000)
}

function stop() {
  if (retryTimer != null) {
    clearTimeout(retryTimer)
    retryTimer = null
  }
  abort?.abort()
  abort = null
  pushActive.value = false
  backoff = 1000
}

// 生命周期挂载（懒初始化：首个 useTopic 消费方出现时才装——避免模块 import 期
// 触碰 pinia（应用未装 store 直接取会抛错）；登录/登出/换号重建连接 + 回前台
// 立即补连（砍退避等待）。
let lifecycleReady = false

function ensureLifecycle() {
  if (lifecycleReady || typeof window === 'undefined') return
  lifecycleReady = true
  watch(
    () => useUserStore().token,
    (t) => {
      stop()
      if (t) void connect()
    },
  )
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden && !pushActive.value && !connecting && useUserStore().token) {
      if (retryTimer != null) {
        clearTimeout(retryTimer)
        retryTimer = null
      }
      void connect()
    }
  })
}

/**
 * useTopic 订阅一个主题（引用计数：首订阅建连，末订阅保留连接——notify 常驻
 * 使连接整会话在线；组件卸载自动退订）。
 */
export function useTopic(topic: string, handler: TopicHandler) {
  let set = handlers.get(topic)
  if (!set) {
    set = new Set()
    handlers.set(topic, set)
  }
  set.add(handler)
  ensureLifecycle()
  if (useUserStore().token && !pushActive.value && !connecting) void connect()
  onScopeDispose(() => {
    set.delete(handler)
    if (set.size === 0) handlers.delete(topic)
  })
}

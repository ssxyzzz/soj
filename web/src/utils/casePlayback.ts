// 逐点色块回放（v0.99.1）：把真实快照按点速限速成展示快照。
//
// v0.98 逐点进度是数据到达即点亮：快题（10 个点数百毫秒判完）叠加后端 200ms 合批与
// 前端 300ms 节流，色块实际成批跳出，终态一到明细表立刻顶替——「逐个点亮」看不出来。
// 这里给显示推进加下限点速：相邻两步（点亮一枚 / 终态回落）至少间隔 100ms。真实判题
// 慢于点速照真实节奏走，快于点速被限速到 100ms/点（10 点 1 秒走完、单点题 100ms）。
//
// 纯逻辑（时钟/定时器可注入，esbuild 打包直跑断言），Vue 绑定见 useCasePlayback。
import { isQueueStatus } from '@/utils/format'
import type { Submission } from '@/types'

/** 最快点速：相邻两次显示推进（点亮一枚 / 终态回落明细表）的最小间隔 */
export const CASE_STEP_MS = 100

/** settle 有界兜底里留给弹窗取数的余量（正常回放在此之前早已走完） */
const SETTLE_FETCH_SLACK_MS = 2000

export interface PlaybackClock {
  now(): number
  set(cb: () => void, ms: number): unknown
  clear(handle: unknown): void
}

const domClock: PlaybackClock = {
  now: () => performance.now(),
  set: (cb, ms) => setTimeout(cb, ms),
  clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
}

export interface CasePlayback {
  /** 喂最新真实快照；on=false 表示色块条不在屏（弹窗关着/后台标签），直出真实 */
  feed(sub: Submission | null, on?: boolean): void
  /** 记下进度信号带的点数总数（终态快照不带 case_total，先到的信号先把 N 记下） */
  noteTotal(sid: number, total: number): void
  /** 等回放走完：真实终态已如实展示才放行；没在回放立即放行 */
  settle(): Promise<void>
  dispose(): void
}

// 跟踪中的回放登记（sid → 回放器）：完成提示经 settleCasePlayback 对齐回放收尾
const playbacks = new Map<number, Set<CasePlayback>>()

/**
 * 等某提交的色块回放走完（完成提示 / AC 开箱与弹窗同步揭晓，不抢在色块亮完前剧透
 * 终态）：没有弹窗在回放这份提交时立即放行——挑战页、未开弹窗等场景零影响。
 */
export function settleCasePlayback(sid: number): Promise<void> {
  const set = playbacks.get(sid)
  if (!set) return Promise.resolve()
  return Promise.all([...set].map((p) => p.settle())).then(() => undefined)
}

export function createCasePlayback(
  emit: (view: Submission | null) => void,
  clock: PlaybackClock = domClock,
  stepMs = CASE_STEP_MS,
): CasePlayback {
  let id = 0 // 跟踪中的提交；0 = 无
  let cur: Submission | null = null // 最新真实快照
  let watching = false // 本轮是否回放：本轮从队列态跟起才回放；首帧就是终态（直接打开
  // 已判完的提交）不回放
  let active = true // 色块条在屏
  let shown = 0 // 已点亮点数
  let total = 0 // 点数总数（取见过的最大值）
  let nextAt = 0 // 下一次显示推进的最早时刻
  let timer: unknown = null
  let bound: unknown = null // settle 有界兜底
  let settlers: (() => void)[] = []

  // 回放已不扣着任何东西：没在跟判题 / 不在屏 / 真实已终态且没有排定格
  function settled(): boolean {
    return !watching || !active || (timer == null && !!cur && !isQueueStatus(cur.status))
  }

  function resolveAll() {
    if (bound != null) {
      clock.clear(bound)
      bound = null
    }
    const list = settlers
    settlers = []
    list.forEach((done) => done())
  }

  function clearTimer() {
    if (timer != null) {
      clock.clear(timer)
      timer = null
    }
  }

  function arm(wait: number) {
    timer = clock.set(() => {
      timer = null
      pump()
    }, wait)
  }

  // 显示推进泵：按最新真实快照续算，一次调用推进到「下一步需要等」或「追平」为止
  function pump() {
    if (timer != null) return // 已排定下一步：到点按最新快照续算
    for (;;) {
      const s = cur
      if (s && watching && active) {
        const rows = [...(s.detail ?? [])].sort((a, b) => a.index - b.index)
        if (shown > rows.length) shown = rows.length // 真实数据缩水（领取清场/回收重排）：点亮数跟随
        const wait = nextAt - clock.now()
        if (shown < rows.length) {
          // 有点待点亮：到了点速点亮一枚，没到就排定格（合批到达的点不跳格）
          if (wait > 0) return arm(wait)
          shown++
          nextAt = clock.now() + stepMs
          emit({
            ...s,
            status: isQueueStatus(s.status) ? s.status : 'Judging', // 真实已终态也按判题中展示
            stage: 'run',
            case_done: shown,
            case_total: Math.max(total, rows.length),
            detail: rows.slice(0, shown),
          })
          continue
        }
        // 终态回落也是一步：最后一枚点亮后停留一个点速再换明细表
        if (!isQueueStatus(s.status) && shown > 0 && wait > 0) return arm(wait)
      }
      emit(s) // 追平：如实直出真实快照
      if (settlers.length && settled()) resolveAll()
      return
    }
  }

  // 结束当前提交的回放：清定时器、注销登记、放行等待方、进度归零
  function endRun() {
    clearTimer()
    playbacks.get(id)?.delete(api)
    if (playbacks.get(id)?.size === 0) playbacks.delete(id)
    resolveAll()
    shown = 0
    total = 0
    nextAt = 0
  }

  function feed(sub: Submission | null, on = true) {
    active = on
    if (!on) clearTimer() // 不在屏：排着的定格作废，下面直出真实
    if (!sub || sub.id !== id) {
      endRun()
      watching = !!sub && isQueueStatus(sub.status)
      cur = sub
      if (sub) {
        id = sub.id
        const set = playbacks.get(id) ?? new Set<CasePlayback>()
        set.add(api)
        playbacks.set(id, set)
      } else {
        id = 0
      }
    } else {
      if (sub.status === 'Pending') {
        // 重测入队 / 失联回收重排：新一轮判题从零点亮（新一轮同样跟到底）
        clearTimer()
        watching = true
        shown = 0
        total = 0
        nextAt = 0
        resolveAll()
      }
      cur = sub
    }
    if (sub?.case_total) total = Math.max(total, sub.case_total)
    if (sub && watching && !active) {
      // 不在屏时如实直出 = 屏上已是全量：点亮数对齐真实——回屏后只给新到的点续播，
      // 判完的（含不在屏期间判完的）不重放
      shown = sub.detail?.length ?? 0
      nextAt = clock.now() // 全量直出不算一次限速步：回屏后新到的点到了就亮
    }
    // 真实数据缩水（领取清场/回收重排）：排着的定格作废，点亮数立刻跟随回落
    if (timer != null && (sub?.detail?.length ?? 0) < shown) clearTimer()
    pump()
  }

  function noteTotal(sid: number, n: number) {
    if (sid === id && n > total) total = n
  }

  function settle(): Promise<void> {
    if (settled()) return Promise.resolve()
    return new Promise<void>((resolve) => {
      settlers.push(resolve)
      if (bound == null) {
        // 有界兜底：弹窗那一拍取数失败时真实快照停在队列态——按剩余点数回放完的时长
        // 再加取数余量放行，完成提示不因显示层挂死
        bound = clock.set(() => {
          bound = null
          resolveAll()
        }, SETTLE_FETCH_SLACK_MS + (Math.max(0, total - shown) + 1) * stepMs)
      }
    })
  }

  function dispose() {
    endRun()
    watching = false
    cur = null
  }

  const api: CasePlayback = { feed, noteTotal, settle, dispose }
  return api
}

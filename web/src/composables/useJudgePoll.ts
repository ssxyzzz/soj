import { ElMessage } from 'element-plus'

import { isQueueStatus } from '@/utils/format'

/**
 * 判题结果轮询（v0.29 收敛）：提交/重测/取消后 1.2s 拉详情，终态停并回调。
 * 终态判断统一走 utils/format 的 isQueueStatus（含 CancelRequested——此前各处
 * 自写 ['Pending','Judging'] 判断会漏取消收尾期）；作用域卸载自动停。
 */
export function useJudgePoll<T extends { status: string }>(fetchOne: () => Promise<T>) {
  let timer: ReturnType<typeof setTimeout> | null = null
  let stopped = false

  function stop() {
    stopped = true
    if (timer != null) clearTimeout(timer)
    timer = null
  }

  /** 开始轮询；onDone 在终态时回调一次（终态提交体作参数），onUpdate 每拍回调 */
  function start(onDone: (s: T) => void, doneMsg?: string, onUpdate?: (s: T) => void) {
    stop()
    stopped = false
    const tick = async () => {
      if (stopped) return
      let s: T | null = null
      try {
        s = await fetchOne()
      } catch {
        return // 拉取失败已由拦截器提示，停止本轮轮询
      }
      if (stopped) return
      onUpdate?.(s)
      if (isQueueStatus(s.status)) {
        timer = setTimeout(tick, 1200)
      } else {
        stopped = true
        if (doneMsg) ElMessage.success(doneMsg)
        onDone(s)
      }
    }
    timer = setTimeout(tick, 1200)
  }

  return { start, stop }
}

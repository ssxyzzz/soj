import { onScopeDispose, ref, shallowRef, watch, type Ref, type ShallowRef } from 'vue'

import { useTopic } from '@/composables/useEventStream'
import { useListener } from '@/composables/useWindowListener'
import { createCasePlayback } from '@/utils/casePlayback'
import type { Submission, SubPushEvent } from '@/types'

/**
 * 逐点色块回放（v0.99.1）：source 是最新真实快照，返回值是给弹窗渲染的展示快照——
 * 判题中色块按 ≥100ms/点 逐个点亮，终态回落明细表排在最后一枚之后（真实判题更慢时
 * 照真实节奏走）。首帧已是终态（直接打开已判完的提交）不回放，原样透传。
 *
 * active=false（弹窗关着 / 不在详情视图）或标签页在后台时色块条无人看：不回放、
 * 如实直出——完成提示经 settleCasePlayback 也随之立即放行。
 */
export function useCasePlayback(
  source: Ref<Submission | null>,
  active: () => boolean = () => true,
): ShallowRef<Submission | null> {
  const view = shallowRef<Submission | null>(null)
  const playback = createCasePlayback((v) => {
    view.value = v
  })
  // 后台标签页定时器被浏览器节流到约 1 次/秒：无人观看的回放没有意义，直接不回放
  const visible = ref(!document.hidden)
  useListener(document, 'visibilitychange', () => {
    visible.value = !document.hidden
  })
  watch(
    [source, active, visible],
    ([s, on, show]) => playback.feed(s, on && show),
    { immediate: true, flush: 'sync' },
  )
  // SSE progress 信号带 case_total：终态快照不带该字段，信号先到先把点数总数记下
  useTopic('sub', (raw) => {
    const ev = raw as unknown as SubPushEvent
    playback.noteTotal(ev.sid, ev.case_total ?? 0)
  })
  onScopeDispose(() => playback.dispose())
  return view
}

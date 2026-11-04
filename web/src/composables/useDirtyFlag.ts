import { onScopeDispose, ref, watch } from 'vue'

/**
 * 脏检查唯一实现（v0.65 输入热路径，病根①同款收敛）：ListEditView/TagsPanel 此前
 * 两处 computed 每次击键同步 JSON.stringify 整文档（大题单数百题/大树数百节点 =
 * 每键毫秒级停顿）。本实现 deep watch 触发 + 300ms 防抖比对一次——输入期间零
 * stringify，且精确保留「改回原值 = 不脏」语义（纯脏标记会误报）。
 *
 * recheck：基线复位点（载入完成/保存成功）立即比对一次，不等防抖。
 */
export function useDirtyFlag(current: () => unknown, baseline: () => string) {
  const dirty = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined

  function check(): void {
    dirty.value = JSON.stringify(current()) !== baseline()
  }

  watch(current, () => {
    if (timer) clearTimeout(timer)
    timer = setTimeout(check, 300)
  }, { deep: true })

  function recheck(): void {
    if (timer) clearTimeout(timer)
    check()
  }

  onScopeDispose(() => {
    if (timer) clearTimeout(timer)
  })

  return { dirty, recheck }
}

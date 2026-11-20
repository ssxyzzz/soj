// 沉浸模式（题面专注 v0.79 起家，画板铺满 v0.97.19 并入）：模块级单例状态——
// ProblemStatement 限制条右端「展开」钮与 BoardView 工具栏「全屏画布」钮切换同一状态，
// MainLayout 据此隐藏顶栏导航，画板侧根节点 fixed 铺满整个浏览器视口（DOM 级铺满，
// 不动 Fullscreen API——保留浏览器控件）。Esc 退出（题面侧与画板侧同语义，画板画布
// 自身的 Esc 分支亦写同一状态）；路由切换自动复位，防带着隐藏态跳走后新页面缺导航。
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'

const focused = ref(false)

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') focused.value = false
}

// 监听器挂模块级 watch（应用生命周期常驻，Esc 语义随状态启停）
watch(focused, (v) => {
  if (v) document.addEventListener('keydown', onKey)
  else document.removeEventListener('keydown', onKey)
})

export function useImmersiveMode() {
  // 路由变化即退出沉浸（各调用方 setup 内各挂一个 watch，复位幂等）
  const route = useRoute()
  watch(
    () => route.fullPath,
    () => {
      if (focused.value) focused.value = false
    },
  )
  return {
    focused,
    toggle: () => {
      focused.value = !focused.value
    },
  }
}

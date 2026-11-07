// 题面专注模式（v0.79）：模块级单例状态——ProblemStatement 限制条右端「展开」钮切换，
// MainLayout 隐藏顶栏导航、做题页隐藏右栏卡片与独立头，只留题面（DOM 级铺满浏览器视口，
// 不动 Fullscreen API——学生场景保留浏览器控件）。Esc 退出（画板铺满模式同语义）；
// 路由切换自动复位，防带着隐藏态跳走后新页面缺导航。
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

export function useStatementFocus() {
  // 路由变化即退出专注（各调用方 setup 内各挂一个 watch，复位幂等）
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

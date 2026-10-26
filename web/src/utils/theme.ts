// 站点主题切换（2026-10-26）：v5 新案（默认）/ 原版 · 深海军蓝。
// 实现是 <html data-theme> 上的变量覆写组（main.css 的 html[data-theme='navy'] 块），
// 持久化仅走本地 localStorage（soj_theme），main.ts 挂载前应用避免首帧闪色。
export type ThemeName = 'v5' | 'navy'

const KEY = 'soj_theme'

export function applyTheme(t: ThemeName) {
  document.documentElement.dataset.theme = t
  localStorage.setItem(KEY, t)
}

export function currentTheme(): ThemeName {
  return localStorage.getItem(KEY) === 'navy' ? 'navy' : 'v5'
}

export function initTheme(): ThemeName {
  const t = currentTheme()
  document.documentElement.dataset.theme = t
  return t
}

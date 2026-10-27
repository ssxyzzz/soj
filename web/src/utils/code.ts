// 判题语言与起始代码：提交弹窗 / 在线编程共用。
// 系统不预置任何语言的代码模板；个人代码模板（stores/user.ts 的 prefs，随登录经
// /me/profile 拉进 store）只作为在线编程（CodeWorkbench）的起始代码填入，
// 提交弹窗默认全空白（2026-10-27）。
import { useLevelsStore } from '@/stores/levels'
import { useUserStore } from '@/stores/user'
import type { UserPrefs } from '@/types'

export const languageOptions = [
  { value: 'cpp', label: 'C++ 14' },
  { value: 'c', label: 'C' },
  { value: 'python3', label: 'Python 3' },
]

// 默认语言：个人设置的偏好优先，非法/未设置回退 cpp。
export function defaultLang(): string {
  const lang = useUserStore().prefs.defaultLang ?? ''
  return languageOptions.some((o) => o.value === lang) ? lang : 'cpp'
}

// 某语言的个人模板起始代码（仅在线编程使用）：模板非空时返回，否则为空白。
export function templateFor(lang: string): string {
  const pref = useUserStore().prefs.codeTemplates?.[lang]
  return pref && pref.trim() ? pref : ''
}

// ---- 自动补全可用性（等级门槛，v0.11 落地 v0.5.3 预留的接口） ----
// 等级 ≥ 系统设置阈值（默认 Lv.5）才能开启自动补全；root/admin 不受限（门槛面向学生）。
// 编辑器与个人设置都走这一个判断点，不各自为政。等级随 /me/profile 异步就绪，就绪后本
// 判断响应式刷新（调用处都在 computed/模板中）。
export function autocompleteAvailable(): boolean {
  const st = useUserStore()
  if (!st.token) return false
  if (st.role === 'root' || st.role === 'admin') return true
  const lv = st.level?.level ?? 1
  return lv >= useLevelsStore().autocompleteMinLevel()
}

// 实际生效：可用且未显式关闭（默认开箱开启，见 v0.5.3 功能基线）。
export function autocompleteOn(): boolean {
  return autocompleteAvailable() && useUserStore().prefs.autocomplete !== false
}

// ---- 偏好保存 ----
// v0.8.1 起后端 prefs 为「在既有偏好上合并」语义：PATCH 只带本次变更的字段即可
// （工具栏切编辑模式/主题走这条路径）。mergedPrefs 仍用于设置页整表保存与
// 本地 store 的乐观更新，保证本地快照字段齐全。
export function mergedPrefs(patch: Partial<UserPrefs>): UserPrefs {
  const st = useUserStore()
  const p = st.prefs ?? {}
  const merged: UserPrefs = {
    defaultLang: p.defaultLang ?? '',
    codeTemplates: p.codeTemplates ?? {},
    autocomplete: p.autocomplete ?? true,
    codeFont: p.codeFont ?? '',
    codeFontSize: p.codeFontSize ?? 0, // 0 = 未设置，后端按默认 14 处理
    mdEditMode: p.mdEditMode ?? '',
    mdTheme: p.mdTheme ?? '',
    ...patch,
  }
  // 等级未达标时强制 false：后端对 autocomplete=true 一律按等级拒绝，而设置页
  // 开关对未达标用户是禁用的（关不掉）——不 clamp 会让低等级用户整表保存、
  // 编辑器字体字号回存全部被 400 拦死。等级尚未加载（null）时不 clamp，
  // 避免把高等级用户的自动补全误关。
  if (st.level !== null && !autocompleteAvailable()) merged.autocomplete = false
  return merged
}

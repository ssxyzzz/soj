// 判题语言与起始代码：提交弹窗 / 在线编程共用。
// 系统不预置任何语言的代码模板，编辑器初始为全空白；
// 仅当用户在个人设置里自定义了该语言的模板时才填充（stores/user.ts 的 prefs，
// 随登录经 /me/profile 拉进 store）。
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

// 某语言的起始代码：仅当个人设置里该语言的模板非空时返回，否则为空白。
export function templateFor(lang: string): string {
  const pref = useUserStore().prefs.codeTemplates?.[lang]
  return pref && pref.trim() ? pref : ''
}

// ---- 自动补全可用性（预留等级接口，v0.5.3） ----
// 后续上线用户等级功能后，由等级决定能否开启自动补全：
// 届时只需改这一个判断点（如 `level >= 门槛等级`），编辑器与个人设置都走这里，不各自为政。
export function autocompleteAvailable(): boolean {
  return true
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
  const p = useUserStore().prefs ?? {}
  return {
    defaultLang: p.defaultLang ?? '',
    codeTemplates: p.codeTemplates ?? {},
    autocomplete: p.autocomplete ?? true,
    codeFont: p.codeFont ?? '',
    codeFontSize: p.codeFontSize ?? 0, // 0 = 未设置，后端按默认 14 处理
    mdEditMode: p.mdEditMode ?? '',
    mdTheme: p.mdTheme ?? '',
    ...patch,
  }
}

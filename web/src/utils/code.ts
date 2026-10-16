// 判题语言与起始代码：提交弹窗 / 在线编程共用。
// 系统不预置任何语言的代码模板，编辑器初始为全空白；
// 仅当用户在个人设置里自定义了该语言的模板时才填充（stores/user.ts 的 prefs，
// 随登录经 /me/profile 拉进 store）。
import { useUserStore } from '@/stores/user'

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

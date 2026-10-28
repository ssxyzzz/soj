// 讨论区共享元数据（v0.24）：三区各自的帖子类型集合（与后端 repo.DiscussValidKinds 同源），
// 标签文案与 el-tag type。新增类型两边同步。
import type { DiscussScope } from '@/types'

export interface DiscussKindMeta {
  value: string
  label: string
  tag: 'primary' | 'success' | 'warning' | 'danger' | 'info'
}

export const DiscussKindSets: Record<DiscussScope, DiscussKindMeta[]> = {
  global: [
    { value: 'experience', label: '学习心得', tag: 'success' },
    { value: 'route', label: '刷题路线', tag: 'primary' },
    { value: 'tutorial', label: '知识讲解', tag: 'warning' },
    { value: 'feedback', label: '站点反馈', tag: 'info' },
  ],
  problem: [
    { value: 'help', label: '求助', tag: 'danger' },
    { value: 'bug', label: '纠错', tag: 'warning' },
    { value: 'share', label: '分享', tag: 'success' },
  ],
  contest: [
    { value: 'question', label: '提问', tag: 'primary' },
    { value: 'notice', label: '公告', tag: 'warning' },
  ],
}

export function discussKind(scope: DiscussScope, value: string): DiscussKindMeta {
  return (
    DiscussKindSets[scope]?.find((k) => k.value === value) || {
      value,
      label: value,
      tag: 'info',
    }
  )
}

export function discussKindLabel(scope: DiscussScope, value: string): string {
  return discussKind(scope, value).label
}

// 通知类型的图标与文案（消息中心 + 铃铛下拉共用）。
export const NotifyTypeMeta: Record<string, { label: string; icon: string }> = {
  reply: { label: '讨论回复', icon: '💬' },
  mention: { label: '提到我', icon: '📣' },
  like: { label: '收到点赞', icon: '👍' },
  accept: { label: '回答被采纳', icon: '✅' },
  assignment: { label: '作业', icon: '📘' },
  exam: { label: '考试', icon: '📝' },
  contest: { label: '比赛', icon: '🏆' },
  broadcast: { label: '公告', icon: '📢' },
  warn: { label: '管理通知', icon: '🛡️' },
}

export function notifyTypeMeta(type: string) {
  return NotifyTypeMeta[type] || { label: '通知', icon: '🔔' }
}

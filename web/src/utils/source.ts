// 来源四字段（v0.12）前端共用：类型枚举展示名与徽章链文案组包。
// 与后端 repo.SourceTypeLabels 一一对应。

export interface SourceFields {
  source_type?: string
  source?: string
  year?: number
  region?: string
}

export const SOURCE_TYPE_OPTIONS: { value: string; label: string }[] = [
  { value: 'oj', label: 'OJ 站点' },
  { value: 'contest', label: '全国赛事' },
  { value: 'region', label: '地区活动' },
  { value: 'local', label: '校内·原创' },
  { value: 'other', label: '其他' },
]

export const SOURCE_TYPE_LABELS: Record<string, string> = Object.fromEntries(
  SOURCE_TYPE_OPTIONS.map((o) => [o.value, o.label]),
)

export function sourceTypeLabel(t?: string): string {
  return t ? (SOURCE_TYPE_LABELS[t] ?? t) : ''
}

// sourceChain 徽章链文案：`类型 · 来源名 年份（地区）`，无类型时省略前缀；
// 四字段全空返回空串（调用方 v-if 隐藏）。
export function sourceChain(s: SourceFields): string {
  const label = sourceTypeLabel(s.source_type)
  const tail = [
    s.source || '',
    s.year ? String(s.year) : '',
    s.region ? `（${s.region}）` : '',
  ]
    .filter(Boolean)
    .join(' ')
  if (label && tail) return `${label} · ${tail}`
  return label || tail
}

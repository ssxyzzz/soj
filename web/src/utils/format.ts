export interface StatusStyle {
  label: string
  type: 'success' | 'warning' | 'danger' | 'info'
}

// 与后端 internal/model 中的状态常量对应
const statusMap: Record<string, StatusStyle> = {
  Pending: { label: '排队中', type: 'info' },
  Judging: { label: '判题中', type: 'warning' },
  Accepted: { label: '通过', type: 'success' },
  'Wrong Answer': { label: '答案错误', type: 'danger' },
  'Compile Error': { label: '编译错误', type: 'danger' },
  'Time Limit Exceeded': { label: '超时', type: 'danger' },
  'Memory Limit Exceeded': { label: '超内存', type: 'danger' },
  'Runtime Error': { label: '运行错误', type: 'danger' },
  'System Error': { label: '系统错误', type: 'info' },
  // 测试点明细使用缩写（judge 写入 CaseResult.Status）
  AC: { label: '通过', type: 'success' },
  WA: { label: '答案错误', type: 'danger' },
  TLE: { label: '超时', type: 'danger' },
  MLE: { label: '超内存', type: 'danger' },
  RE: { label: '运行错误', type: 'danger' },
  OLE: { label: '答案超长', type: 'warning' },
  SE: { label: '系统错误', type: 'info' },
}

export function statusStyle(status: string): StatusStyle {
  return statusMap[status] ?? { label: status, type: 'info' }
}

export function fmtMem(kb: number): string {
  if (!kb) return '-'
  return kb >= 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${kb} KB`
}

export function fmtDate(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('zh-CN', { hour12: false })
}

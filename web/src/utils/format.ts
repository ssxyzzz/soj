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

// 评测状态说明（含义 + 常见原因）：提交记录/我的提交的状态标签悬停展示，方便学生定位问题。
export interface StatusDoc {
  meaning: string
  cause?: string
}

const statusDocMap: Record<string, StatusDoc> = {
  Pending: { meaning: '排队中，等待评测' },
  Judging: { meaning: '正在评测' },
  Accepted: { meaning: '通过，拿到满分' },
  'Wrong Answer': {
    meaning: '答案错误',
    cause: '输出与期望不一致：检查输出格式、多余空格/换行、边界情况',
  },
  'Time Limit Exceeded': {
    meaning: '超出时间限制',
    cause: '算法复杂度过高或死循环：优化算法，确认循环能终止',
  },
  'Memory Limit Exceeded': {
    meaning: '超出内存限制',
    cause: '数组/递归开太大：减小规模或改用更省内存的写法',
  },
  'Runtime Error': {
    meaning: '运行时错误',
    cause: '常见原因：数组越界、除零、空指针、栈溢出（递归过深）',
  },
  'Compile Error': {
    meaning: '编译错误',
    cause: '代码无法编译：查看提交详情里的编译器报错信息定位行号',
  },
  'Output Limit Exceeded': {
    meaning: '输出超过限制',
    cause: '可能存在死循环输出，检查输出是否无边界的循环',
  },
  'System Error': {
    meaning: '系统错误',
    cause: '评测机内部异常（不是你的问题），可重新提交',
  },
}

// 取某状态的悬停说明；未知状态返回 undefined（标签按原文展示）。
export function statusDesc(status: string): StatusDoc | undefined {
  return statusDocMap[status]
}

export function statusStyle(status: string): StatusStyle {
  return statusMap[status] ?? { label: status, type: 'info' }
}

export function fmtMem(kb: number): string {
  if (!kb) return '-'
  const strip = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1))
  const mb = kb / 1024
  if (mb < 1) return `${kb} KB`
  const gb = mb / 1024
  return gb >= 1 ? `${strip(gb)} GB` : `${strip(mb)} MB`
}

export function fmtDate(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('zh-CN', { hour12: false })
}

export interface StatusStyle {
  label: string
  type: 'success' | 'warning' | 'danger' | 'info'
  /** 状态圆灯颜色（CSS .dot 工具类；与站点语义色一致） */
  dot: 'green' | 'yellow' | 'red' | 'gray' | 'blue'
  /** 灯后英文简称（AC/WA/TLE…；Pending/Judging 用原词） */
  abbr: string
}

// 与后端 internal/model 中的状态常量对应
// 灯色语义：绿=通过 / 红=答案或运行错误 / 黄=超限类（时/存/输出）/ 灰=编译错误·排队·系统错误 / 蓝=进行中
// 状态候选（提交记录筛选 chips 用）：全站状态表原样导出
export const statusMap: Record<string, StatusStyle> = {
  Pending: { label: '排队中', type: 'info', dot: 'gray', abbr: 'Pending' },
  Judging: { label: '判题中', type: 'warning', dot: 'blue', abbr: 'Judging' },
  CancelRequested: { label: '取消中', type: 'warning', dot: 'blue', abbr: 'Cancel' },
  Cancelled: { label: '已取消', type: 'info', dot: 'gray', abbr: 'Cancelled' },
  Accepted: { label: '通过', type: 'success', dot: 'green', abbr: 'AC' },
  'Wrong Answer': { label: '答案错误', type: 'danger', dot: 'red', abbr: 'WA' },
  'Compile Error': { label: '编译错误', type: 'danger', dot: 'gray', abbr: 'CE' },
  'Time Limit Exceeded': { label: '超时', type: 'danger', dot: 'yellow', abbr: 'TLE' },
  'Memory Limit Exceeded': { label: '超内存', type: 'danger', dot: 'yellow', abbr: 'MLE' },
  'Runtime Error': { label: '运行错误', type: 'danger', dot: 'red', abbr: 'RE' },
  'Output Limit Exceeded': { label: '输出超限', type: 'warning', dot: 'yellow', abbr: 'OLE' },
  'System Error': { label: '系统错误', type: 'info', dot: 'gray', abbr: 'SE' },
  // 测试点明细使用缩写（judge 写入 CaseResult.Status）
  AC: { label: '通过', type: 'success', dot: 'green', abbr: 'AC' },
  WA: { label: '答案错误', type: 'danger', dot: 'red', abbr: 'WA' },
  TLE: { label: '超时', type: 'danger', dot: 'yellow', abbr: 'TLE' },
  MLE: { label: '超内存', type: 'danger', dot: 'yellow', abbr: 'MLE' },
  RE: { label: '运行错误', type: 'danger', dot: 'red', abbr: 'RE' },
  OLE: { label: '答案超长', type: 'warning', dot: 'yellow', abbr: 'OLE' },
  SE: { label: '系统错误', type: 'info', dot: 'gray', abbr: 'SE' },
}

// 评测状态说明（含义 + 常见原因）：提交记录/我的提交的状态标签悬停展示，方便学生定位问题。
export interface StatusDoc {
  meaning: string
  cause?: string
}

const statusDocMap: Record<string, StatusDoc> = {
  Pending: { meaning: '排队中，等待评测' },
  Judging: { meaning: '正在评测' },
  CancelRequested: { meaning: '已请求取消，评测将在当前测试点结束后停止' },
  Cancelled: { meaning: '提交已被取消，得分 0，不参与计分' },
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

// 队列态（未出结果）：排队/评测中/已请求取消。轮询「判题完成」的统一判据，
// 误把 CancelRequested 当终态会在取消收尾期间提前停止轮询。
export function isQueueStatus(status?: string): boolean {
  return status === 'Pending' || status === 'Judging' || status === 'CancelRequested'
}

export function statusStyle(status: string): StatusStyle {
  return statusMap[status] ?? { label: status, type: 'info', dot: 'gray', abbr: status }
}

export function fmtMem(kb: number): string {
  if (!kb) return '-'
  const strip = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1))
  const mb = kb / 1024
  if (mb < 1) return `${kb} KB`
  const gb = mb / 1024
  return gb >= 1 ? `${strip(gb)} GB` : `${strip(mb)} MB`
}

// 评测耗时/时限展示：毫秒统一换算为秒（固定1位小数，如 "1.0 s"），站点口径不用 ms。
// null/undefined 返回 ''（表格空列）；0 返回 "0.0 s"。
export function fmtSec(ms?: number | null): string {
  if (ms == null) return ''
  return `${(ms / 1000).toFixed(1)} s`
}

export function fmtDate(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('zh-CN', { hour12: false })
}

// 短日期（v0.19 首页/比赛行）：MM-DD HH:mm（v3 卡片行时间口径）
export function fmtShort(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

// 统一成绩表视图模型（v0.6.2）：比赛/考试/作业三处共用。
// 原始数据由各自 adapter 归一到这里，组件本身不判来源、不判权限。
export type ScoreState = 'full' | 'partial' | 'zero' | 'untried'

export interface ScoreProblemView {
  pid: number
  label: string // T1 / T2…
  title: string // 真实题名（T 头 hover）
  fullScore: number
  route: string | null // 点 T 跳题；null = 不可跳
}

export interface ScoreCellView {
  pid: number
  score: number
  errors: number // 未满分提交次数（角标口径）
  fullScore: number
  tries: number
  state: ScoreState
}

export interface ScoreRowView {
  uid: number
  rank: number | null // 学生视角单人行时置 null，显示 —
  name: string // 昵称，空则用户名
  total: number
  solved: number
  totalProblems: number
  progress: number // 0–100，进度条
  cells: ScoreCellView[]
}

export interface ScoreBoardView {
  problems: ScoreProblemView[]
  rows: ScoreRowView[]
  canInspect: boolean // 谁能点分数看提交明细（3A）
  showRank: boolean // 作业学生视角为 false
}

export function scoreState(score: number, fullScore: number, tries: number): ScoreState {
  if (fullScore > 0 && score >= fullScore) return 'full'
  if (score > 0) return 'partial'
  return tries > 0 ? 'zero' : 'untried'
}

export function progressPct(solved: number, total: number): number {
  if (total <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((solved / total) * 100)))
}

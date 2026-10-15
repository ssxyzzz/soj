// 与后端 internal/server 各 handler 返回的 JSON 字段一一对应
export interface ApiResponse<T> {
  code: number
  message?: string
  data: T
}

export interface User {
  id: number
  username: string
  nickname: string
  role: string
}

export interface LoginResult {
  token: string
  user: User
}

export interface ProblemListItem {
  id: number
  title: string
  hidden?: boolean
  locked?: boolean
  contest_id?: number
  exam_id?: number
  lock_kind?: 'contest' | 'exam'
  lock_phase?: string
}

export interface ProblemListResult {
  list: ProblemListItem[]
  total: number
}

export interface TestCase {
  id: number
  input: string
  output: string
}

export interface ProblemDetail {
  id: number
  title: string
  description: string
  time_limit_ms: number
  memory_limit_kb: number
  samples: TestCase[]
  has_subtasks?: boolean
  hidden?: boolean
  can_set_hidden?: boolean
  can_view_others?: boolean
  locked?: boolean
  lock_kind?: 'contest' | 'exam'
  contest_id?: number
  contest_title?: string
  exam_id?: number
  exam_title?: string
  masked?: boolean
}

export interface Submission {
  id: number
  user_id: number
  username: string
  problem_id: number
  problem_title: string
  language: string
  status: string
  time_ms: number
  memory_kb: number
  score: number
  code?: string
  error_info?: string
  created_at: string
  contest_id?: number
  contest_title?: string
  exam_id?: number
  exam_title?: string
}

export interface SubmissionListResult {
  list: Submission[]
  total: number
}

export interface RankItem {
  rank: number
  user_id: number
  username: string
  nickname: string
  solved: number
}

export interface ClassCard {
  id: number
  name: string
  intro: string
  member_count: number
  problem_count: number
}

export interface ClassProblem {
  id: number
  title: string
  hidden: boolean
  contest_id?: number
}

// ---------- 作业（班级题目清单，即时出分） ----------

export interface AssignmentListItem {
  id: number
  title: string
  intro: string
  problem_count: number
  created_at: string
}

export interface AssignmentProblem {
  id: number
  title: string
  contest_id?: number
  exam_id?: number
}

export interface AssignmentDetail {
  id: number
  class_id: number
  title: string
  intro: string
  problems: AssignmentProblem[]
  manageable: boolean
}

export interface ProgressCell {
  pid: number
  tries: number
  best: number
  solved: boolean
}

export interface ProgressRow {
  uid: number
  username: string
  nickname: string
  attempted: number
  solved: number
  problems: ProgressCell[]
}

export interface ClassMember {
  uid: number
  username: string
  role: string
  joined_at: string
  is_owner: boolean
}

export interface ClassDetail {
  id: number
  name: string
  intro: string
  owner_id: number
  joined: boolean
  manageable: boolean
  invite_code?: string
  members?: ClassMember[]
}

export interface AdminUser {
  id: number
  username: string
  nickname: string
  role: string
}

// ---------- 比赛（v0.4） ----------

export interface ContestListItem {
  id: number
  title: string
  status: 'scheduled' | 'running' | 'ended'
  start: string
  end: string
  hidden_until?: string
  problem_count: number
  is_organizer: boolean
}

export interface ContestProblem {
  id: number
  title: string
  score: number
}

export interface ContestDetail {
  id: number
  title: string
  intro: string
  status: 'scheduled' | 'running' | 'ended'
  start: string
  end: string
  hidden_until?: string
  problem_count: number
  participant: boolean
  is_organizer: boolean
  deletable: boolean
  problems?: ContestProblem[]
}

// ---------- 考试（班级级赛事，v0.4.2） ----------

export interface ExamListItem {
  id: number
  title: string
  intro: string
  status: 'scheduled' | 'running' | 'ended'
  start: string
  end: string
  hidden_until?: string
  problem_count: number
  is_organizer: boolean
}

export type ExamDetail = ContestDetail & { class_id: number }

export interface ContestMySubmission {
  id: number
  pid: number
  created_at: string
  status?: string
  score?: number
  language?: string
  time_ms?: number
  memory_kb?: number
  error_info?: string
}

export interface LeaderCell {
  pid: number
  score: number
  tries: number
}

export interface LeaderRow {
  rank: number
  uid: number
  username: string
  nickname: string
  total: number
  last_scoring_at?: string
  problems: LeaderCell[]
}

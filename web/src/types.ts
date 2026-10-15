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

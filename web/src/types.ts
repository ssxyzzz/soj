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
  perms?: string[] // 生效权限位（root 展开为全目录）；仅登录态接口返回
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
  submit_count?: number // 公开态提交数（通过率展示用）
  accepted_count?: number // 其中 AC 的提交数
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
  compare?: string
  float_tolerance?: number
  has_checker?: boolean
  samples: TestCase[]
  has_subtasks?: boolean
  hidden?: boolean
  can_set_hidden?: boolean
  can_edit_statement?: boolean
  can_view_others?: boolean
  can_view_solutions?: boolean
  file_io_input?: string
  file_io_output?: string
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
  detail?: CaseResult[]
}

// 单个测试点结果（与后端 CaseResult 对应）
export interface CaseResult {
  index: number
  status: string
  time_ms: number
  memory_kb: number
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
  assignment_count: number
  private: boolean
  created_at: string
  joined: boolean
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
  private: boolean
  created_at: string
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
  real_name?: string // 真实姓名：仅管理员后台可见
}

// ---------- 个人设置（v0.4.8） ----------

// 用户偏好：跟随账号存储，前端消费（默认语言/代码模板/MD 编辑模式）
export interface UserPrefs {
  defaultLang?: string
  codeTemplates?: Record<string, string>
  mdEditMode?: '' | 'wysiwyg' | 'ir' | 'sv'
  autocomplete?: boolean // 代码自动补全（默认开箱开启；后续按用户等级门槛放开）
  codeFont?: string // 编辑器字体：Consolas / Menlo / Monaco（默认 Consolas）
  codeFontSize?: number // 编辑器字号 px（12-28，默认 14）
}

// 本人视角的个人信息（仅本人与管理员后台可见，不进任何公开接口）
export interface UserProfile {
  real_name: string
  gender: string
  school: string
  grade: string
  qq: string
  prefs: UserPrefs
}

// ---------- 题解（v0.5b-1） ----------

export interface SolutionItem {
  id: number
  uid: number
  nickname: string
  content: string
  pinned: boolean
  created_at: string
  updated_at: string
}

// ---------- 备份（v0.4.9） ----------

// 备份归档下载接口路径（集中在此：调用方引用常量名即可）
export const BACKUP_RUN_URL = '/api/admin/backup/run'

export interface BackupStatus {
  mode: 'uninitialized' | 'tier0' | 'tier1'
  last_backup_at: string | null
  last_backup_file: string | null
  guide?: string // 备份面板使用说明（Markdown，空 = 前端内置默认）
}

// ---------- 角色与权限位（v0.4.7） ----------

export interface PermInfo {
  id: string
  label: string
  desc: string
  group: string
}

export interface PermGroupInfo {
  id: string
  label: string
}

export interface RoleInfo {
  id: string
  name: string
  perms: string[]
  builtin: boolean
  user_count: number
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
  file_io?: boolean // 本场启用文件 IO（v0.5b-3）
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
  code?: string
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

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
  has_cases?: boolean // 无测试数据空壳题（v0.6.1 验收反馈）：题库列表「无数据」小提示
  locked?: boolean
  contest_id?: number
  exam_id?: number
  lock_kind?: 'contest' | 'exam'
  lock_phase?: string
  submit_count?: number // 公开态提交数（通过率展示用）
  accepted_count?: number // 其中 AC 的提交数
  status?: string // 我的最高分提交状态（登录才返回，全称如 Accepted/Wrong Answer）
  my_score?: number // 该提交的得分
  tags?: number[] // 标签树节点 ID，名字由前端标签树映射
}

// 标签树（v0.5.5）：嵌套 children 表达层级，数组顺序即展示顺序
export interface TagNode {
  id: number
  name: string
  children?: TagNode[]
}

export interface TagTreeResult {
  nodes: TagNode[]
}

// 管理面板读（GET /admin/tagtree）：树 + 每节点被引用题数（key 为节点 ID 字符串，未出现 = 0）
export interface AdminTagTreeResult {
  nodes: TagNode[]
  counts: Record<string, number>
}

// 标签树保存被拒（删除被引用标签）时 failData 附带的引用题目明细
export interface TagRefProblem {
  id: number
  title: string
  tags: string[]
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

// 题目 URL 导入预览（v0.6.1）：服务端抓取解析 + 外链图转存后的统一中间格式
export interface ImportPreviewResult {
  title: string
  html: string
  samples: { input: string; output: string }[]
  time_limit_ms: number
  memory_limit_mb: number
  source: string
  transfer: string
}

export interface ProblemDetail {
  id: number
  title: string
  description: string
  description_html: string // 富文本题面（v0.6；存量 md 由服务端读时计算）
  time_limit_ms: number
  memory_limit_kb: number
  compare?: string
  float_tolerance?: number
  has_checker?: boolean
  has_cases?: boolean // 无测试数据空壳题（v0.6.1 验收反馈）：详情提示 + 禁提交
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
  tags?: number[] // 标签树节点 ID（题目信息卡展示，名字由前端标签树映射）
  submit_count?: number // 公开态提交数（题目信息卡）
  accepted_count?: number // 其中 AC 的提交数
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
  msg?: string // 失败明细（如 超出时间限制 / 未生成输出文件 x.out），AC 为空
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
  submitted?: boolean
  status?: string
  my_score?: number
  tags?: number[]
  submit_count?: number
  accepted_count?: number
  result_hidden?: boolean
}

export interface AssignmentDetail {
  id: number
  class_id: number
  title: string
  intro: string
  intro_html: string
  problems: AssignmentProblem[]
  edit_problems?: { id: number; title: string; hidden?: boolean }[] // 管理员调整题目用完整清单
  manageable: boolean
}

export interface ProgressCell {
  pid: number
  tries: number
  best: number
  errors: number
  solved: boolean
}

export interface ProgressRow {
  uid: number
  username: string
  nickname: string
  attempted: number
  solved: number
  total: number
  rank: number
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
  intro_html: string
  owner_id: number
  private: boolean
  created_at: string
  joined: boolean
  manageable: boolean
  leaveable?: boolean // 可退出班级（v0.7.2：class.leave 权限位 + 建班人空班例外，后端计算）
  invite_code?: string
  members?: ClassMember[]
}

export interface AdminUser {
  id: number
  username: string
  nickname: string
  role: string
}

// ---------- 个人设置（v0.4.8） ----------

// 用户偏好：跟随账号存储，前端消费（默认语言/代码模板/MD 编辑模式）
export interface UserPrefs {
  defaultLang?: string
  codeTemplates?: Record<string, string>
  autocomplete?: boolean // 代码自动补全（默认开箱开启；后续按用户等级门槛放开）
  codeFont?: string // 编辑器字体：Consolas / Menlo / Monaco（默认 Consolas）
  codeFontSize?: number // 编辑器字号 px（12-48，默认 14）
}

// 本人视角的个人信息（仅本人与管理员后台可见，不进任何公开接口）
export interface UserProfile {
  nickname: string
  can_edit_nickname: boolean // 当前角色是否可自助改昵称（后端按生效权限位现算）
  gender: string
  school: string
  grade: string
  qq: string
  prefs: UserPrefs
}

// ---------- 题解（v0.5.2-1） ----------

export interface SolutionItem {
  id: number
  uid: number
  nickname: string
  content: string
  content_html: string
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
  guide_html?: string // 备份面板使用说明（富文本，v0.6；空 = 前端内置默认）
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
  submitted?: boolean // 当前用户在本场次是否提交过（登录才返回）
  status?: string // 当前用户本场次最高分提交状态（结果可见时返回）
  my_score?: number
  tags?: number[] // 标签节点 ID（结果不可见时不返回）
  submit_count?: number // 通过率（公开态提交；结果不可见时不返回）
  accepted_count?: number
  result_hidden?: boolean // 该题结果/状态/标签/通过率是否对本视角隐藏
}

export interface ContestDetail {
  id: number
  title: string
  intro: string
  intro_html: string
  status: 'scheduled' | 'running' | 'ended'
  start: string
  end: string
  hidden_until?: string
  problem_count: number
  participant: boolean
  is_organizer: boolean
  deletable: boolean
  published: boolean // 赛后隐藏期结束、结果已公开
  file_io?: boolean // 本场启用文件 IO（v0.5.2-3）
  scoreboard: boolean // v0.7：是否开启成绩表
  hide_during: boolean // v0.7：赛期内对参赛者隐藏结果
  result_hidden: boolean // 当前视角是否隐藏结果
  class_id?: number // 考试归属班级（v0.5.4 起两种详情同构返回）
  problems?: ContestProblem[]
  edit_problems?: { id: number; title: string; score: number; hidden?: boolean }[] // 组织者编辑用完整清单
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
  errors: number
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

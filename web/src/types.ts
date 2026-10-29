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

// 统一选题器（v0.7.3 ProblemPicker）的已选条目：带标题保序，hidden 为组织者视角
// edit_problems 里保留的隐藏题标记（候选列表一律排除隐藏题）
export interface PickedProblem {
  id: number
  title: string
  hidden?: boolean
}

export interface ProblemListItem {
  id: number
  title: string
  hidden?: boolean
  has_cases?: boolean // 无测试数据空壳题（v0.6.1 验收反馈）：题库列表「无数据」小提示
  difficulty?: number // 难度 1–10（v0.11；缺省展示按 D3）
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
  type?: string // 题型（v0.9）：空 = 传统题；objective / function / interactive 显示徽章
  kind?: string // 客观题小类（single/multiple/judge/blank），仅 objective 时返回
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

// ---------- 来源库（v0.12） ----------

// 公开来源元数据（GET /sources/meta）：建题表单 datalist 建议名 + 题库筛选年份选项
export interface SourceMetaResult {
  names: string[]
  years: number[]
}

// root 来源面板条目（GET /admin/sources）：引用数按题目侧实时聚合
export interface SourceEntry {
  id: number
  name: string
  type: string // 空 = 未分类
  refs: number
}

// ---------- 批量导入（v0.15） ----------

// 单题导入结果（结果流条目）
export interface BatchImportResult {
  name: string
  status: 'ok' | 'skipped' | 'failed'
  pid?: number
  title?: string
  reason?: string
}

// 导入任务进度快照（GET /admin/problems/import/status）
export interface BatchImportStatus {
  state: 'idle' | 'running' | 'done' | 'cancelled' | 'failed'
  /** v0.18.2 导入阶段：extracting = 解压+识别中（total 未定，前端出不定态提示）| importing = 逐题导入 */
  phase?: 'extracting' | 'importing'
  /** v0.18.2 解压实时进度（字节；extracting 期有值，前端换算百分比） */
  extract_done?: number
  extract_total?: number
  total: number
  done: number
  ok_count: number
  fail_count: number
  skip_count: number
  /** 后端恒返回数组（v0.18.2 修复）；类型上保留可空以防御旧后端/异常帧 */
  results?: BatchImportResult[] | null
  error?: string
  by?: string
  started_at?: string
  ended_at?: string
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

// 客观题小类（v0.9）
export type ObjectiveKind = 'single' | 'multiple' | 'judge' | 'blank'

// 客观题单小题对外视图（GetProblem questions；不含答案/解析）
export interface ObjectiveQuestionView {
  index: number
  stem: string
  kind: ObjectiveKind
  options: string[]
  blank_count: number
  score: number
}

// 客观题整卷完整定义（满分∨老师门槛下的 questions_full；编辑回填也用它）
export interface ObjectiveQuestionFull {
  stem?: string
  kind: ObjectiveKind
  options?: string[]
  answer?: number[]
  blanks?: { accept: string[]; case_sensitive?: boolean }[]
  explanation?: string
  score?: number
}

// 客观题单小题编辑草稿（建题/编辑表单 → question_json）
export interface ObjectiveQuestionDraft {
  stem: string
  kind: ObjectiveKind
  options: string[]
  answer: number[]
  blanks: { accept: string[]; case_sensitive?: boolean }[]
  explanation: string
  score: number
}

// 客观题整卷作答（提交用）
export interface ObjectiveAnswer {
  answer?: number[]
  blanks?: string[]
}

export interface ProblemDetail {
  id: number
  title: string
  description: string // 题面（Markdown，v0.8 起单一事实源）
  description_html?: string // 渲染缓存（v0.30 写入时渲染；空 = 回退 md 客户端渲染）
  type?: string // 题目类型（v0.9）：空/default = 传统题；objective = 客观题；function = 函数题
  questions?: ObjectiveQuestionView[] // 客观题整卷（脱敏视图）
  questions_full?: ObjectiveQuestionFull[] // 完整定义（满分∨老师门槛）
  function?: { has_driver: boolean; has_template: boolean; template_code?: string } // 函数题（v0.9.1）
  interactive?: { has_interactor: boolean } // 交互题（v0.9.2）
  time_limit_ms: number
  memory_limit_kb: number
  difficulty?: number // 难度 1–10（v0.11；缺省展示按 D3）
  meta_tags?: number[] // 来源标签树节点 ID（2026-10-25 双树：地区/赛事/题库/年份/其他）
  compare?: string
  float_tolerance?: number
  has_checker?: boolean
  has_cases?: boolean // 无测试数据空壳题（v0.6.1 验收反馈）：详情提示 + 禁提交
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
  challenge?: { eligible: boolean; sec: number } // 挑战模式（v0.23）：资格 + 生效时长（服务端权威）
  challenge_time_sec?: number // 原始覆盖配置（编辑回填；0/缺省 = 按难度默认）——仅编辑者下发
  video_url?: string // 答案解析视频原始链接（编辑回填）——仅编辑者下发
  solution_video_url?: string // 答案解析视频（展示侧，满分∨老师门槛；无链接缺省）
  discuss_code_threshold?: number // 题内讨论代码门槛覆盖（编辑回填；0 = 跟随全局）——仅编辑者下发
}

// 挑战模式（v0.23）
export interface ChallengeStartResult {
  id: number
  started_at: number // Unix ms
  deadline_at: number
  sec: number
}

export interface ChallengeSessionView {
  id: number
  status: 'running' | 'ac' | 'timeout' | 'quit'
  started_at: number
  deadline_at?: number // running 态
  remaining_ms?: number
  finished_at?: number
  duration_ms?: number // AC 态
  chars_per_sec?: number
}

// 题内榜单行（v0.23 三榜）
export interface BoardRow {
  rank: number
  uid: number
  nickname: string
  username?: string
  value?: number // 用时榜 ms / 内存榜 KB
  duration_ms?: number // 挑战榜
  chars_per_sec?: number
  lang: string
  submission_id?: number
  created_at: number
}

export interface BoardsResult {
  can_view_boards: boolean // 时间/内存榜门槛（满分∨特权）；挑战榜恒登录可见
  challenge: BoardRow[] | null
  time: BoardRow[] | null
  mem: BoardRow[] | null
}

// 自测直跑结果（v0.23）
export interface SelfTestResult {
  status: string
  stdout: string
  stderr: string
  time_ms: number
  mem_kb: number
  truncated: boolean
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
  answers?: ObjectiveAnswer[] // 客观题整卷作答（v0.9.3，与代码同门槛回传）
  is_objective?: boolean
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
  level?: number // 学生等级 1–10（v0.11；聚合失败省略）
}

// 选人筛选用账号条目（GET /users/lookup；提交记录/所有代码的「选手」下拉）
export interface LookupUser {
  id: number
  username: string
  nickname: string
}

// ---------- 用户公开主页（v0.17，GET /users/:id/profile） ----------

// 首次 AC 条目：服务端全量下发（隐藏题已按观察者权限剔除），时间窗前端本地切
export interface UserPageFirstAC {
  pid: number
  title: string
  difficulty: number
  hidden?: boolean
  first_ac_at: string
}

// 近期提交（published 投影，不带代码/明细；点击进提交详情弹窗走既有权限）
export interface UserPageRecent {
  id: number
  pid: number
  title: string
  status: string
  score: number
  lang?: string
  created_at: string
}

// 比赛历史行：status = 场次时段；result 仅结束后出现——
// hidden = 隐藏期未过（成绩未公布），published = 已公布（rank/score 随之下发）
export interface UserPageContest {
  id: number
  title: string
  status: 'scheduled' | 'running' | 'ended'
  start: string
  end: string
  result?: 'hidden' | 'published'
  rank?: number
  score?: number
  total_users?: number
}

// 主页聚合数据（本人与他人统一视图，无私有区块）
export interface UserPage {
  id: number
  username: string
  nickname: string
  created_at: string
  school?: string
  grade?: string
  region?: string // 地区（v0.23；公开开关开时下发）
  gender?: string // 资料公开开关开时下发（v0.23）
  qq?: string
  avatar_url?: string // QQ 头像直链（绑定开关开 + 已填 QQ 时下发；缺省回退首字头像）
  level?: LevelBadgeInfo
  rank?: { rank: number; solved: number } // 全站排名（同排行榜口径）
  stats: {
    submissions: number
    accepted: number
    ac_rate: number
    answers_accepted?: number // 讨论区被采纳解答数（v0.24；无记录缺省）
  }
  recent?: UserPageRecent[]
  first_ac?: UserPageFirstAC[]
  contests?: UserPageContest[]
}

// ---------- 等级体系（v0.11） ----------

// 等级配置公开视图（GET /api/levels）：徽章与进度渲染所需
export interface LevelConfigInfo {
  names: string[] // Lv.1–10 名称文字
  difficulty_names?: string[] // 难度 1–10 档展示名（缺省与等级名相同）
  difficulty_notes?: string[] // 每档难度各一条说明（题库说明卡逐档展示；空档不显示）
  thresholds: number[] // 达 Lv.2–10 的累计 XP（9 项）
  decay: [number, number, number] // 低 1/2/3 级题分系数（低 ≥4 级恒 0）
  effects_enabled: boolean
  effects_tiers: boolean[]
  autocomplete_min_level?: number // 代码自动补全解锁等级（缺省按 5）
}

// root 等级面板（GET/PUT /admin/levelconfig）
export interface AdminLevelConfig {
  names: string[]
  difficulty_names: string[]
  difficulty_notes: string[]
  thresholds: number[]
  decay1: number
  decay2: number
  decay3: number
  effects_enabled: boolean
  effects_tiers: boolean[]
}

// /me/profile 附带的当前用户等级信息（聚合失败省略）
export interface LevelBadgeInfo {
  level: number
  xp: number
  next_xp?: number
}

// 管理端视角的用户等级信息（含经验调整合计，可负）
export interface UserLevelInfo {
  level: number
  xp: number
  next_xp?: number
  adjust?: number
}

export interface ClassCard {
  id: number
  name: string
  intro: string
  owner_id: number
  owner_name: string // 建班人昵称（空回退用户名）——建班人视角卡片显示「xxx的班」
  member_count: number
  assignment_count: number
  private: boolean
  created_at: string
  joined: boolean
  pinned?: boolean // 当前用户是否置顶（个人视角 marks；仅已入班可置顶/拖动）
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
  difficulty?: number // 难度档（2026-10-27 作业题目表新增难度列；缺省前端按 D3 兜底）
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
  intro: string // 作业说明（Markdown，v0.8 起单一事实源）
  intro_html?: string // 渲染缓存（v0.30 写入时渲染；空 = 回退 md 客户端渲染）
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
  level?: number // 学生等级（v0.11；聚合失败省略）
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
  intro: string // 班级公告（Markdown，v0.8 起单一事实源）
  intro_html?: string // 渲染缓存（v0.30）
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
  locked_until?: string // 锁定中（v0.14 登录失败锁定）才有值，ISO 时间
}

// ---------- 安全日志（v0.14，root 后台） ----------

export interface SecurityLogItem {
  at: string
  type: string // login_success | login_failure | login_locked | password_change | password_reset_admin | rate_limited
  uid: number
  username: string
  ip: string
  ua: string
  detail: string
}

// 重复题扫描（v0.18.1）：同「标题+题面」的疑似重复组，只列出不自动删
export interface DuplicateGroup {
  title: string
  preview: string // 题面前 80 字
  problems: {
    id: number
    title: string
    type: string
    hidden: boolean
    created_by: number
    created_at: string
  }[]
}

// 种子题管理（后台面板）：种子目录 ↔ 库内对账与清除/重部署结果
export interface SeedRow {
  dir: string // 种子目录名
  title: string // 期望标题（problem.yaml title > 目录名回退）
  exists: boolean // 库内已部署
  pid?: number // 已部署时的库内题号
  db_title?: string // 已部署时的实际标题
  submissions: number // 该题现有提交数（清除将一并删除）
}

export interface SeedOutcome {
  dir: string
  status: 'imported' | 'skipped' | 'failed'
  pid?: number
  title?: string
  reason?: string
}

export interface SeedClearStats {
  problems: number
  pids: number[]
  submissions: number
  challenges: number
  solutions: number
  discussions: number
  replies: number
}

export interface SeedStatus {
  seed_dir: string
  marked: boolean
  rows: SeedRow[]
}

// ---------- 题单（v0.18.3，GET /lists*） ----------

// 章节树节点（提交/存储形态）：题挂叶子、章最多两层、单章不混挂
export interface ListEntry {
  title: string
  desc?: string
  problems?: number[]
  children?: ListEntry[]
}

// 编辑器草稿节点：题带标题（保序展示与拖拽）
export interface ListEntryDraft {
  title: string
  desc: string
  problems: PickedProblem[]
  children: ListEntryDraft[]
}

export interface ListCard {
  id: number
  title: string
  intro: string
  hidden?: boolean
  created_at: string
  created_by: number
  chapter_count: number
  problem_count: number
  solved: number
  difficulties: number[]
  participants: number
  manageable: boolean
}

// 详情条目：pid + 摘要 + 我的最高分状态
export interface ListProblemEntry {
  pid: number
  title: string
  difficulty: number
  hidden?: boolean
  locked?: boolean
  lock_kind?: string
  status?: string
  score?: number
  tags?: number[]
}

export interface ListSection {
  no: string
  title: string
  desc?: string
  problems: ListProblemEntry[]
}

export interface ListChapter {
  no: string
  title: string
  desc?: string
  problems: ListProblemEntry[]
  sections: ListSection[]
}

export interface ListDetail {
  id: number
  title: string
  intro: string
  intro_html?: string // 渲染缓存（v0.30）
  hidden?: boolean
  created_at: string
  manageable: boolean
  total: number
  solved: number
  difficulties: number[]
  chapters: ListChapter[]
}

export interface ListRankItem {
  rank: number
  uid: number
  username: string
  nickname: string
  solved: number
  level?: number
}

export interface ListRankResult {
  total: number
  page: number
  page_size: number
  items: ListRankItem[]
  me: { rank: number; solved: number } | null
}

// 作业「从题单复制」拍平结果（隐藏题一律跳过并计数）
export interface ListFlat {
  title: string
  items: { id: number; title: string }[]
  skipped_hidden: number
}

// ---------- 个人设置（v0.4.8） ----------

// 用户偏好：跟随账号存储，前端消费（默认语言/代码模板/MD 编辑模式）
export interface UserPrefs {
  defaultLang?: string
  codeTemplates?: Record<string, string>
  autocomplete?: boolean // 代码自动补全（v0.11 起需等级 ≥ 系统设置阈值，默认 Lv.5）
  codeFont?: string // 编辑器字体：Consolas / Menlo / Monaco（默认 Consolas）
  codeFontSize?: number // 编辑器字号 px（12-48，默认 14）
  mdEditMode?: 'wysiwyg' | 'ir' | 'sv' | '' // Markdown 编辑器默认模式（空 = wysiwyg）
  mdTheme?: 'light' | 'ant-design' | 'dark' | 'wechat' | '' // Markdown 内容主题（空 = light）
}

// 本人视角的个人信息（仅本人与管理员后台可见，不进任何公开接口）
export interface UserProfile {
  nickname: string
  can_edit_nickname: boolean // 当前角色是否可自助改昵称（后端按生效权限位现算）
  gender: string
  school: string
  grade: string
  region: string
  qq: string
  profile_vis: { gender: boolean; school: boolean; grade: boolean; region: boolean; qq: boolean } // 资料公开开关（v0.23）
  qq_avatar: boolean // QQ 头像绑定开关（v0.23）
  avatar_url?: string // QQ 头像直链（绑定开关开 + 已填 QQ 时下发；顶栏/主页共用）
  prefs: UserPrefs
  level?: LevelBadgeInfo // 学生等级（v0.11；顶栏徽章与补全门槛提示用）
}

// ---------- 题解（v0.5.2-1） ----------

export interface SolutionItem {
  id: number
  uid: number
  nickname: string
  /** 题解正文（Markdown，单一事实源）。列表不携带（快赢批⑥ 去双发），编辑经
   * getSolution 详情接口按需取；MdView 回退渲染场景仍可能出现 */
  content?: string
  content_html?: string // 渲染缓存（v0.30）；列表查看端的主渲染源
  pinned: boolean
  created_at: string
  updated_at: string
}

// ---------- 备份（v0.4.9） ----------

// 备份归档下载接口路径（v0.26 任务化取包通道：调用方引用常量名即可）
export const BACKUP_DOWNLOAD_URL = '/api/admin/backup/download'

export interface BackupJobState {
  state: 'running' | 'done' | 'failed' | 'cancelled'
  tier: string
  error: string | null
  file: string | null
  started_at: string
  ended_at: string | null
}

export interface BackupStatus {
  mode: 'uninitialized' | 'tier0' | 'tier1'
  last_backup_at: string | null
  last_backup_file: string | null
  last_db_at?: string | null // 分层备份（v0.15.1）：每日库备最近一次
  last_db_file?: string | null
  last_media_at?: string | null // 每周数据卷备最近一次
  last_media_file?: string | null
  job?: BackupJobState | null // 当前/最近一次备份任务（v0.26；null = 尚未跑过）
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
  contest_id?: number // 考试题被主题库比赛收录时返回：入口引导进比赛做题页（比赛优先级）
  submitted?: boolean // 当前用户在本场次是否提交过（登录才返回）
  status?: string // 当前用户本场次最高分提交状态（结果可见时返回）
  my_score?: number
  tags?: number[] // 知识点节点 ID（结果隐藏或赛时隐藏难度/知识点时不返回，前端渲染横杠）
  difficulty?: number // 难度档（同上，隐藏时不返回）
  submit_count?: number // 通过率（公开态提交；结果隐藏时不返回）
  accepted_count?: number
  result_hidden?: boolean // 该题结果/状态/标签/通过率是否对本视角隐藏
}

// ---------- 比赛/考试打印试卷（v0.10） ----------

export interface PaperQuestion {
  kind: ObjectiveKind
  stem: string
  options?: string[]
  blank_count: number
}

export interface PaperProblem {
  id: number
  title: string
  type?: string // 空 = 传统题；objective = 客观题
  description: string
  description_html?: string // 渲染缓存（v0.30）
  questions?: PaperQuestion[] // 客观题公开字段（无答案/解析/分值）
  file_io_input?: string // 比赛/考试开启文件 IO 时的英文文件名
  file_io_output?: string
  meta_tags?: number[] // 来源标签树节点 ID（2026-10-25 双树，打印带来源）
}

export interface PaperData {
  id: number
  title: string
  kind: 'contest' | 'exam' | 'problem'
  single?: boolean // 单题试卷（不显示 T1 标题行）
  file_io?: boolean
  problems: PaperProblem[]
}

export interface ContestDetail {
  id: number
  title: string
  intro: string // 赛事说明（Markdown，v0.8 起单一事实源）
  intro_html?: string // 渲染缓存（v0.30）
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
  hide_meta?: boolean // 2026-10-27：发布前对参赛者隐藏难度与知识点（比赛默认开）
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
  level?: number // 学生等级（v0.11；聚合失败省略）
  problems: LeaderCell[]
}

// ---------- 讨论区 + 站内通知（v0.24） ----------

export type DiscussScope = 'problem' | 'contest' | 'global'

export interface DiscussTopic {
  id: number
  scope: DiscussScope
  scope_id: number
  uid: number
  kind: string
  title: string
  content?: string
  related_pids?: number[]
  list_id?: number
  tag_id?: number
  reply_count: number
  like_count: number
  pinned?: boolean
  featured?: boolean
  locked?: boolean
  folded?: boolean
  official?: boolean
  private?: boolean
  public_at?: string
  accepted_reply_id?: number
  edited?: boolean
  deleted?: boolean
  created_at: string
  last_active_at: string
  liked?: boolean
  nickname?: string
  username?: string
  can_edit?: boolean
  can_manage?: boolean
  can_accept?: boolean
  is_organizer?: boolean
}

export interface DiscussReply {
  id: number
  tid: number
  uid: number
  content: string
  reply_to: number
  like_count: number
  accepted?: boolean
  folded?: boolean
  deleted?: boolean
  edited?: boolean
  created_at: string
  liked?: boolean
  nickname?: string
  username?: string
}

export interface DiscussListResult {
  list: DiscussTopic[]
  total: number
  enabled?: boolean
  disabled_reason?: string
  code_threshold?: number
  can_view_code?: boolean
  locked?: boolean
  running?: boolean
  ended?: boolean
  is_organizer?: boolean
  title?: string
}

export interface DiscussTopicDetail {
  topic: DiscussTopic
  replies: DiscussReply[]
  /** 顶层楼总数/当前页/总页数（v0.26 主楼分页；楼中楼随本页主楼带出） */
  reply_total?: number
  reply_page?: number
  reply_pages?: number
  gate?: { code_threshold: number; can_view_code: boolean }
}

export interface NotificationItem {
  id: number
  type: string
  title: string
  body: string
  link?: string
  agg_count: number
  read?: boolean
  created_at: string
}

export interface NotifyPrefsResult {
  categories: { type: string; enabled: boolean }[]
  has_email: boolean
}

/** 后台系统设置（GET/PUT /admin/settings 全量视图）。v0.29 收敛：原 api/index.ts 三份
 * 手抄内联类型已漂移（put 返回漏 v0.24 讨论区/通知字段）——单一事实源在此，
 * 局部保存用 Partial&lt;AdminSettings&gt;。 */
export interface AdminSettings {
  rate_limit_per_minute: number
  home_notice: string
  data_download_daily_limit: number
  autocomplete_min_level: number
  allow_register: boolean
  icp_number: string
  batch_import_problem_mb: number
  batch_import_zip_mb: number
  /** 邮箱通道（v0.16） */
  mail_channel: 'log' | 'smtp'
  smtp_host: string
  smtp_port: number
  smtp_username: string
  smtp_password: string
  smtp_from: string
  register_verify_mode: 'none' | 'email'
  password_reset_enabled: boolean
  /** 讨论区与通知（v0.24） */
  discuss_global_enabled: boolean
  discuss_problem_enabled: boolean
  discuss_contest_enabled: boolean
  discuss_global_locked: boolean
  discuss_code_threshold: number
  discuss_topic_daily_limit: number
  discuss_reply_interval_sec: number
  discuss_newbie_hours: number
  discuss_report_fold: number
  discuss_accept_xp: number
  notify_retention_days: number
  notify_broadcast_per_day: number
  user_seq_start: number
  problem_seq_start: number
}

export interface DiscussReportItem {
  id: number
  target_type: 'topic' | 'reply'
  target_id: number
  tid: number
  reason: string
  status: string
  at: string
  action?: string
  reporter?: { uid: number; nickname: string; username: string }
  snippet?: string
  target_uid?: number
}

export interface DiscussMuteItem {
  id: number
  uid: number
  nickname?: string
  username?: string
  until: string
  permanent: boolean
  reason: string
  by?: string
  at: string
}

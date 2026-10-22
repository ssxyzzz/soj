import axios from 'axios'
import { ElMessage } from 'element-plus'

import { useUserStore } from '@/stores/user'
import type {
  AdminUser,
  ApiResponse,
  AssignmentDetail,
  AssignmentListItem,
  ClassCard,
  ClassDetail,
  ContestDetail,
  ContestListItem,
  ContestMySubmission,
  ExamListItem,
  LeaderRow,
  LevelConfigInfo,
  AdminLevelConfig,
  LoginResult,
  UserLevelInfo,
  ObjectiveQuestionFull,
  PaperData,
  PermGroupInfo,
  PermInfo,
  SolutionItem,
  BackupStatus,
  BatchImportStatus,
  SourceEntry,
  SourceMetaResult,
  UserProfile,
  UserPrefs,
  ProblemDetail,
  ProblemListResult,
  ProgressRow,
  RankItem,
  RoleInfo,
  SecurityLogItem,
  Submission,
  SubmissionListResult,
  AdminTagTreeResult,
  TagNode,
  TagTreeResult,
} from '@/types'

const client = axios.create({ baseURL: '/api', timeout: 20000 })

client.interceptors.request.use((config) => {
  // token 取自 pinia store（每标签页一份内存态），不直接读 localStorage：
  // localStorage 全标签页共享，B 标签页登录会覆盖 token，导致 A 标签页界面仍是
  // 原会话、请求却带上了 B 的身份（多标签页串号，权限类操作 403）
  const token = useUserStore().token
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

client.interceptors.response.use(
  (resp) => {
    const body = resp.data as ApiResponse<unknown>
    if (body && typeof body.code === 'number' && body.code !== 0) {
      const message = body.message || '请求失败'
      ElMessage.error(message)
      // 附加后端 failData 携带的明细（如标签树保存被拒时的引用题目清单）
      const err = new Error(message) as Error & { data?: unknown }
      err.data = body.data
      return Promise.reject(err)
    }
    return resp
  },
  (error) => {
    if (error.response?.status === 401) {
      useUserStore().logout() // 清本标签页内存态与 localStorage（角色/权限一并清）
      if (!location.pathname.startsWith('/login')) {
        location.href = '/login'
      }
    } else {
      ElMessage.error(error.response?.data?.message || error.message || '网络错误')
    }
    // 与成功分支同构：把 failData 的 data 挂到 error 上（如隐藏期 404 携带的锁信息，供重定向）
    const err = error as Error & { data?: unknown }
    err.data = error.response?.data?.data
    return Promise.reject(err)
  },
)

async function get<T>(url: string, params?: Record<string, unknown>): Promise<T> {
  const resp = await client.get<ApiResponse<T>>(url, { params })
  return resp.data.data
}

async function post<T>(url: string, body?: unknown): Promise<T> {
  const resp = await client.post<ApiResponse<T>>(url, body)
  return resp.data.data
}

async function put<T>(url: string, body?: unknown): Promise<T> {
  const resp = await client.put<ApiResponse<T>>(url, body)
  return resp.data.data
}

async function patch<T>(url: string, body?: unknown): Promise<T> {
  const resp = await client.patch<ApiResponse<T>>(url, body)
  return resp.data.data
}

async function del<T>(url: string): Promise<T> {
  const resp = await client.delete<ApiResponse<T>>(url)
  return resp.data.data
}

export const authApi = {
  login: (username: string, password: string) =>
    post<LoginResult>('/auth/login', { username, password }),
  register: (username: string, password: string, nickname: string) =>
    post<LoginResult>('/auth/register', { username, password, nickname }),
  me: () => get<{ id: number; username: string; nickname: string; role: string; perms: string[] }>('/me'),
  // 本人修改密码（v0.14）：验旧密码后落库并 pwdVer+1，全部登录态（含当前）立即失效，
  // 成功后前端主动登出跳登录页；新密码二次确认在 UI 层做
  changePassword: (oldPassword: string, newPassword: string) =>
    post<{ changed: boolean }>('/auth/password', { old_password: oldPassword, new_password: newPassword }),
  // 个人设置（v0.4.8）：本人自助字段 + 偏好；昵称额外受 user.nickname.edit 权限位约束
  getProfile: () => get<UserProfile>('/me/profile'),
  updateProfile: (body: {
    nickname?: string
    gender?: string
    school?: string
    grade?: string
    qq?: string
    prefs?: UserPrefs
  }) => patch<UserProfile>('/me/profile', body),
}

// 站点公开信息（v0.13 收编原 /auth/register-open）：注册开关 + ICP 备案号，
// 登录页页签显隐与页脚悬挂共用；后续公开站点事实（v0.15 注册验证方式等）并入此处
export const siteApi = {
  info: () => get<{ register_open: boolean; icp_number: string }>('/site'),
}

export const problemApi = {
  list: (
    page: number,
    pageSize: number,
    q?: string,
    tags?: number[],
    difficulty?: number[],
    sourceType?: string[],
    year?: number[],
  ) =>
    get<ProblemListResult>('/problems', {
      page,
      page_size: pageSize,
      ...(q ? { q } : {}),
      ...(tags && tags.length ? { tags: tags.join(',') } : {}),
      ...(difficulty && difficulty.length ? { difficulty: difficulty.join(',') } : {}),
      ...(sourceType && sourceType.length ? { source_type: sourceType.join(',') } : {}),
      ...(year && year.length ? { year: year.join(',') } : {}),
    }),
  detail: (id: number) => get<ProblemDetail>(`/problems/${id}`),
  paper: (id: number) => get<PaperData>(`/problems/${id}/paper`),
  create: (form: FormData) =>
    client
      .post<ApiResponse<{ id: number }>>('/problems', form, { timeout: 0 })
      .then((r) => r.data.data),
  updateMeta: (id: number, body: Record<string, unknown>) =>
    patch<{ updated: number }>(`/problems/${id}/meta`, body), // 客观题编辑带 question_json（v0.9）
  parseObjectiveYaml: (yaml: string) =>
    post<{ questions: ObjectiveQuestionFull[] }>('/objective/questions/parse', { yaml }),
  uploadChecker: (id: number, form: FormData) =>
    client.post(`/problems/${id}/checker`, form, { timeout: 0 }).then((r) => r.data.data),
  removeChecker: (id: number) => del<{ removed: boolean }>(`/problems/${id}/checker`),
  uploadFunctionFile: (id: number, file: 'driver' | 'template', form: FormData) =>
    client.post(`/problems/${id}/function/${file}`, form, { timeout: 0 }).then((r) => r.data.data),
  removeFunctionTemplate: (id: number) => del<{ removed: boolean }>(`/problems/${id}/function/template`),
  uploadInteractor: (id: number, form: FormData) =>
    client.post(`/problems/${id}/interactor`, form, { timeout: 0 }).then((r) => r.data.data),
  solutions: (id: number, page: number, pageSize: number) =>
    get<{ list: SolutionItem[]; total: number; can_manage: boolean }>(`/problems/${id}/solutions`, {
      page,
      page_size: pageSize,
    }),
  postSolution: (id: number, md: string) =>
    post<{ id: number }>(`/problems/${id}/solutions`, { content: md }),
  updateSolution: (id: number, sid: number, md: string) =>
    patch<{ updated: number }>(`/problems/${id}/solutions/${sid}`, { content: md }),
  deleteSolution: (id: number, sid: number) =>
    del<{ deleted: number }>(`/problems/${id}/solutions/${sid}`),
  pinSolution: (id: number, sid: number, pinned: boolean) =>
    patch<{ pinned: boolean }>(`/problems/${id}/solutions/${sid}/pinned`, { pinned }),
  setHidden: (id: number, hidden: boolean) =>
    patch<{ id: number; hidden: boolean }>(`/problems/${id}/hidden`, { hidden }),
  // 整包替换测试数据（编辑题目）：旧测试点全部删除，按新 zip 重建
  replaceTestdata: (id: number, form: FormData) =>
    client.post(`/problems/${id}/testdata`, form, { timeout: 0 }).then((r) => r.data.data),
  submissions: (id: number, page: number, pageSize: number, sort?: string, lang?: string) =>
    get<SubmissionListResult>(`/problems/${id}/submissions`, {
      page,
      page_size: pageSize,
      ...(sort ? { sort } : {}),
      ...(lang ? { lang } : {}),
    }),
  userSubmissions: (id: number, pid: number, uid: number) =>
    get<{ list: Submission[] }>(`/contests/${id}/problems/${pid}/submissions`, { uid }),
  mySubmissions: (id: number, page: number, pageSize: number) =>
    get<SubmissionListResult>(`/problems/${id}/my-submissions`, { page, page_size: pageSize }),
  setStatement: (id: number, md: string) =>
    patch<{ id: number }>(`/problems/${id}/statement`, { md }),
}

// 标签树（v0.5.5）：读公开（题库/详情/表单共用），写仅 root（后端 RequireRoot）
export const tagApi = {
  tree: () => get<TagTreeResult>('/tags/tree'),
  adminTree: () => get<AdminTagTreeResult>('/admin/tagtree'),
  save: (nodes: TagNode[]) => put<{ count: number }>('/admin/tagtree', { nodes }),
  // 保存被删拦截后的解绑（一键全部/逐题）：从题目上摘除待删标签，其余标签不动
  unbind: (problemIds: number[], tagIds: number[]) =>
    post<{ unbound: number }>('/admin/tagtree/unbind', { problem_ids: problemIds, tag_ids: tagIds }),
}

// 来源库（v0.12）：元数据公开读（表单 datalist + 题库筛选），管理写仅 root
export const sourcesApi = {
  meta: () => get<SourceMetaResult>('/sources/meta'),
  adminList: () => get<{ list: SourceEntry[] }>('/admin/sources'),
  create: (name: string, type: string) => post<{ id: number }>('/admin/sources', { name, type }),
  update: (id: number, body: { name?: string; type?: string }) =>
    patch<{ updated: number }>(`/admin/sources/${id}`, body),
  remove: (id: number) => del<{ deleted: number }>(`/admin/sources/${id}`),
  toTag: (id: number, body: { node_id?: number; parent_id?: number; name?: string }) =>
    post<{ node_id: number; updated: number }>(`/admin/sources/${id}/to-tag`, body),
}

// 题包导入（v0.15 统一格式）：独立 /import 页按来源（soj|hydro）适配；problem.create 权限
export const importApi = {
  upload: (form: FormData) =>
    client
      .post<ApiResponse<{ started: boolean; source: string }>>('/import', form, { timeout: 0 })
      .then((r) => r.data.data),
  status: () => get<BatchImportStatus>('/import/status'),
  cancel: () => post<{ cancel_requested: boolean }>('/import/cancel'),
}

export const submissionApi = {
  create: (problemId: number, language: string, code: string) =>
    post<{ id: number }>('/submissions', { problem_id: problemId, language, code }),
  // 客观题提交（v0.9）：作答即判，免语言/代码
  createObjective: (problemId: number, answers: import('@/types').ObjectiveAnswer[]) =>
    post<{ id: number }>('/submissions', { problem_id: problemId, answers }),
  get: (id: number) => get<Submission>(`/submissions/${id}`),
  rejudge: (id: number) => post<{ id: number }>(`/submissions/${id}/rejudge`, {}),
  list: (params: { page: number; page_size: number; problem_id?: number }) =>
    get<SubmissionListResult>('/submissions', { ...params }),
}

export const rankApi = {
  top: () => get<RankItem[]>('/rank'),
}

// 等级体系（v0.11）：公开读配置（排行榜/顶栏徽章渲染）
export const levelsApi = {
  config: () => get<LevelConfigInfo>('/levels'),
}

export const classApi = {
  list: () => get<ClassCard[]>('/classes'),
  detail: (id: number) => get<ClassDetail>(`/classes/${id}`),
  create: (name: string, intro: string, priv = false) =>
    post<{ id: number; name: string; invite_code: string }>('/classes', { name, intro, private: priv }),
  join: (id: number, code: string) => post<{ joined: boolean }>(`/classes/${id}/join`, { code }),
  joinByCode: (code: string) =>
    post<{ joined: boolean; class_id: number; name: string }>('/classes/join-by-code', { code }),
  leave: (id: number) => post<{ left: boolean }>(`/classes/${id}/leave`),
  setPrivate: (id: number, priv: boolean) => patch<{ private: boolean }>(`/classes/${id}/private`, { private: priv }),
  regenInvite: (id: number) => post<{ invite_code: string }>(`/classes/${id}/invite`),
  importMembers: (id: number, usernames: string) =>
    post<{ added: number; total: number }>(`/classes/${id}/members`, { usernames }),
  setMemberRole: (id: number, uid: number, role: string) =>
    post<{ updated: number; role: string }>(`/classes/${id}/members/${uid}/role`, { role }),
  listAssignments: (id: number) =>
    get<{ visible: boolean; list: AssignmentListItem[] }>(`/classes/${id}/assignments`),
}

// ---------- 作业（v0.4.x） ----------

export const assignmentApi = {
  create: (classId: number, title: string, intro: string) =>
    post<{ id: number; title: string }>(`/classes/${classId}/assignments`, { title, intro }),
  detail: (id: number) => get<AssignmentDetail>(`/assignments/${id}`),
  update: (id: number, body: { title?: string; intro?: string }) =>
    patch<{ updated: number }>(`/assignments/${id}`, body),
  remove: (id: number) => del<{ deleted: number }>(`/assignments/${id}`),
  setProblems: (id: number, pids: number[]) =>
    put<{ count: number }>(`/assignments/${id}/problems`, { pids }),
  progress: (id: number) => get<ProgressRow[]>(`/assignments/${id}/progress`),
  userSubmissions: (id: number, pid: number, uid: number) =>
    get<{ list: Submission[] }>(`/assignments/${id}/problems/${pid}/submissions`, { uid }),
}

export const adminApi = {
  listUsers: (page: number, pageSize: number, q?: string, role?: string) =>
    get<{ list: AdminUser[]; total: number }>('/admin/users', {
      page,
      page_size: pageSize,
      ...(q ? { q } : {}),
      ...(role ? { role } : {}),
    }),
  createUser: (username: string, password: string, nickname: string, role: string) =>
    post<{ id: number }>('/admin/users', { username, password, nickname, role }),
  updateUser: (id: number, patchBody: { role?: string; password?: string; nickname?: string; username?: string }) =>
    patch<{ updated: number }>(`/admin/users/${id}`, patchBody),
  getSettings: () =>
    get<{
      rate_limit_per_minute: number
      home_notice: string
      backup_guide: string
      data_download_daily_limit: number
      autocomplete_min_level: number
      allow_register: boolean
      icp_number: string
      batch_import_problem_mb: number
      batch_import_zip_mb: number
    }>('/admin/settings'),
  updateSettings: (patch: {
    rate_limit_per_minute?: number
    home_notice?: string
    backup_guide?: string
    data_download_daily_limit?: number
    autocomplete_min_level?: number
    allow_register?: boolean
    icp_number?: string
    batch_import_problem_mb?: number
    batch_import_zip_mb?: number
  }) =>
    put<{
      rate_limit_per_minute: number
      home_notice: string
      backup_guide: string
      data_download_daily_limit: number
      autocomplete_min_level: number
      allow_register: boolean
      icp_number: string
      batch_import_problem_mb: number
      batch_import_zip_mb: number
    }>('/admin/settings', patch),
  // 等级面板（v0.11，root）
  getLevelConfig: () => get<{ config: AdminLevelConfig }>('/admin/levelconfig'),
  saveLevelConfig: (body: {
    names: string[]
    difficulty_names: string[]
    thresholds: number[]
    decay1: number
    decay2: number
    decay3: number
    effects_enabled: boolean
    effects_tiers: boolean[]
  }) => put<{ config: AdminLevelConfig }>('/admin/levelconfig', body),
  // 经验调整（用户管理界面，admin.user.manage 权限位）
  getUserLevel: (id: number) => get<UserLevelInfo>(`/admin/users/${id}/level`),
  adjustUserXp: (id: number, delta: number, reason: string) =>
    post<UserLevelInfo>(`/admin/users/${id}/xp-adjust`, { delta, reason }),
  // 安全日志（v0.14，root）：登录/锁定/改密/重置/限流事件，type 精确、q 按用户名/IP 模糊
  securityLogs: (page: number, pageSize: number, type?: string, q?: string) =>
    get<{ list: SecurityLogItem[]; total: number; types: string[] }>('/admin/security-logs', {
      page,
      page_size: pageSize,
      ...(type ? { type } : {}),
      ...(q ? { q } : {}),
    }),
  // 角色管理（后端硬编码 root，不可下放）
  listRoles: () =>
    get<{ list: RoleInfo[]; catalog: PermInfo[]; groups: PermGroupInfo[] }>('/admin/roles'),
  createRole: (id: string, name: string, permsList: string[]) =>
    post<{ id: string }>('/admin/roles', { id, name, perms: permsList }),
  updateRole: (id: string, body: { name?: string; perms?: string[] }) =>
    patch<{ id: string }>(`/admin/roles/${id}`, body),
  deleteRole: (id: string) => del<{ deleted: string }>(`/admin/roles/${id}`),
  // 备份面板（v0.4.9）：密文归档下载在 AdminView 内联处理（blob + 独立超时）
  backupStatus: () => get<BackupStatus>('/admin/backup'),
  backupOfflineKey: () => post<{ private_key: string }>('/admin/backup/offline-key'),
}

// ---------- 比赛（v0.4） ----------

export interface CreateContestPayload {
  class_id?: number
  title: string
  intro?: string
  start: string
  end: string
  hidden_days?: number
  problem_ids: number[]
  file_io?: boolean
  scoreboard?: boolean // v0.7：开启成绩表（默认 true）
  hide_during?: boolean // v0.7：赛期内对参赛者隐藏结果（默认 true）
}

// ---------- 比赛/考试（v0.5.4 ① 合并：10 对同构接口收敛为 roundApiOf(kind) 工厂） ----------

export type RoundKind = 'contest' | 'exam'

// roundApiOf 比赛/考试同构接口（路径 /contests|/exams，语义差异在后端 round.go）。
const roundApiOf = (kind: RoundKind) => {
  const base = `/${kind}s`
  return {
    detail: (id: number) => get<ContestDetail>(`${base}/${id}`),
    update: (id: number, body: { title?: string; start?: string; end?: string; intro?: string; problem_ids?: number[]; scoreboard?: boolean; hide_during?: boolean; file_io?: boolean }) =>
      patch<{ updated: number }>(`${base}/${id}`, body),
    remove: (id: number) => del<{ deleted: number }>(`${base}/${id}`),
    submit: (roundId: number, pid: number, language: string, code: string) =>
      post<{ id: number }>(`${base}/${roundId}/problems/${pid}/submissions`, { language, code }),
    submitObjective: (roundId: number, pid: number, answers: import('@/types').ObjectiveAnswer[]) =>
      post<{ id: number }>(`${base}/${roundId}/problems/${pid}/submissions`, { answers }),
    mySubmissions: (roundId: number) =>
      get<{ revealed: boolean; list: ContestMySubmission[] }>(`${base}/${roundId}/submissions/mine`),
    leaderboard: (roundId: number) => get<LeaderRow[]>(`${base}/${roundId}/leaderboard`),
    setFileIO: (roundId: number, enabled: boolean) =>
      patch<{ file_io: boolean }>(`${base}/${roundId}/file-io`, { enabled }),
    userSubmissions: (roundId: number, pid: number, uid: number) =>
      get<{ list: Submission[] }>(`${base}/${roundId}/problems/${pid}/submissions`, { uid }),
    paper: (roundId: number) => get<PaperData>(`${base}/${roundId}/paper`),
  }
}

export const contestApi = {
  list: () => get<ContestListItem[]>('/contests'),
  create: (payload: CreateContestPayload) => post<{ id: number }>('/contests', payload),
  ...roundApiOf('contest'),
}

// ---------- 考试（班级级赛事，v0.4.2） ----------

export const examApi = {
  listByClass: (classId: number) =>
    get<{ visible: boolean; list: ExamListItem[] }>(`/classes/${classId}/exams`),
  create: (classId: number, payload: CreateExamPayload) =>
    post<{ id: number }>(`/classes/${classId}/exams`, { ...payload, class_id: classId }),
  ...roundApiOf('exam'),
}

export interface CreateExamPayload {
  title: string
  intro?: string
  start: string
  end: string
  hidden_days?: number
  problem_ids: number[]
  file_io?: boolean
  scoreboard?: boolean // v0.7：开启成绩表（默认 true）
  hide_during?: boolean // v0.7：赛期内对参赛者隐藏结果（默认 true）
}

// ---------- 首页公告 ----------

export const noticeApi = {
  get: () => get<{ content: string }>('/notice'),
}

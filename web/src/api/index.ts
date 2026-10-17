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
  LoginResult,
  PermGroupInfo,
  PermInfo,
  SolutionItem,
  BackupStatus,
  UserProfile,
  UserPrefs,
  ProblemDetail,
  ProblemListResult,
  ProgressRow,
  RankItem,
  RoleInfo,
  Submission,
  SubmissionListResult,
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
    return Promise.reject(error)
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
  // 个人设置（v0.4.8）：真实姓名仅管理员可改（走 adminApi.updateUser），本人自助字段 + 偏好走这里
  getProfile: () => get<UserProfile>('/me/profile'),
  updateProfile: (body: {
    gender?: string
    school?: string
    grade?: string
    qq?: string
    prefs?: UserPrefs
  }) => patch<UserProfile>('/me/profile', body),
}

export const problemApi = {
  list: (page: number, pageSize: number, q?: string, tags?: number[]) =>
    get<ProblemListResult>('/problems', {
      page,
      page_size: pageSize,
      ...(q ? { q } : {}),
      ...(tags && tags.length ? { tags: tags.join(',') } : {}),
    }),
  detail: (id: number) => get<ProblemDetail>(`/problems/${id}`),
  create: (form: FormData) =>
    client
      .post<ApiResponse<{ id: number }>>('/problems', form, { timeout: 0 })
      .then((r) => r.data.data),
  updateMeta: (id: number, body: Record<string, unknown>) =>
    patch<{ updated: number }>(`/problems/${id}/meta`, body),
  uploadChecker: (id: number, form: FormData) =>
    client.post(`/problems/${id}/checker`, form, { timeout: 0 }).then((r) => r.data.data),
  removeChecker: (id: number) => del<{ removed: boolean }>(`/problems/${id}/checker`),
  solutions: (id: number, page: number, pageSize: number) =>
    get<{ list: SolutionItem[]; total: number; can_manage: boolean }>(`/problems/${id}/solutions`, {
      page,
      page_size: pageSize,
    }),
  postSolution: (id: number, content: string) =>
    post<{ id: number }>(`/problems/${id}/solutions`, { content }),
  updateSolution: (id: number, sid: number, content: string) =>
    patch<{ updated: number }>(`/problems/${id}/solutions/${sid}`, { content }),
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
  setStatement: (id: number, statement: string) =>
    patch<{ id: number }>(`/problems/${id}/statement`, { statement }),
}

// 标签树（v0.5f）：读公开（题库/详情/表单共用），写仅 root（后端 RequireRoot）
export const tagApi = {
  tree: () => get<TagTreeResult>('/tags/tree'),
  save: (nodes: TagNode[]) => put<{ count: number }>('/admin/tagtree', { nodes }),
}

export const submissionApi = {
  create: (problemId: number, language: string, code: string) =>
    post<{ id: number }>('/submissions', { problem_id: problemId, language, code }),
  get: (id: number) => get<Submission>(`/submissions/${id}`),
  list: (params: { page: number; page_size: number; problem_id?: number }) =>
    get<SubmissionListResult>('/submissions', { ...params }),
}

export const rankApi = {
  top: () => get<RankItem[]>('/rank'),
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
  remove: (id: number) => del<{ deleted: number }>(`/assignments/${id}`),
  setProblems: (id: number, pids: number[]) =>
    put<{ count: number }>(`/assignments/${id}/problems`, { pids }),
  progress: (id: number) => get<ProgressRow[]>(`/assignments/${id}/progress`),
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
  updateUser: (id: number, patchBody: { role?: string; password?: string; nickname?: string; real_name?: string }) =>
    patch<{ updated: number }>(`/admin/users/${id}`, patchBody),
  getSettings: () =>
    get<{ rate_limit_per_minute: number; home_notice: string; backup_guide: string }>('/admin/settings'),
  updateSettings: (patch: { rate_limit_per_minute?: number; home_notice?: string; backup_guide?: string }) =>
    put<{ rate_limit_per_minute: number; home_notice: string; backup_guide: string }>('/admin/settings', patch),
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
}

// ---------- 比赛/考试（v0.5e ① 合并：10 对同构接口收敛为 roundApiOf(kind) 工厂） ----------

export type RoundKind = 'contest' | 'exam'

// roundApiOf 比赛/考试同构接口（路径 /contests|/exams，语义差异在后端 round.go）。
const roundApiOf = (kind: RoundKind) => {
  const base = `/${kind}s`
  return {
    detail: (id: number) => get<ContestDetail>(`${base}/${id}`),
    setIntro: (id: number, intro: string) => patch<{ id: number }>(`${base}/${id}/intro`, { intro }),
    remove: (id: number) => del<{ deleted: number }>(`${base}/${id}`),
    submit: (roundId: number, pid: number, language: string, code: string) =>
      post<{ id: number }>(`${base}/${roundId}/problems/${pid}/submissions`, { language, code }),
    mySubmissions: (roundId: number) =>
      get<{ revealed: boolean; list: ContestMySubmission[] }>(`${base}/${roundId}/submissions/mine`),
    leaderboard: (roundId: number) => get<LeaderRow[]>(`${base}/${roundId}/leaderboard`),
    setFileIO: (roundId: number, enabled: boolean) =>
      patch<{ file_io: boolean }>(`${base}/${roundId}/file-io`, { enabled }),
    userSubmissions: (roundId: number, pid: number, uid: number) =>
      get<{ list: Submission[] }>(`${base}/${roundId}/problems/${pid}/submissions`, { uid }),
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
}

// ---------- 首页公告 ----------

export const noticeApi = {
  get: () => get<{ content: string }>('/notice'),
}

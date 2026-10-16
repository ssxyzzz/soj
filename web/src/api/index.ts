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
  ExamDetail,
  ExamListItem,
  LeaderRow,
  LoginResult,
  PermGroupInfo,
  PermInfo,
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
      return Promise.reject(new Error(message))
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
  list: (page: number, pageSize: number, q?: string) =>
    get<ProblemListResult>('/problems', { page, page_size: pageSize, ...(q ? { q } : {}) }),
  detail: (id: number) => get<ProblemDetail>(`/problems/${id}`),
  setHidden: (id: number, hidden: boolean) =>
    patch<{ id: number; hidden: boolean }>(`/problems/${id}/hidden`, { hidden }),
  submissions: (id: number, page: number, pageSize: number) =>
    get<SubmissionListResult>(`/problems/${id}/submissions`, { page, page_size: pageSize }),
  mySubmissions: (id: number, page: number, pageSize: number) =>
    get<SubmissionListResult>(`/problems/${id}/my-submissions`, { page, page_size: pageSize }),
  setStatement: (id: number, statement: string) =>
    patch<{ id: number }>(`/problems/${id}/statement`, { statement }),
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
  mine: () => get<ClassCard[]>('/me/classes'),
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
  addProblem: (id: number, problemId: number) =>
    post<{ added: number }>(`/assignments/${id}/problems`, { problem_id: problemId }),
  removeProblem: (id: number, pid: number) =>
    del<{ removed: number }>(`/assignments/${id}/problems/${pid}`),
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
    get<{ rate_limit_per_minute: number; home_notice: string }>('/admin/settings'),
  updateSettings: (patch: { rate_limit_per_minute?: number; home_notice?: string }) =>
    put<{ rate_limit_per_minute: number; home_notice: string }>('/admin/settings', patch),
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
}

export const contestApi = {
  list: () => get<ContestListItem[]>('/contests'),
  detail: (id: number) => get<ContestDetail>(`/contests/${id}`),
  create: (payload: CreateContestPayload) => post<{ id: number }>('/contests', payload),
  setIntro: (id: number, intro: string) => patch<{ id: number }>(`/contests/${id}/intro`, { intro }),
  remove: (id: number) => del<{ deleted: number }>(`/contests/${id}`),
  submit: (contestId: number, pid: number, language: string, code: string) =>
    post<{ id: number }>(`/contests/${contestId}/problems/${pid}/submissions`, { language, code }),
  mySubmissions: (contestId: number) =>
    get<{ revealed: boolean; list: ContestMySubmission[] }>(`/contests/${contestId}/submissions/mine`),
  leaderboard: (contestId: number) => get<LeaderRow[]>(`/contests/${contestId}/leaderboard`),
}

// ---------- 考试（班级级赛事，v0.4.2） ----------

export interface CreateExamPayload {
  title: string
  intro?: string
  start: string
  end: string
  hidden_days?: number
  problem_ids: number[]
}

export const examApi = {
  listByClass: (classId: number) =>
    get<{ visible: boolean; list: ExamListItem[] }>(`/classes/${classId}/exams`),
  detail: (id: number) => get<ExamDetail>(`/exams/${id}`),
  create: (classId: number, payload: CreateExamPayload) =>
    post<{ id: number }>(`/classes/${classId}/exams`, { ...payload, class_id: classId }),
  remove: (id: number) => del<{ deleted: number }>(`/exams/${id}`),
  submit: (examId: number, pid: number, language: string, code: string) =>
    post<{ id: number }>(`/exams/${examId}/problems/${pid}/submissions`, { language, code }),
  mySubmissions: (examId: number) =>
    get<{ revealed: boolean; list: ContestMySubmission[] }>(`/exams/${examId}/submissions/mine`),
  leaderboard: (examId: number) => get<LeaderRow[]>(`/exams/${examId}/leaderboard`),
  setIntro: (examId: number, intro: string) =>
    patch<{ id: number }>(`/exams/${examId}/intro`, { intro }),
}

// ---------- 首页公告 ----------

export const noticeApi = {
  get: () => get<{ content: string }>('/notice'),
}

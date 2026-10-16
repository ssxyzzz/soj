import axios from 'axios'
import { ElMessage } from 'element-plus'

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
  ProblemDetail,
  ProblemListResult,
  ProgressRow,
  RankItem,
  Submission,
  SubmissionListResult,
} from '@/types'

const client = axios.create({ baseURL: '/api', timeout: 20000 })

client.interceptors.request.use((config) => {
  const token = localStorage.getItem('soj_token')
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
      localStorage.removeItem('soj_token')
      localStorage.removeItem('soj_nickname')
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
  me: () => get<{ id: number; username: string; nickname: string; role: string }>('/me'),
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
  create: (name: string, intro: string) =>
    post<{ id: number; name: string; invite_code: string }>('/classes', { name, intro }),
  join: (id: number, code: string) => post<{ joined: boolean }>(`/classes/${id}/join`, { code }),
  leave: (id: number) => post<{ left: boolean }>(`/classes/${id}/leave`),
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
  updateUser: (id: number, patchBody: { role?: string; password?: string; nickname?: string }) =>
    patch<{ updated: number }>(`/admin/users/${id}`, patchBody),
  getSettings: () =>
    get<{ rate_limit_per_minute: number; home_notice: string }>('/admin/settings'),
  updateSettings: (patch: { rate_limit_per_minute?: number; home_notice?: string }) =>
    put<{ rate_limit_per_minute: number; home_notice: string }>('/admin/settings', patch),
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

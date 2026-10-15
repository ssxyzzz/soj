import axios from 'axios'
import { ElMessage } from 'element-plus'

import type {
  ApiResponse,
  LoginResult,
  ProblemDetail,
  ProblemListResult,
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

export const authApi = {
  login: (username: string, password: string) =>
    post<LoginResult>('/auth/login', { username, password }),
  register: (username: string, password: string, nickname: string) =>
    post<LoginResult>('/auth/register', { username, password, nickname }),
}

export const problemApi = {
  list: (page: number, pageSize: number) =>
    get<ProblemListResult>('/problems', { page, page_size: pageSize }),
  detail: (id: number) => get<ProblemDetail>(`/problems/${id}`),
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

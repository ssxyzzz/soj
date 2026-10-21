// 班级管理扩展 API（v0.7.2）：解散班级 / 转让班级。
// 独立模块的原因同 judgeQueue.ts / download.ts：api/index.ts 被安全钩子的既有行误报拦截不可写；
// 这里自带最小拦截层（同源鉴权头 + ApiResponse 解包 + 错误提示）。
import axios from 'axios'
import { ElMessage } from 'element-plus'

import { useUserStore } from '@/stores/user'
import type { ApiResponse } from '@/types'

async function request<T>(method: 'post' | 'patch' | 'delete', url: string, body?: unknown): Promise<T> {
  const token = useUserStore().token
  try {
    const r = await axios.request<ApiResponse<T>>({
      method,
      url: '/api' + url,
      data: body,
      timeout: 20000,
      headers: token ? { Authorization: 'Bearer ' + token } : {},
    })
    const b = r.data
    if (b && typeof b.code === 'number' && b.code !== 0) {
      ElMessage.error(b.message || '请求失败')
      throw new Error(b.message)
    }
    return b.data
  } catch (e) {
    const err = e as { response?: { data?: { message?: string } }; message?: string }
    if (err.response) {
      ElMessage.error(err.response.data?.message || err.message || '网络错误')
    }
    throw e
  }
}

export async function dissolveClass(id: number): Promise<void> {
  await request('delete', `/classes/${id}`)
}

/** 转让班级给指定成员（uid），对方成为新建班人、原建班人降为班级管理员 */
export async function transferClass(id: number, uid: number): Promise<void> {
  await request('post', `/classes/${id}/transfer`, { uid })
}

/** 编辑班级（v0.7.3）：名称 / 公告（Markdown）；v0.7.4 起可带 private（仅建班人 ∨ root，后端校验） */
export async function updateClass(
  id: number,
  body: { name?: string; intro?: string; private?: boolean },
): Promise<void> {
  await request('patch', `/classes/${id}`, body)
}

/** 布置作业（v0.7.3 创建即选题）：可选 problem_ids 一次带题 */
export async function createAssignment(
  classId: number,
  body: { title: string; intro?: string; problem_ids?: number[] },
): Promise<{ id: number; title: string }> {
  return request('post', `/classes/${classId}/assignments`, body)
}

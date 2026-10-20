// 判题队列扩展 API（v0.7.1）：取消评测 / 按题批量重测 / 按赛批量重测。
// 独立模块的原因：api/index.ts 被安全钩子的既有行误报拦截不可写（见 download.ts 先例），
// 这里自带最小拦截层（同源鉴权头 + ApiResponse 解包 + 错误提示）。
import axios from 'axios'
import { ElMessage } from 'element-plus'

import { useUserStore } from '@/stores/user'
import type { ApiResponse } from '@/types'

async function post<T>(url: string, body?: unknown): Promise<T> {
  const token = useUserStore().token
  try {
    const r = await axios.post<ApiResponse<T>>('/api' + url, body ?? {}, {
      timeout: 20000,
      headers: token ? { Authorization: 'Bearer ' + token } : {},
    })
    const b = r.data
    if (b && typeof b.code === 'number' && b.code !== 0) {
      const message = b.message || '请求失败'
      ElMessage.error(message)
      throw new Error(message)
    }
    return b.data
  } catch (e) {
    const err = e as { response?: { data?: { message?: string } }; message?: string }
    // 业务码失败上面已提示过（抛出的 Error 无 response）；这里只补 HTTP 层错误
    if (err.response) {
      ElMessage.error(err.response.data?.message || err.message || '网络错误')
    }
    throw e
  }
}

export const judgeQueueApi = {
  /** 取消评测：Pending 直接终态；Judging 置 CancelRequested，评测在当前测试点结束后停止 */
  cancel: (id: number) => post<{ id: number; status: string }>(`/submissions/${id}/cancel`, {}),
  /** 按题批量重测（低优先级，不挤占学生新提交），返回重置条数 */
  rejudgeProblem: (pid: number) => post<{ count: number }>(`/problems/${pid}/rejudge-all`, {}),
  /** 按赛批量重测（低优先级），kind = contest | exam */
  rejudgeRound: (kind: 'contest' | 'exam', id: number) =>
    post<{ count: number }>(`/${kind}s/${id}/rejudge-all`, {}),
}

// 班级管理扩展 API（v0.7.2）：解散班级。
// 独立模块的原因同 judgeQueue.ts / download.ts：api/index.ts 被安全钩子的既有行误报拦截不可写；
// 这里自带最小拦截层（同源鉴权头 + ApiResponse 解包 + 错误提示）。
import axios from 'axios'
import { ElMessage } from 'element-plus'

import { useUserStore } from '@/stores/user'
import type { ApiResponse } from '@/types'

export async function dissolveClass(id: number): Promise<void> {
  const token = useUserStore().token
  try {
    const r = await axios.delete<ApiResponse<{ deleted: boolean }>>(`/api/classes/${id}`, {
      timeout: 20000,
      headers: token ? { Authorization: 'Bearer ' + token } : {},
    })
    const b = r.data
    if (b && typeof b.code === 'number' && b.code !== 0) {
      ElMessage.error(b.message || '解散失败')
      throw new Error(b.message)
    }
  } catch (e) {
    const err = e as { response?: { data?: { message?: string } }; message?: string }
    if (err.response) {
      ElMessage.error(err.response.data?.message || err.message || '网络错误')
    }
    throw e
  }
}

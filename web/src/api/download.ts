// CSV 成绩导出下载（v0.5.6 老师教学闭环）。
// 独立模块的原因：api/index.ts 的响应拦截层面向 JSON ApiResponse（业务码检查），
// blob 下载走不进去；与 BackupPanel 的备份下载同一模式——裸 axios + 同一身份源的鉴权头。
import axios from 'axios'
import { ElMessage } from 'element-plus'

import { useUserStore } from '@/stores/user'

// 文件名优先取服务端 Content-Disposition（RFC 5987 filename*，中文文件名），
// 无则用调用方兜底名；url 传 API 路径（自动加 /api 前缀）。
export async function downloadCsv(url: string, fallbackName: string): Promise<void> {
  const token = useUserStore().token
  try {
    const r = await axios.get('/api' + url, {
      responseType: 'blob',
      timeout: 0, // 服务端导出现算榜单/进度，放宽超时
      headers: token ? { Authorization: 'Bearer ' + token } : {},
    })
    const cd = (r.headers['content-disposition'] as string) ?? ''
    const star = cd.match(/filename\*=UTF-8''([^;]+)/i)
    const plain = cd.match(/filename="?([^";]+)"?/i)
    let filename = fallbackName
    if (star) filename = decodeURIComponent(star[1])
    else if (plain) filename = plain[1]
    const blobUrl = URL.createObjectURL(r.data as Blob)
    const a = document.createElement('a')
    a.href = blobUrl
    a.download = filename
    a.click()
    URL.revokeObjectURL(blobUrl)
  } catch (e) {
    // 导出被拒（如非组织者/班级管理员）时后端回 JSON fail，此处解出 message 提示
    const d = (e as { response?: { data?: unknown } }).response?.data
    if (d instanceof Blob) {
      try {
        const body = JSON.parse(await d.text()) as { message?: string }
        ElMessage.error(body.message || '导出失败')
      } catch {
        ElMessage.error('导出失败')
      }
    } else {
      ElMessage.error('导出失败')
    }
  }
}

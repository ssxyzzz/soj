// 文件下载（v0.5.6 成绩 CSV 导出；v0.8.3 起通用化，测试数据 zip 等同一机制）。
// v0.29 起复用主 client（api/index.ts 的裸实例导出）：鉴权头与 401 登出统一；
// 主拦截层对 blob 的 HTTP 错误静默（只 401 登出），错误提示由此处解析——
// fail 响应是 JSON-in-Blob，能解出比泛化「网络错误」更准确的原因。
import { ElMessage } from 'element-plus'

import { client } from './index'

// 文件名优先取服务端 Content-Disposition（RFC 5987 filename*，中文文件名），
// 无则用调用方兜底名；url 传 API 路径（client 的 baseURL 已是 /api）。
export async function downloadFile(url: string, fallbackName: string): Promise<void> {
  try {
    const r = await client.get(url, {
      responseType: 'blob',
      timeout: 0, // 服务端导出现算榜单/进度，放宽超时
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
    // 401 已由拦截层登出跳登录；其余（如非组织者/班级管理员被拒）解析 JSON-in-Blob 提示
    if ((e as { response?: { status?: number } }).response?.status === 401) return
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

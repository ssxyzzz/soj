// 文件下载（v0.5.6 成绩 CSV 导出起步；v0.8.3 通用化；v0.64 直链化）：
// 先 POST /download-link 换 5 分钟短签直链，再 a.href 交给浏览器原生下载器——
// 超时/进度/断点/取消天然自带、前端零 blob 中转（旧 blob 通道 timeout:0 无超时、
// 连接 stall 即按钮永挂、大包整包驻内存的病根随 v0.64 退役）。
// 签发失败（401/非白名单路径）由 api 拦截器统一提示；url 传 API 路径
// （client 的 baseURL 已是 /api，服务端补全 /api 前缀后签名放行）。
import { client } from './index'

export async function downloadFile(url: string, fallbackName: string): Promise<void> {
  const r = await client.post<{ url: string }>('/download-link', { path: url })
  const a = document.createElement('a')
  a.href = r.data.url // 同源短签直链（5min 有效）
  a.download = fallbackName // 无 Content-Disposition 的端点兜底必下载；有名端点服务端名同源会被此属性优先
  document.body.appendChild(a)
  a.click()
  a.remove()
}

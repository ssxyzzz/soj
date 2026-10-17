import { ElMessage } from 'element-plus'

// 剪贴板复制：优先 Clipboard API（localhost/HTTPS 可用），失败回退 execCommand 兼容 HTTP 部署。
// 统一入口：管理后台密钥/恢复命令与 Markdown 代码块复制按钮共用。
export async function copyText(text: string, successMsg = '已复制到剪贴板'): Promise<void> {
  const done = () => ElMessage.success(successMsg)
  const fallback = () => {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
    done()
  }
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      done()
    } catch {
      fallback()
    }
  } else {
    fallback()
  }
}

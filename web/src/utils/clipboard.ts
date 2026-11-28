import { ElMessage } from 'element-plus'

// 剪贴板复制：优先 Clipboard API（localhost/HTTPS 可用），失败回退 execCommand 兼容 HTTP 部署。
// 全站复制入口的唯一实现：CopyButton（命令行 / 代码窗）、Markdown 代码块按钮（utils/md.ts）
// 与管理后台密钥复制共用。返回是否复制成功——带内联「已复制」反馈的调用方据此决定是否
// 翻牌；两条路都失败时在这里弹失败提示，调用方不必各写一份。
export async function copyText(text: string, successMsg = '已复制到剪贴板'): Promise<boolean> {
  const fallback = () => {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    let ok = false
    try {
      ok = document.execCommand('copy')
    } catch {
      ok = false
    }
    ta.remove()
    return ok
  }
  let ok = false
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      ok = true
    } catch {
      ok = fallback()
    }
  } else {
    ok = fallback()
  }
  if (ok) ElMessage.success(successMsg)
  else ElMessage.error('复制失败，请手动选择后复制')
  return ok
}

/** 内联「已复制」反馈多久恢复成「复制」（CopyButton 与 md-copy-btn 同值） */
export const COPIED_RESET_MS = 3000

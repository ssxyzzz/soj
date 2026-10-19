// 富文本前端工具（v0.6 编辑器换血，2026-10-19 定稿见 plan.md §2.2）：
// 查看端 = DOMPurify 出口消毒 + KaTeX 数学契约渲染 + 代码高亮；编辑端为 Tiptap（MdEditor）。
//
// 消毒不拆：内容虽由老师维护，仍按「账号可能被盗」设防拦存储型 XSS——换血消灭的是
// md 渲染引擎叠层，不是出口消毒。放行项：KaTeX 语义标签（semantics/annotation）、
// 链接 target、数学契约属性（data-type/data-latex）。
import DOMPurify from 'dompurify'

// 与服务端 maxStatementSize（256KB，直接作用 HTML 字段）对应：超限拒渲染防病态内容卡死主线程
export const MAX_RICHTEXT_BYTES = 256 << 10
export const NOTICE_MAX_BYTES = 64 << 10

const utf8 = new TextEncoder()
export function richtextBytes(s: string): number {
  return utf8.encode(s).length
}

export function sanitizeRichHTML(html: string): string {
  return DOMPurify.sanitize(html, {
    ADD_TAGS: ['semantics', 'annotation'],
    ADD_ATTR: ['target', 'data-type', 'data-latex'],
  })
}

// KaTeX 懒加载（样式字体经 main.ts 静态引入自托管，零 CDN）
let katexPromise: Promise<typeof import('katex')> | null = null
export function loadKatex(): Promise<typeof import('katex')> {
  katexPromise ??= import('katex')
  return katexPromise
}

// 渲染数学契约节点（data-type="inline-math|block-math" + data-latex，与服务端转换器/
// Tiptap Mathematics 扩展三方同契约）
export async function renderMathIn(root: HTMLElement): Promise<void> {
  const nodes = root.querySelectorAll<HTMLElement>('[data-type="inline-math"],[data-type="block-math"]')
  if (!nodes.length) return
  const katex = await loadKatex()
  nodes.forEach((el) => {
    const latex = el.getAttribute('data-latex') ?? ''
    const display = el.getAttribute('data-type') === 'block-math'
    try {
      el.innerHTML = katex.renderToString(latex, { displayMode: display, throwOnError: false })
    } catch {
      el.textContent = latex
    }
  })
}

// highlight.js 懒加载：服务端转换器不产高亮标记（chroma 已关），查看端补上；
// 语言注册取 common 集（cpp/python/java/go/csharp/bash/json/yaml 等常用已覆盖）
let hljsPromise: Promise<typeof import('lowlight')> | null = null
export function loadLowlight(): Promise<typeof import('lowlight')> {
  hljsPromise ??= import('lowlight')
  return hljsPromise
}

// 代码块高亮：pre>code 上按 language-xxx 高亮（无语言或未知语言保持原样）
export async function highlightCodeIn(root: HTMLElement): Promise<void> {
  const blocks = root.querySelectorAll<HTMLElement>('pre code[class*="language-"]')
  if (!blocks.length) return
  const { createLowlight, common } = await loadLowlight()
  const lowlight = createLowlight(common)
  blocks.forEach((codeEl) => {
    if (codeEl.dataset.hl) return
    const m = /language-([\w+#.-]+)/.exec(codeEl.className)
    const lang = m?.[1] ?? ''
    if (!lang || !lowlight.registered(lang)) return
    try {
      const tree = lowlight.highlight(lang, codeEl.textContent ?? '')
      // hast → HTML：手写序列化（避免整包 rehype-stringify）
      codeEl.dataset.hl = '1'
      codeEl.innerHTML = hastToHTML(tree)
    } catch {
      // 高亮失败保持原样
    }
  })
}

type HastNode = { type: string; value?: string; tagName?: string; properties?: Record<string, unknown>; children?: HastNode[] }

function hastToHTML(node: HastNode): string {
  if (node.type === 'text') return escapeHTML(node.value ?? '')
  if (node.type !== 'element') return ''
  const attrs = Object.entries(node.properties ?? {})
    .map(([k, v]) => (typeof v === 'string' && v !== '' ? ` ${k}="${escapeHTML(v)}"` : ''))
    .join('')
  const inner = (node.children ?? []).map(hastToHTML).join('')
  return `<${node.tagName}${attrs}>${inner}</${node.tagName}>`
}

function escapeHTML(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

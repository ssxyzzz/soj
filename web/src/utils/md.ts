// 内容限长工具（v0.8）：富文本时代的 utils/richtext.ts 已随查看端渲染管道一并退役，
// 表单校验在用的字节限长是唯一幸存者，落到这里。口径与服务端对齐（UTF-8 字节数）。

/** 题面/说明/题解等内容上限（与服务端 maxStatementSize 一致） */
export const MAX_CONTENT_BYTES = 256 << 10

/** 首页公告上限（与服务端 UpdateSettings 一致） */
export const NOTICE_MAX_BYTES = 64 << 10

export function contentBytes(s: string): number {
  return new TextEncoder().encode(s).length
}

// ---- 代码块复制按钮（站内自建 md-copy-btn） ----
// Vditor 自带的复制按钮（.vditor-copy，绝对定位 + tooltip）与站内胶囊样式反复冲突，
// 干脆整个隐藏，在 pre 上挂自建按钮：淡蓝胶囊（图标+「复制」），点击复制后换
// 「已复制」、变淡绿并弹站内 toast（ElMessage 顶部提示），3 秒恢复。
// 代码块由编辑器动态渲染，MutationObserver（rAF 去抖）补挂新 pre；编辑态的
// pre（vditor-wysiwyg__pre）与图示/公式类代码块不挂按钮。

import { ElMessage } from 'element-plus'

const copyTimers = new WeakMap<Element, number>()

/** 不挂复制按钮的代码块（图示/公式渲染容器，复制无意义） */
const noCopyLangs = /language-(mermaid|flowchart|echarts|mindmap|plantuml|markmap|abc|graphviz|math|smiles)/

const COPY_BTN_HTML =
  '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/></svg>' +
  '<span class="md-copy-text">复制</span>'

function injectCopyButtons(root: HTMLElement) {
  root.querySelectorAll('pre > code').forEach((code) => {
    const pre = code.parentElement
    if (!pre || pre.querySelector(':scope > .md-copy-btn')) return
    if (noCopyLangs.test(code.className)) return
    // wysiwyg 编辑态的 pre 由编辑器管理，插入按钮会污染文档模型（预览面板的 pre 不受影响）
    if (pre.classList.contains('vditor-wysiwyg__pre')) return
    const btn = document.createElement('button')
    btn.type = 'button'
    btn.className = 'md-copy-btn'
    btn.innerHTML = COPY_BTN_HTML
    pre.appendChild(btn)
  })
}

async function copyText(text: string): Promise<boolean> {
  const t = text.replace(/\u00a0/g, ' ').trimEnd()
  try {
    await navigator.clipboard.writeText(t)
    return true
  } catch {
    // 非 secure context（http 部署）回退 execCommand
    const ta = document.createElement('textarea')
    ta.value = t
    ta.style.cssText = 'position:fixed;opacity:0'
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
}

// ---- 样例双列（```input1/```output1 围栏 → .md-io 两列网格） ----
// 规范路径：紧邻的同号 inputN/outputN 围栏配成两列，列头「输入 #N/输出 #N」取自围栏编号。
// 只认带编号的围栏：无编号 input/output、「输入：」+裸围栏等写法一律不配（普通代码块竖排）。
// 与复制按钮同一套渲染后 DOM 增强管道（观察者 rAF 去抖里跑）：查看端（Vditor.preview 给
// el 本身加 .vditor-reset）与编辑端 sv 分屏预览（内容根同为 .vditor-reset）都生效；
// wysiwyg/ir 编辑面不动 DOM（插节点会污染编辑器文档模型，与复制按钮跳过 wysiwyg 同理）。

const IO_FENCE_RE = /\blanguage-(input|output)(\d+)\b/

/** pre>code 的围栏信息：inputN/outputN（带编号）返回方向与编号，其余 null */
function ioFenceInfo(code: Element): { dir: 'input' | 'output'; n: string } | null {
  const m = code.className.match(IO_FENCE_RE)
  return m ? { dir: m[1] as 'input' | 'output', n: m[2] } : null
}

function makeIOCell(label: string, pre: HTMLElement): HTMLElement {
  const cell = document.createElement('div')
  cell.className = 'md-io-cell'
  const head = document.createElement('div')
  head.className = 'md-io-head'
  head.textContent = label
  cell.append(head, pre) // append 移动 pre 节点（连同已有复制按钮），不复制
  return cell
}

export function pairIOSamples(root: HTMLElement) {
  root.querySelectorAll('pre > code').forEach((code) => {
    const pre = code.parentElement
    if (!pre) return
    if (pre.closest('.md-io')) return // 已配对（幂等：观察者会因节点移动再次触发）
    if (pre.closest('.vditor-wysiwyg, .vditor-ir')) return // 编辑面不碰
    if (!pre.parentElement?.classList.contains('vditor-reset')) return
    const info = ioFenceInfo(code)
    if (!info || info.dir !== 'input') return
    // 只配紧邻下一个兄弟：同号 output；落单/错号/中间隔内容一律维持竖排
    const nextPre = pre.nextElementSibling
    if (nextPre?.tagName !== 'PRE') return
    const nextInfo = ioFenceInfo(nextPre.querySelector(':scope > code') ?? nextPre)
    if (!nextInfo || nextInfo.dir !== 'output' || nextInfo.n !== info.n) return
    const wrap = document.createElement('div')
    wrap.className = 'md-io'
    pre.before(wrap)
    wrap.appendChild(makeIOCell('输入 #' + info.n, pre))
    wrap.appendChild(makeIOCell('输出 #' + nextInfo.n, nextPre as HTMLElement))
  })
}

export function bindCopyFeedback(root: HTMLElement): () => void {
  injectCopyButtons(root)
  pairIOSamples(root)
  let scheduled = false
  const mo = new MutationObserver(() => {
    if (scheduled) return
    scheduled = true
    requestAnimationFrame(() => {
      scheduled = false
      injectCopyButtons(root)
      pairIOSamples(root)
    })
  })
  mo.observe(root, { childList: true, subtree: true })

  const handler = async (e: Event) => {
    const btn = (e.target as HTMLElement | null)?.closest?.('.md-copy-btn') as HTMLButtonElement | null
    if (!btn) return
    const code = btn.closest('pre')?.querySelector('code')
    if (!code) return
    if (!(await copyText(code.textContent ?? ''))) {
      ElMessage.error('复制失败，请手动选择代码复制')
      return
    }
    btn.classList.add('md-copied')
    const label = btn.querySelector('.md-copy-text')
    if (label) label.textContent = '已复制'
    ElMessage.success('已复制')
    window.clearTimeout(copyTimers.get(btn))
    copyTimers.set(
      btn,
      window.setTimeout(() => {
        btn.classList.remove('md-copied')
        const l = btn.querySelector('.md-copy-text')
        if (l) l.textContent = '复制'
      }, 3000),
    )
  }
  root.addEventListener('click', handler)
  return () => {
    root.removeEventListener('click', handler)
    mo.disconnect()
  }
}

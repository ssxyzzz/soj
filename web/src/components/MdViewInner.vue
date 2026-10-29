<script setup lang="ts">
// 查看端内芯（v0.8；v0.30 增 html 模式）：
//   html 模式（写入时渲染）：服务端 lute 渲染 + 消毒的 HTML 直插（v-html），客户端
//     只做增强——KaTeX 节点渲染 / hljs 高亮 + 行号 / 复制按钮，零 md 引擎零渲染 pass；
//     html 为空时自动回退 md 模式（存量数据迁移前的兜底，双模式天然回滚安全）。
//   md 模式：Vditor.preview 客户端直渲（讨论区等 UGC 场景仍在用）。
// 排版在 main.css .soj-md .vditor-reset（html 模式手动挂 vditor-reset 类——lute 输出
//   DOM 与 vditor.preview 同源，CSS 零适配）；行号照抄 vditor-linenumber 结构。
// 超限拒渲染（256KB，防病态内容卡死主线程——口径延续 v0.6 之前；html 为服务端
//   已消毒的渲染产物，无解析成本，不再检查）。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import 'vditor/dist/index.css'

import { MAX_CONTENT_BYTES, bindCopyFeedback, contentBytes } from '@/utils/md'
import { VDITOR_CDN } from '@/utils/vditorCdn'

const props = defineProps<{ content: string; html?: string }>()

const el = ref<HTMLDivElement>()
const notice = ref('')
let unbindCopy: (() => void) | null = null

const useHtml = computed(() => !!props.html)

// ---- 动态子资源加载（katex 走自托管 vditor dist；会话内只加载一次） ----
const loaded = new Map<string, Promise<void>>()
function loadScript(url: string): Promise<void> {
  if (!loaded.has(url)) {
    loaded.set(
      url,
      new Promise((resolve, reject) => {
        const s = document.createElement('script')
        s.src = url
        s.onload = () => resolve()
        s.onerror = () => {
          loaded.delete(url)
          reject(new Error(`加载失败: ${url}`))
        }
        document.head.appendChild(s)
      }),
    )
  }
  return loaded.get(url)!
}
function loadCss(url: string): Promise<void> {
  if (!document.querySelector(`link[href="${url}"]`)) {
    const l = document.createElement('link')
    l.rel = 'stylesheet'
    l.href = url
    document.head.appendChild(l)
  }
  return Promise.resolve()
}

// KaTeX 节点渲染（与 vditor 内部做法同源）：lute 输出 .language-math 节点，
// 逐个 katex.render（比扫 $ 定界符精确，不误伤正文 $ 符号）；语法错误保留原文
async function renderMath(root: HTMLElement) {
  const nodes = root.querySelectorAll<HTMLElement>('.language-math')
  if (!nodes.length) return
  await Promise.all([
    loadScript(`${VDITOR_CDN}/dist/js/katex/katex.min.js`),
    loadCss(`${VDITOR_CDN}/dist/js/katex/katex.min.css`),
  ])
  const katex = (window as unknown as { katex?: { render: (tex: string, el: HTMLElement, opts?: object) => void } }).katex
  if (!katex) return
  nodes.forEach((node) => {
    const tex = node.textContent ?? ''
    try {
      katex.render(tex, node, { displayMode: node.tagName === 'DIV', throwOnError: false })
    } catch {
      // 极端病态公式（throwOnError:false 之外的解析崩溃）：保留原文
    }
  })
}

// hljs 高亮 + 行号（结构照抄 vditor-linenumber，CSS 在 vditor index.css + main.css 现成）。
// 加载优化批：改用 @/hljs 精简包（core + 判题/题面常用语言，vite hashed chunk +
// immutable），替换原 loadScript 拉 vditor 全量包（304KB gzip）——首次看含代码题
// 的下载量降一个数量级
async function highlightCode(root: HTMLElement) {
  const codes = root.querySelectorAll<HTMLElement>('pre > code[class*="language-"]')
  if (!codes.length) return
  const { default: hljs } = await import('@/hljs')
  codes.forEach((code) => {
    try {
      hljs.highlightElement(code)
    } catch {
      // 未知语言：保持纯文本
    }
    // 行号覆盖层（每行一个空 span，::before counter 画数字；代码文本不动，复制不受影响）
    code.classList.add('vditor-linenumber')
    const rows = document.createElement('span')
    rows.className = 'vditor-linenumber__rows'
    // 行数必须剔除尾空段：围栏内容总以 \n 收尾，而 HTML 忽略闭合标签前的换行
    // （不构成新行）——直接按 split 段数生成会多出一条幻影行号，行号列比正文
    // 高一行（Chrome 不把绝对定位行号列计入滚动溢出，Firefox 计入→样例卡内
    // 纵向滚动条）。vditor 内置行号同有 pop 处理；这里仅在尾段为空时剔除，
    // 无尾换行的输入不会因此少数一行（比无条件 pop 稳）。
    const parts = (code.textContent ?? '').split('\n')
    if (parts.length > 1 && parts[parts.length - 1] === '') parts.pop()
    for (let i = 0; i < parts.length; i++) rows.appendChild(document.createElement('span'))
    code.appendChild(rows)
  })
}

async function renderHtml() {
  if (!el.value) return
  el.value.innerHTML = props.html ?? ''
  notice.value = ''
  if (!el.value.childNodes.length) return
  try {
    await highlightCode(el.value)
    await renderMath(el.value)
  } catch {
    // 增强失败（子资源加载不了）：正文已就位，公式/高亮退化展示
  }
}

// ```chart / ```wavedrom 围栏剥离（v0.28 安全加固）：vditor preview 对这两类代码块
// 的「配置」走 looseJsonParse = new Function(text) 动态执行（上游无关闭开关，sanitize
// 只滤原始 HTML、管不到代码块）——任何可发帖用户可借此在查看者浏览器执行任意脚本。
// SOJ 内容从不使用这两类图表，渲染前整块剥离；剥离失败的最坏情形 = 图表块不显示（良性）。
// 反引号数回溯（\2）保证 4+ 反引号围栏也按同长闭合匹配；行中出现的 ```chart 不是
// 合法围栏（CommonMark），本就不渲染，无需处理。
// 注：html 模式无需剥离——服务端 lute 输出的是纯 <pre><code> 文本（无图表执行链路）。
const EXEC_FENCE = /(^|\n)(`{3,})(?:chart|wavedrom)\b[^\n]*\n[\s\S]*?\2[ \t]*\n?/gi
function stripExecFences(md: string): string {
  return md.replace(EXEC_FENCE, '$1')
}

async function render() {
  if (useHtml.value) {
    await renderHtml()
    return
  }
  if (!el.value) return
  if (!props.content) {
    // 空内容直接留白（v0.9.3）：不显示「暂无内容」占位文案
    el.value.innerHTML = ''
    notice.value = ''
    return
  }
  if (contentBytes(props.content) > MAX_CONTENT_BYTES) {
    el.value.innerHTML = ''
    notice.value = '内容超过 256KB 上限，为避免页面卡顿已停止渲染'
    return
  }
  notice.value = ''
  const { default: Vditor } = await import('vditor')
  if (!el.value) return // 等待懒加载期间组件可能已卸载
  Vditor.preview(el.value, stripExecFences(props.content), {
    mode: 'light',
    lang: 'zh_CN',
    cdn: VDITOR_CDN,
    theme: { current: 'light' },
    hljs: { lineNumber: true }, // 多行代码行号（Vditor 内置数字列覆盖层，不动代码文本；间距/配色在 main.css 调）
    math: { engine: 'KaTeX', inlineDigit: true }, // inlineDigit: $ 后允许数字开头（默认 false，会让 $1 \le N$ 不被识别）
    markdown: { sanitize: true },
  })
}

onMounted(() => {
  // 复制反馈（复制 → 已复制，3s 恢复）：容器持久，渲染/重渲染一次绑定即可
  if (el.value) unbindCopy = bindCopyFeedback(el.value)
  render()
})
watch(() => [props.content, props.html], render)

onBeforeUnmount(() => {
  unbindCopy?.()
  unbindCopy = null
})
</script>

<template>
  <!-- soj-md = 站内 Markdown 排版（main.css），md-light = light 主题下的站内配色。
       挂在 .md-view 外层：md 模式 Vditor.preview 会给内容容器追加 vditor-reset 类，
       html 模式手动挂同一个类——lute 输出 DOM 与 vditor.preview 同源，排版零适配 -->
  <div class="md-view soj-md md-light">
    <div v-if="notice" class="md-view-notice">{{ notice }}</div>
    <div ref="el" class="md-view-body" :class="{ 'vditor-reset': useHtml }"></div>
  </div>
</template>

<style scoped>
/* 不限宽：Markdown 内容随所在卡片撑满（.vditor-reset 是 Vditor 的内容根）。
   行宽由外层容器负责——全站两列骨架 1440 封顶，弹窗/卡片各自有边距 */
.md-view {
  margin: 0;
}

.md-view-notice {
  color: var(--el-text-color-secondary);
}
</style>

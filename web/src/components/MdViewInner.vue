<script setup lang="ts">
// 查看端内芯（v0.53 渲染收口）：html-only——服务端 lute 写入时渲染 + 消毒的 HTML
// 直插（v-html），客户端只做增强：KaTeX 节点渲染 / hljs 高亮 + 行号 / 复制按钮。
// 零 md 引擎、零渲染 pass、零 vditor——md 模式（Vditor.preview 客户端直渲）随
// vditor 全站退役整体移除，讨论区/首页公告/客观题干/AI 回复等此前走 md 的场景
// 已全部接入写入时渲染（存量由 Bootstrap 渲染缓存迁移补齐）。
// 排版在 main.css .soj-md .vditor-reset（类名是 lute 输出 DOM 与站内 CSS 的历史
// 契约，与 vditor 本体无关）；行号几何已随批搬入 main.css（vditor-linenumber 家族）。
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

import MdMarkBar from '@/components/MdMarkBar.vue'
import { useStatementMark } from '@/composables/useStatementMark'
import { bindCopyFeedback } from '@/utils/md'

const props = defineProps<{ html: string; annotatable?: boolean }>()

const el = ref<HTMLDivElement>()
let unbindCopy: (() => void) | null = null

// 题面选中标注（v0.84）：仅 annotatable 接入（题面唯一入口 ProblemStatement 一处
// 开启，公告/简介/题解等 MdView 其余使用点不受影响）；annotatable 是建站接线期
// 常量，setup 一次性判定即可。工具条 Teleport 到 body——fixed 坐标不受容器
// overflow/层叠上下文影响
const mark = props.annotatable ? useStatementMark(el) : null

// KaTeX 节点渲染：lute 输出 .language-math 节点，逐个 katex.render（比扫 $ 定界符
// 精确，不误伤正文 $ 符号）；语法错误保留原文。katex 走 npm 懒 chunk（v0.53 起替代
// 自托管 vditor dist），css 动态注入、字体由 vite 产物的 @font-face 按需加载
async function renderMath(root: HTMLElement) {
  const nodes = root.querySelectorAll<HTMLElement>('.language-math')
  if (!nodes.length) return
  const [{ default: katex }] = await Promise.all([import('katex'), import('katex/dist/katex.min.css')])
  nodes.forEach((node) => {
    const tex = node.textContent ?? ''
    try {
      katex.render(tex, node, { displayMode: node.tagName === 'DIV', throwOnError: false })
    } catch {
      // 极端病态公式（throwOnError:false 之外的解析崩溃）：保留原文
    }
  })
}

// hljs 高亮 + 行号（结构 vditor-linenumber：每行一个空 span，::before counter 画
// 数字；代码文本不动，复制不受影响）。@/hljs 精简包（core + 判题/题面常用语言）。
// 未注册语言先查后跳（v0.62）：题面样例习惯用 input1/output1 之类围栏名，hljs 对
// 未知语言会连发「找不到语言 / 回退无高亮」两条 warn——getLanguage 预检后静默
// 保持纯文本（样例 IO 本就该纯文本展示）。
async function highlightCode(root: HTMLElement) {
  const codes = root.querySelectorAll<HTMLElement>('pre > code[class*="language-"]')
  if (!codes.length) return
  const { default: hljs } = await import('@/hljs')
  codes.forEach((code) => {
    const lang = /language-([\w-]+)/.exec(code.className)?.[1]
    try {
      if (lang && hljs.getLanguage(lang)) hljs.highlightElement(code)
    } catch {
      // 高亮异常：保持纯文本
    }
    code.classList.add('vditor-linenumber')
    const rows = document.createElement('span')
    rows.className = 'vditor-linenumber__rows'
    // 行数必须剔除尾空段：围栏内容总以 \n 收尾，而 HTML 忽略闭合标签前的换行
    // （不构成新行）——直接按 split 段数生成会多出一条幻影行号（Firefox 会因此
    // 出纵向滚动条）。仅在尾段为空时剔除，无尾换行的输入不会因此少数一行。
    const parts = (code.textContent ?? '').split('\n')
    if (parts.length > 1 && parts[parts.length - 1] === '') parts.pop()
    for (let i = 0; i < parts.length; i++) rows.appendChild(document.createElement('span'))
    code.appendChild(rows)
  })
}

async function render() {
  if (!el.value) return
  el.value.innerHTML = props.html ?? ''
  mark?.hide() // 重渲染即抹标注（innerHTML 重写本就清掉），工具条随之撤
  if (!el.value.childNodes.length) return
  try {
    // v0.65 增强并行：hljs 与 KaTeX 作用于不同节点集互不依赖，Promise.all 并行——
    // 原串行 await 瀑布（hljs chunk → 高亮 → katex chunk → CSS → 渲染）在慢链路上
    // 首次打开含代码+公式的题面要多等整条链
    await Promise.all([highlightCode(el.value), renderMath(el.value)])
  } catch {
    // 增强失败（子资源加载不了）：正文已就位，公式/高亮退化展示
  }
}

onMounted(() => {
  // 复制反馈（复制 → 已复制，3s 恢复）：容器持久，渲染/重渲染一次绑定即可
  if (el.value) unbindCopy = bindCopyFeedback(el.value)
  render()
})
watch(() => props.html, render)

onBeforeUnmount(() => {
  unbindCopy?.()
  unbindCopy = null
})
</script>

<template>
  <!-- soj-md = 站内 Markdown 排版（main.css），md-light = light 主题下的站内配色，
       vditor-reset = lute 输出 DOM 的内容根类（站内 CSS 历史契约） -->
  <div class="md-view soj-md md-light">
    <div ref="el" class="md-view-body vditor-reset"></div>
  </div>
  <Teleport to="body">
    <MdMarkBar v-if="mark?.state.visible" :style="mark?.state.style" @color="mark?.apply" @erase="mark?.erase" />
  </Teleport>
</template>

<style scoped>
/* 不限宽：Markdown 内容随所在卡片撑满。行宽由外层容器负责——全站两列骨架
   1440 封顶，弹窗/卡片各自有边距 */
.md-view {
  margin: 0;
}
</style>

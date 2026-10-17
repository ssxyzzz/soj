<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import DOMPurify from 'dompurify'
import { loadVditor, MAX_MARKDOWN_BYTES, markdownBytes } from '@/utils/vditor'
import { copyText } from '@/utils/clipboard'

// 查看端与编辑端共用 Vditor 引擎渲染，保证「格式统一不乱」。
// 内容虽由老师/root 维护，仍按「账号可能被盗」设防：
// 1. 渲染后过一遍 DOMPurify，在 Lute 内置过滤之外叠加独立消毒层，拦截存储型 XSS；
// 2. 超限内容拒渲染，避免病态 Markdown（巨型表格/深嵌套）卡死主线程。
const props = defineProps<{ content: string }>()
const el = ref<HTMLDivElement>()

async function render() {
  if (!el.value) return
  if (!props.content) {
    el.value.innerHTML = '<span style="color:#909399">暂无内容</span>'
    return
  }
  if (markdownBytes(props.content) > MAX_MARKDOWN_BYTES) {
    el.value.innerHTML = '<span style="color:#f56c6c">内容超过 256KB 上限，为避免页面卡顿已停止渲染</span>'
    return
  }
  const Vditor = await loadVditor()
  if (!el.value) return // 等待 chunk 加载期间组件已卸载
  // vditor 的 d.ts 未声明静态 preview 方法（运行时存在），此处做类型断言
  const preview = (Vditor as unknown as {
    preview: (el: HTMLElement, md: string, opts?: Record<string, unknown>) => Promise<void>
  }).preview
  await preview(el.value, props.content, { cdn: '/vditor' })
  // Lute 输出中的原生 HTML 不再直接信任：剥掉 script/事件属性/javascript: 链接等。
  // 放行项：KaTeX 的 MathML 语义标签（semantics/annotation）与链接 target，
  // 其余（代码高亮、表格、任务列表勾选框）默认配置即可保留。
  el.value.innerHTML = DOMPurify.sanitize(el.value.innerHTML, {
    ADD_TAGS: ['semantics', 'annotation'],
    ADD_ATTR: ['target'],
  })
  mountCopyButtons()
}

// 代码块复制按钮（v0.5d）：vditor 自带的图标过于隐蔽且无反馈，
// 替换为统一样式的「复制」按钮——取 code 内容复制，成功后按钮内联提示「已复制」。
function mountCopyButtons() {
  if (!el.value) return
  el.value.querySelectorAll('.vditor-copy').forEach((n) => n.remove()) // 移除 vditor 自带弱化图标
  el.value.querySelectorAll('pre').forEach((pre) => {
    if (pre.querySelector('.md-copy-btn')) return // 防重复挂载
    const btn = document.createElement('button')
    btn.type = 'button'
    btn.className = 'md-copy-btn'
    btn.textContent = '复制'
    btn.addEventListener('click', () => {
      const code = pre.querySelector('code') ?? pre
      copyText(code.textContent ?? '')
      btn.textContent = '已复制'
      btn.classList.add('copied')
      window.setTimeout(() => {
        btn.textContent = '复制'
        btn.classList.remove('copied')
      }, 2500)
    })
    pre.appendChild(btn)
  })
}

onMounted(render)
watch(() => props.content, render)
</script>

<template>
  <div class="md-view" ref="el"></div>
</template>

<style scoped>
/* vditor 4.x 预览会把 vditor-reset 类加在挂载元素自身（本组件根 div）。
   覆盖其样式必须以 .md-view.vditor-reset 开头，并用 :deep 穿透运行时生成的子元素——
   普通后代/同类选择器会因类不在后代位置、或运行时子元素缺 data-v 而静默失效 */
.md-view.vditor-reset {
  padding: 0;
}

/* 代码框（样例输入/输出等）与行内代码统一 15px（vditor 默认 85%≈13.6px，数字可读性差） */
.md-view.vditor-reset :deep(pre > code),
.md-view.vditor-reset :deep(:not(pre) > code) {
  font-size: 15px;
}

.md-view :deep(pre) {
  position: relative; /* 复制按钮的定位基准 */
}

.md-view :deep(.md-copy-btn) {
  position: absolute;
  top: 6px;
  right: 6px;
  z-index: 1;
  padding: 1px 8px; /* 15px 字号下收紧行高与内边距，保持徽章紧凑 */
  font-size: 15px;
  line-height: 1.3;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--el-color-primary-light-7);
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.15s;
}

.md-view :deep(.md-copy-btn:hover) {
  color: #fff;
  background: var(--el-color-primary);
  border-color: var(--el-color-primary);
}

.md-view :deep(.md-copy-btn.copied) {
  color: var(--el-color-success);
  background: var(--el-color-success-light-9);
  border-color: var(--el-color-success-light-7);
}
</style>

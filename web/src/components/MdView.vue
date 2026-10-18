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
    el.value.innerHTML = '<span style="color:var(--el-text-color-secondary)">暂无内容</span>'
    return
  }
  if (markdownBytes(props.content) > MAX_MARKDOWN_BYTES) {
    el.value.innerHTML = '<span style="color:var(--el-color-danger)">内容超过 256KB 上限，为避免页面卡顿已停止渲染</span>'
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

// 代码块复制按钮：vditor 自带图标过隐蔽，替换为统一「复制」按钮，成功后内联提示「已复制」
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
    // 语言徽标：挂 pre 左上角（右上角是复制按钮，构成代码块顶栏）；无语言的块不加
    pre.querySelectorAll('code').forEach((codeEl) => {
      const m = /language-([\w+#.-]+)/.exec(codeEl.className)
      if (m && !pre.querySelector('.md-lang-badge')) {
        pre.classList.add('has-lang')
        const badge = document.createElement('span')
        badge.className = 'md-lang-badge'
        badge.textContent = m[1]
        pre.appendChild(badge)
      }
    })
  })
}

onMounted(render)
watch(() => props.content, render)
</script>

<template>
  <div class="md-view" ref="el"></div>
</template>

<style scoped>
/* vditor 把 vditor-reset 类挂在组件根元素自身：覆盖须以 .md-view.vditor-reset 开头
   并用 :deep 穿透运行时子元素，普通后代选择器会静默失效 */
.md-view.vditor-reset {
  padding: 0;
}

/* 内容列限宽 860 居中（与编辑端同规则，见 main.css） */
.md-view {
  max-width: 860px;
  margin: 0 auto;
}

.md-view :deep(pre) {
  position: relative; /* 复制按钮与语言徽标的定位基准 */
}

/* 语言徽标 + 让出首行空间（右上角为复制按钮，两端构成代码块顶栏） */
.md-view :deep(pre.has-lang) {
  padding-top: 34px;
}

.md-view :deep(.md-lang-badge) {
  position: absolute;
  top: 7px;
  left: 10px;
  padding: 0 8px;
  font-size: 12px;
  line-height: 20px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
}

.md-view :deep(.md-copy-btn) {
  position: absolute;
  top: 6px;
  right: 6px;
  z-index: 1;
  padding: 1px 8px;
  font-size: 14px; /* 内容区 16px 环境下显式对齐全站界面字号 */
  line-height: 1.3;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--el-color-primary-light-7);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s;
}

.md-view :deep(.md-copy-btn:hover) {
  color: var(--el-color-white);
  background: var(--el-color-primary);
  border-color: var(--el-color-primary);
}

.md-view :deep(.md-copy-btn.copied) {
  color: var(--el-color-success);
  background: var(--el-color-success-light-9);
  border-color: var(--el-color-success-light-7);
}
</style>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'

import { copyText } from '@/utils/clipboard'
import {
  MAX_RICHTEXT_BYTES,
  highlightCodeIn,
  renderMathIn,
  richtextBytes,
  sanitizeRichHTML,
} from '@/utils/richtext'

// 查看端（v0.6 换血）：HTML 直出 + 出口消毒 + KaTeX 数学 + 代码高亮，一段管道。
// 内容由服务端同引擎转换器产出（存量 md 读时计算，plan.md §2.2），编辑端 Tiptap 同契约；
// 超限拒渲染（256KB，防病态内容卡死主线程的口径换血前后一致）。
const props = defineProps<{ content: string }>()
const el = ref<HTMLDivElement>()

async function render() {
  if (!el.value) return
  if (!props.content) {
    el.value.innerHTML = '<span style="color:var(--el-text-color-secondary)">暂无内容</span>'
    return
  }
  if (richtextBytes(props.content) > MAX_RICHTEXT_BYTES) {
    el.value.innerHTML = '<span style="color:var(--el-color-danger)">内容超过 256KB 上限，为避免页面卡顿已停止渲染</span>'
    return
  }
  // 消毒在前：契约属性（data-latex）经白名单放行，数学渲染在其产物上进行
  el.value.innerHTML = sanitizeRichHTML(props.content)
  await renderMathIn(el.value)
  await highlightCodeIn(el.value)
  mountCopyButtons()
}

// 代码块顶栏：语言徽标（左上）+ 复制按钮（右上）
function mountCopyButtons() {
  if (!el.value) return
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
  <div class="md-view rich" ref="el"></div>
</template>

<style scoped>
/* 内容列限宽 860 居中（排版规则在 main.css .rich 段） */
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

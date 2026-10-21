<script setup lang="ts">
// 查看端内芯（v0.8）：Vditor.preview 客户端直渲 Markdown（取代 v0.6 的服务端转 HTML +
// DOMPurify/KaTeX/lowlight 管道）。Lute 渲染自带 XSS 过滤（preview.markdown.sanitize 默认开），
// 公式/代码高亮由 Vditor 懒加载子资源渲染（自托管 /vditor）；内容列限宽 860 居中。
// 排版在 main.css .soj-md（与编辑端共用一套站内字阶与行内代码样式）。
// 查看端固定 light 内容主题：内容主题是全局单例 <link>，跟着编辑器偏好走会把
// 全站题面/公告染成暗色（阅读面与站点整体视觉应保持一致）。
// 超限拒渲染（256KB，防病态内容卡死主线程——口径延续 v0.6 之前）。
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import 'vditor/dist/index.css'

import { MAX_CONTENT_BYTES, bindCopyFeedback, contentBytes } from '@/utils/md'

const props = defineProps<{ content: string }>()

const el = ref<HTMLDivElement>()
const notice = ref('')
let unbindCopy: (() => void) | null = null

async function render() {
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
  Vditor.preview(el.value, props.content, {
    mode: 'light',
    lang: 'zh_CN',
    cdn: '/vditor',
    theme: { current: 'light' },
    hljs: { lineNumber: false },
    math: { engine: 'KaTeX', inlineDigit: true }, // inlineDigit: $ 后允许数字开头（默认 false，会让 $1 \le N$ 不被识别）
    markdown: { sanitize: true },
  })
}

onMounted(() => {
  // 复制反馈（复制 → 已复制，3s 恢复）：容器持久，渲染/重渲染一次绑定即可
  if (el.value) unbindCopy = bindCopyFeedback(el.value)
  render()
})
watch(() => props.content, render)

onBeforeUnmount(() => {
  unbindCopy?.()
  unbindCopy = null
})
</script>

<template>
  <!-- soj-md = 站内 Markdown 排版（main.css），md-light = light 主题下的站内配色。
       挂在 .md-view 外层：Vditor.preview 会给内容容器追加 vditor-reset 类，
       排版规则是 .soj-md .vditor-reset 后代选择器 -->
  <div class="md-view soj-md md-light">
    <div v-if="notice" class="md-view-notice">{{ notice }}</div>
    <div ref="el" class="md-view-body"></div>
  </div>
</template>

<style scoped>
/* 内容列限宽 860 居中（.vditor-reset 是 Vditor 的内容根） */
.md-view {
  max-width: 860px;
  margin: 0 auto;
}

.md-view-notice {
  color: var(--el-text-color-secondary);
}
</style>

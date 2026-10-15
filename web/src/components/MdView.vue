<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import DOMPurify from 'dompurify'
import { loadVditor, MAX_MARKDOWN_BYTES, markdownBytes } from '@/utils/vditor'

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
}

onMounted(render)
watch(() => props.content, render)
</script>

<template>
  <div class="md-view" ref="el"></div>
</template>

<style scoped>
.md-view :deep(.vditor-reset) {
  padding: 0;
}
</style>

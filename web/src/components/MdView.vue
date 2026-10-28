<script setup lang="ts">
// 查看端薄壳（v0.8；v0.30 增 html 透传）：props 原样透传给异步内芯 MdViewInner——
// Vditor 只进查看端懒 chunk（与编辑器共享同一 vditor chunk），任何挂载 MdView 的页面首屏零增量。
// html = 服务端写入时渲染的 HTML 缓存（v0.30）：有值时内芯走 html 模式（零引擎零渲染 pass），
// 空值自动回退 md 客户端渲染（存量兜底）。
import { defineAsyncComponent } from 'vue'

const MdViewInner = defineAsyncComponent(() => import('./MdViewInner.vue'))

defineProps<{ content: string; html?: string }>()
</script>

<template>
  <MdViewInner :content="content" :html="html" />
</template>

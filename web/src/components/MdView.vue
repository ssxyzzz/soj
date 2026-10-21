<script setup lang="ts">
// 查看端薄壳（v0.8）：props/content 原样透传给异步内芯 MdViewInner——
// Vditor 只进查看端懒 chunk（与编辑器共享同一 vditor chunk），任何挂载 MdView 的页面首屏零增量。
import { defineAsyncComponent } from 'vue'

const MdViewInner = defineAsyncComponent(() => import('./MdViewInner.vue'))

defineProps<{ content: string }>()
</script>

<template>
  <MdViewInner :content="content" />
</template>

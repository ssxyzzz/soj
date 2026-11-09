<script setup lang="ts">
// 查看端薄壳（v0.8；v0.53 渲染收口改 html-only）：props 原样透传给异步内芯
// MdViewInner——查看链路只有 html（写入时渲染缓存），md 客户端渲染已随 vditor
// 全站退役移除，任何页面首屏零引擎。
import { defineAsyncComponent } from 'vue'

const MdViewInner = defineAsyncComponent(() => import('./MdViewInner.vue'))

defineProps<{ html: string; annotatable?: boolean }>()
</script>

<template>
  <MdViewInner :html="html" :annotatable="annotatable" />
</template>

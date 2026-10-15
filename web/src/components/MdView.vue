<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import Vditor from 'vditor'
import 'vditor/dist/index.css'

// 查看端与编辑端共用 Vditor 引擎渲染，保证「格式统一不乱」；
// 内容由老师/root 维护（题面/公告/考试说明），非用户生成内容。
const props = defineProps<{ content: string }>()
const el = ref<HTMLDivElement>()

async function render() {
  if (!el.value) return
  if (!props.content) {
    el.value.innerHTML = '<span style="color:#909399">暂无内容</span>'
    return
  }
  // vditor 的 d.ts 未声明静态 preview 方法（运行时存在），此处做类型断言
  const preview = (Vditor as unknown as {
    preview: (el: HTMLElement, md: string, opts?: Record<string, unknown>) => Promise<void>
  }).preview
  await preview(el.value, props.content, { cdn: '/vditor' })
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

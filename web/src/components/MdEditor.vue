<script setup lang="ts">
// Markdown 编辑器薄壳（v0.8）：props/modelValue 原样透传给异步内芯 MdEditorInner——
// Vditor 只进编辑器懒 chunk，任何挂载 MdEditor 的页面首屏零增量。
import { defineAsyncComponent } from 'vue'

const MdEditorInner = defineAsyncComponent(() => import('./MdEditorInner.vue'))

const props = withDefaults(
  defineProps<{
    modelValue: string
    height?: string
    /** 工具栏档位：full = 文档型场景（题面/公告/赛事介绍，默认）；compact = 短内容瘦身（题解/说明，去表格/图片/全屏） */
    variant?: 'full' | 'compact'
    /** 高度随内容自生长（v0.9.3）：短内容不留大片空白，超过视口一定比例才出滚动条 */
    autoHeight?: boolean
  }>(),
  {
    height: '560px',
    variant: 'full',
    autoHeight: false,
  },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()
</script>

<template>
  <MdEditorInner
    :model-value="props.modelValue"
    :height="props.height"
    :variant="props.variant"
    :auto-height="props.autoHeight"
    @update:model-value="(v: string) => emit('update:modelValue', v)"
  />
</template>

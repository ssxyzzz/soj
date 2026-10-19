<script setup lang="ts">
// 富文本编辑器薄壳（v0.6）：props/modelValue 原样透传给异步内芯 MdEditorInner——
// Tiptap 全家桶只进编辑器懒 chunk，任何挂载 MdEditor 的页面首屏零增量
// （与 vditor/CM6 的动态 import 同策略，见 plan.md §2.2）。
import { defineAsyncComponent } from 'vue'

const MdEditorInner = defineAsyncComponent(() => import('./MdEditorInner.vue'))

const props = withDefaults(
  defineProps<{
    modelValue: string
    height?: string
    /** 自生长上限：传入后编辑器高度随内容在 height → maxHeight 间生长，超出内部滚动；不传维持固定高 */
    maxHeight?: string
    /** 工具栏档位：full = 文档型场景（题面/公告/赛事介绍，默认）；compact = 短内容瘦身（题解/说明，去表格/图片/全屏） */
    variant?: 'full' | 'compact'
  }>(),
  {
    height: '420px',
    maxHeight: undefined,
    variant: 'full',
  },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()
</script>

<template>
  <MdEditorInner
    :model-value="props.modelValue"
    :height="props.height"
    :max-height="props.maxHeight"
    :variant="props.variant"
    @update:model-value="(v: string) => emit('update:modelValue', v)"
  />
</template>

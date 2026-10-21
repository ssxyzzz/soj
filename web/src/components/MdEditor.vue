<script setup lang="ts">
// Markdown 编辑器薄壳（v0.8）：props/modelValue 原样透传给异步内芯 MdEditorInner——
// Vditor 只进编辑器懒 chunk，任何挂载 MdEditor 的页面首屏零增量。
import { defineAsyncComponent, defineComponent, h } from 'vue'

// 异步内芯的加载/失败兜底：服务重启或发版后旧 chunk 可能 404，
// 不能让编辑器位置只剩空白；重试两次仍失败则提示刷新页面。
const AsyncFallback = defineComponent({
  name: 'MdEditorAsyncFallback',
  props: {
    error: { type: Object, default: null },
    retry: { type: Function, default: null },
  },
  setup(props) {
    return () =>
      props.error
        ? h('div', { class: 'md-editor-async-fallback is-error' }, [
            h('span', '编辑器加载失败，可能是页面版本已更新。'),
            h('button', { type: 'button', onClick: () => props.retry?.() }, '重试'),
            h('button', { type: 'button', onClick: () => window.location.reload() }, '刷新页面'),
          ])
        : h('div', { class: 'md-editor-async-fallback' }, '编辑器加载中…')
  },
})

const MdEditorInner = defineAsyncComponent({
  loader: () => import('./MdEditorInner.vue'),
  loadingComponent: AsyncFallback,
  errorComponent: AsyncFallback,
  delay: 120,
  timeout: 20000,
  onError(_error, retry, fail, attempts) {
    if (attempts < 3) window.setTimeout(retry, 400 * attempts)
    else fail()
  },
})

const props = withDefaults(
  defineProps<{
    modelValue: string
    height?: string
    /** 工具栏档位：full = 文档型场景（题面/公告/赛事介绍，默认）；compact = 短内容瘦身（题解/说明，去表格/图片/全屏） */
    variant?: 'full' | 'compact'
    /** 高度随内容自生长（v0.9.3；v0.9.5 起不设视口上限）：短内容不留大片空白 */
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

<style>
.md-editor-async-fallback {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-height: 96px;
  padding: 12px;
  border: 1px dashed var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-body, 14px);
}

.md-editor-async-fallback.is-error {
  color: var(--el-color-danger);
  border-color: var(--el-color-danger-light-5);
}

.md-editor-async-fallback button {
  padding: 0;
  border: 0;
  background: none;
  color: var(--el-color-primary);
  font: inherit;
  cursor: pointer;
}
</style>

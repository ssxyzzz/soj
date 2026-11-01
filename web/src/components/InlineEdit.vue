<script setup lang="ts">
// 就地编辑文本（题单编辑器大纲行用）：点击文字变输入框，blur/Enter 落值、Esc 取消。
// 标题/描述同一组件：variant 决定视觉权重（title 加重 / desc 弱化灰 + 占位提示）。
import { nextTick, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string // 空值时的占位文字（如「添加描述」）
    maxlength?: string | number // 模板静态属性传字符串、脚本传数字，两可
    variant?: 'title' | 'desc'
  }>(),
  { placeholder: '', maxlength: 32, variant: 'title' },
)

const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const editing = ref(false)
const buf = ref('')
const inputRef = ref<InstanceType<typeof import('element-plus')['ElInput']>>()

function start() {
  buf.value = props.modelValue
  editing.value = true
  nextTick(() => inputRef.value?.focus())
}

function commit() {
  if (!editing.value) return
  editing.value = false
  const v = buf.value.trim()
  if (v !== props.modelValue) emit('update:modelValue', v)
}

function cancel() {
  editing.value = false
}
</script>

<template>
  <el-input
    v-if="editing"
    ref="inputRef"
    v-model="buf"
    :maxlength="maxlength"
    :class="['ie-input', `ie-${variant}`]"
    @blur="commit"
    @keydown.enter.prevent="commit"
    @keydown.esc.prevent="cancel"
    @click.stop
  />
  <span
    v-else
    :class="['ie-text', `ie-${variant}`, { 'ie-empty': !modelValue }]"
    :title="placeholder"
    @click.stop="start"
  >{{ modelValue || placeholder }}</span>
</template>

<style scoped>
.ie-text {
  cursor: text;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ie-title {
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-navy);
}

.ie-desc {
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-13);
}

.ie-empty {
  color: var(--el-text-color-placeholder);
  font-weight: var(--soj-fw-regular);
}

.ie-input {
  width: 100%;
}
</style>

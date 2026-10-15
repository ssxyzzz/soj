<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import Vditor from 'vditor'
import 'vditor/dist/index.css'

const props = defineProps<{ modelValue: string; height?: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const el = ref<HTMLDivElement>()
let inst: Vditor | null = null
let inner = props.modelValue // 防止 setValue 触发的回显循环

onMounted(() => {
  inst = new Vditor(el.value!, {
    cdn: '/vditor', // 自托管资源（构建时随镜像分发，不依赖外网）
    height: props.height ?? '420px',
    mode: 'wysiwyg',
    value: props.modelValue,
    cache: { enable: false },
    placeholder: '支持 Markdown 与所见即所得，右上角可切换编辑模式',
    toolbar: [
      'headings', 'bold', 'italic', 'strike', '|',
      'list', 'ordered-list', 'check', 'outdent', 'indent', '|',
      'quote', 'code', 'inline-code', 'insert-code', '|',
      'link', 'table', '|',
      'undo', 'redo', '|',
      'edit-mode', '|',
      'fullscreen', 'export',
    ],
    input: (v) => {
      inner = v
      emit('update:modelValue', v)
    },
  })
})

watch(() => props.modelValue, (v) => {
  if (inst && v !== inner) {
    inst.setValue(v ?? '')
    inner = v
  }
})

onUnmounted(() => inst?.destroy())
</script>

<template>
  <div ref="el"></div>
</template>

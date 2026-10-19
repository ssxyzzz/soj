<script setup lang="ts">
// 公式编辑弹层（v0.6）：TeX 源码 + 实时预览（KaTeX）。
// 所有编辑器（含 vditor 时代）公式都是改源码，弹层 + 预览只是把这件事做顺手的交互壳。
import { computed, ref, watch } from 'vue'
import { ElButton, ElDialog, ElInput } from 'element-plus'

import { loadKatex } from '@/utils/richtext'

const props = defineProps<{ initialLatex: string }>()
const emit = defineEmits<{ (e: 'save', latex: string): void; (e: 'close'): void }>()

const latex = ref(props.initialLatex)
const preview = ref('')

const dirty = computed(() => latex.value !== props.initialLatex)

async function renderPreview() {
  const katex = await loadKatex()
  try {
    preview.value = latex.value.trim()
      ? katex.renderToString(latex.value, { displayMode: true, throwOnError: false })
      : ''
  } catch {
    preview.value = ''
  }
}

watch(latex, renderPreview, { immediate: true })

function save() {
  emit('save', latex.value.trim())
}
</script>

<template>
  <el-dialog
    :model-value="true"
    :title="dirty ? '编辑公式（TeX）' : '公式（TeX）'"
    width="560px"
    @close="emit('close')"
  >
    <el-input
      v-model="latex"
      type="textarea"
      :rows="3"
      placeholder="TeX 源码，如 e^{i\pi}+1=0"
      spellcheck="false"
    />
    <div v-if="preview" class="math-preview" v-html="preview" />
    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" @click="save">确定</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.math-preview {
  margin-top: 12px;
  padding: 14px;
  text-align: center;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
  overflow-x: auto;
}
</style>

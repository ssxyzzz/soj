<script setup lang="ts">
// 只读代码窗（2026-10-27）：CodeEditor 只读态渲染——语法高亮、行号与代码编辑器同款
// 背景字体，右上角悬浮复制按钮。评测详情 / 我的提交 / 所有代码等代码查看场景共用，
// 取代各处背景不一的 <pre>（soj-codepane 深蓝 / sub-code 浅色混用）。
import { ref } from 'vue'

import CodeEditor from '@/components/CodeEditor.vue'
import { copyText } from '@/utils/clipboard'

const props = withDefaults(
  defineProps<{
    code?: string
    language?: string // cpp / c / python3（决定高亮）
    height?: string
  }>(),
  { code: '', language: 'cpp', height: '360px' },
)

const copied = ref(false)
async function copy() {
  await copyText(props.code ?? '', '代码已复制')
  copied.value = true
  window.setTimeout(() => (copied.value = false), 1500)
}
</script>

<template>
  <div class="code-pane">
    <CodeEditor :model-value="code" :language="language" :height="height" :autocomplete="false" read-only />
    <el-button class="copy-btn" size="small" :type="copied ? 'success' : 'default'" @click="copy">
      {{ copied ? '已复制' : '复制' }}
    </el-button>
  </div>
</template>

<style scoped>
.code-pane {
  position: relative; /* 复制按钮的定位锚 */
}

/* 悬浮右上角：压在编辑器边框内缘，首行代码极少写到最右，不遮内容 */
.copy-btn {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 5;
}
</style>

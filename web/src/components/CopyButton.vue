<script setup lang="ts">
// 文字复制按钮（全站复制入口的统一观感）：主蓝文字、无图标无描边，点击后内联翻成绿色的
// 「已复制」并弹 toast，COPIED_RESET_MS 后恢复。代码 / 命令类复制入口的唯一 Vue 实现——
// 备份面板恢复命令行、CodePane 右上角共用；Markdown 代码块里的同类按钮由 utils/md.ts
// 直插 DOM（无 Vue 上下文），观感与反馈在 main.css .md-copy-btn 对齐本组件
// （EP link / primary / small 文字按钮）。
import { onBeforeUnmount, ref } from 'vue'

import { COPIED_RESET_MS, copyText } from '@/utils/clipboard'

const props = withDefaults(defineProps<{ text: string; successMsg?: string }>(), {
  successMsg: '已复制到剪贴板',
})

const copied = ref(false)
let timer = 0
async function copy() {
  if (!(await copyText(props.text ?? '', props.successMsg))) return
  copied.value = true
  window.clearTimeout(timer)
  timer = window.setTimeout(() => (copied.value = false), COPIED_RESET_MS)
}
onBeforeUnmount(() => window.clearTimeout(timer))
</script>

<template>
  <el-button size="small" link :type="copied ? 'success' : 'primary'" @click="copy">
    {{ copied ? '已复制' : '复制' }}
  </el-button>
</template>

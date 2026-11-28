<script setup lang="ts">
// 只读代码窗（2026-10-27）：CodeEditor 只读态渲染——语法高亮、行号与代码编辑器同款
// 背景字体，右上角文字复制按钮（CopyButton，全站统一的复制入口观感）。评测详情 / 我的提交 /
// 所有代码 / 提交明细等代码查看场景共用，取代各处背景不一的 <pre>（soj-codepane 深蓝 /
// sub-code 浅色混用）。
import { onBeforeUnmount, ref } from 'vue'

import CodeEditor from '@/components/CodeEditor.vue'
import CopyButton from '@/components/CopyButton.vue'

withDefaults(
  defineProps<{
    code?: string
    language?: string // cpp / c / python3（决定高亮）
    height?: string
  }>(),
  { code: '', language: 'cpp', height: '360px' },
)

const scrollBarW = ref(0) // 编辑器纵向滚动条的占位宽度（没有滚动条 / 覆盖式滚动条 = 0）

// 悬浮的复制按钮要让开滚动条。占位宽度只有量出来才知道（经典滚动条占位、覆盖式不占位，
// 各平台不同），CM 的滚动容器就位后量一次；之后滚动条出现 / 消失会让内容箱变宽变窄，
// ResizeObserver 跟着重量
let ro: ResizeObserver | null = null
function onReady(scrollDOM: HTMLElement) {
  const measure = () => (scrollBarW.value = scrollDOM.offsetWidth - scrollDOM.clientWidth)
  measure()
  ro = new ResizeObserver(measure)
  ro.observe(scrollDOM)
}
onBeforeUnmount(() => ro?.disconnect())
</script>

<template>
  <div class="code-pane" :style="{ '--copy-right': `${8 + scrollBarW}px` }">
    <CodeEditor
      :model-value="code"
      :language="language"
      :height="height"
      :autocomplete="false"
      read-only
      @ready="onReady"
    />
    <CopyButton class="copy-btn" :text="code ?? ''" success-msg="代码已复制" />
  </div>
</template>

<style scoped>
.code-pane {
  position: relative; /* 复制按钮的定位锚 */
}

/* 悬浮首行右端：按钮盒与首行代码的行盒重合——top = 编辑器 1px 边框 + cm-content 上内距 4px，
   高 = 一行（14px × 1.6，CodeEditor 默认档），文字与首行代码同一条中线。长代码行不折行，
   会横向滚到按钮下面，垫一层编辑器底色遮住（观感仍是纯文字按钮）；right 让开纵向滚动条的
   占位（见 onReady）。EP 的 link 态自带透明底、悬停规则特异度更高，这里按 .el-button.is-link
   叠写提级才压得住 */
.code-pane .el-button.is-link.copy-btn {
  position: absolute;
  top: 5px;
  right: var(--copy-right, 8px);
  z-index: 5;
  box-sizing: border-box;
  height: 22.4px;
  background: var(--el-bg-color);
}

/* 首行带 active 高亮条时垫底跟随高亮底色，高亮条右端不开口子（弹窗打开时焦点落进编辑器，
   首行即带高亮）。色值对齐 CodeMirror baseTheme 的 .cm-activeLine（#cceeff44） */
.code-pane:has(:deep(.cm-line:first-child.cm-activeLine)) .el-button.is-link.copy-btn {
  background: color-mix(in srgb, rgb(204 238 255) 27%, var(--el-bg-color));
}
</style>

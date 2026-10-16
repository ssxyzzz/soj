<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import MdView from '@/components/MdView.vue'
import { defaultLang, languageOptions, templateFor } from '@/utils/code'

// 在线编程（v0.4.6 右列入口）：全屏覆盖层，左侧题面（MdView 同引擎渲染）、
// 右侧代码，中间分隔条可拖动调整两栏宽度；提交动作由父组件接手。
const props = defineProps<{
  modelValue: boolean
  title: string
  statement: string
  disabled?: boolean
  disabledReason?: string
  submitting?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'submit', lang: string, code: string): void
}>()

const lang = ref(defaultLang()) // 个人设置的默认语言优先
const code = ref('')
const leftPct = ref(50)
const divider = ref<HTMLElement>()

watch(() => props.modelValue, (open) => {
  if (open) code.value = code.value || templateFor(lang.value)
})

function changeLang(l: string) {
  if (!code.value.trim() || code.value === templateFor(lang.value)) code.value = templateFor(l)
  lang.value = l
}

function doSubmit() {
  if (props.disabled) return
  if (!code.value.trim()) {
    ElMessage.warning('代码不能为空')
    return
  }
  emit('submit', lang.value, code.value)
}

// ---- 拖动分栏：以容器百分比记录左栏宽度，夹在 15%~85% ----
let dragging = false
let containerEl: HTMLElement | null = null

function onDragStart(e: MouseEvent) {
  dragging = true
  containerEl = divider.value?.parentElement ?? null
  document.addEventListener('mousemove', onDragMove)
  document.addEventListener('mouseup', onDragEnd)
  e.preventDefault()
}

function onDragMove(e: MouseEvent) {
  if (!dragging || !containerEl) return
  const rect = containerEl.getBoundingClientRect()
  const pct = ((e.clientX - rect.left) / rect.width) * 100
  leftPct.value = Math.min(85, Math.max(15, pct))
}

function onDragEnd() {
  dragging = false
  document.removeEventListener('mousemove', onDragMove)
  document.removeEventListener('mouseup', onDragEnd)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.modelValue) emit('update:modelValue', false)
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKeydown)
  onDragEnd()
})
</script>

<template>
  <teleport to="body">
    <div v-if="modelValue" class="workbench">
      <header class="wb-header">
        <span class="wb-title">{{ title }}</span>
        <div class="wb-actions">
          <el-select :model-value="lang" size="default" style="width: 140px" @change="changeLang">
            <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
          </el-select>
          <el-button type="primary" :disabled="disabled" :loading="submitting" @click="doSubmit">提交</el-button>
          <el-button @click="emit('update:modelValue', false)">退出全屏（Esc）</el-button>
        </div>
      </header>
      <div v-if="disabled" class="wb-disabled">{{ disabledReason }}</div>
      <div class="wb-body">
        <section class="wb-left" :style="{ width: leftPct + '%' }">
          <MdView :content="statement" />
        </section>
        <div ref="divider" class="wb-divider" @mousedown="onDragStart" title="拖动调整分栏" />
        <section class="wb-right">
          <textarea
            v-model="code"
            class="wb-code mono"
            spellcheck="false"
            placeholder="在此编写或粘贴你的代码（系统不提供默认模板）"
          />
        </section>
      </div>
    </div>
  </teleport>
</template>

<style scoped>
.workbench {
  position: fixed;
  inset: 0;
  z-index: 2000;
  background: #fff;
  display: flex;
  flex-direction: column;
}

.wb-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 10px 20px;
  border-bottom: 1px solid #e4e7ed;
  flex: none;
}

.wb-title {
  font-weight: 600;
  font-size: 15px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.wb-actions {
  display: flex;
  gap: 8px;
  flex: none;
}

.wb-disabled {
  padding: 8px 20px;
  background: #fdf6ec;
  color: #e6a23c;
  font-size: 13px;
  flex: none;
}

.wb-body {
  flex: 1;
  display: flex;
  min-height: 0;
}

.wb-left {
  overflow: auto;
  padding: 16px 24px;
  min-width: 0;
}

.wb-divider {
  flex: none;
  width: 6px;
  cursor: col-resize;
  background: #f0f2f5;
  transition: background 0.2s;
}

.wb-divider:hover {
  background: #c6e2ff;
}

.wb-right {
  flex: 1;
  min-width: 0;
  display: flex;
}

.wb-code {
  flex: 1;
  border: none;
  outline: none;
  resize: none;
  padding: 16px;
  font-size: 14px;
  line-height: 1.6;
  background: #fafafa;
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
}
</style>

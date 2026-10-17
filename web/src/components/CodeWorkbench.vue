<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import CodeEditor from '@/components/CodeEditor.vue'
import MdView from '@/components/MdView.vue'
import { authApi } from '@/api'
import { useUserStore } from '@/stores/user'
import { autocompleteOn, defaultLang, languageOptions, mergedPrefs, templateFor } from '@/utils/code'
import type { UserPrefs } from '@/types'

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
const userStore = useUserStore()
const acOn = computed(() => autocompleteOn())
const codeFont = computed(() => userStore.prefs.codeFont || 'Consolas')
const codeFontSize = computed(() => userStore.prefs.codeFontSize || 14)

// 字体/字号即改即存：先乐观更新本地 store（编辑器点击瞬间重绘，不等网络往返），
// 再全量字段落库（整体覆盖语义，见 utils/code.ts mergedPrefs）。
// 竞态守卫：快速连点时多个请求乱序返回，仅最后一次请求的响应允许回写 store。
let prefsSeq = 0
async function saveEditorPrefs(patch: Partial<UserPrefs>) {
  const merged = mergedPrefs(patch)
  userStore.setPrefs(merged)
  const seq = ++prefsSeq
  try {
    const p = await authApi.updateProfile({ prefs: merged })
    if (seq === prefsSeq) userStore.setPrefs(p.prefs ?? merged)
  } catch {
    // 错误提示已由 axios 拦截器统一弹出；本地保持本次会话生效，下次进入以后端为准
  }
}

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
          <el-select :model-value="lang" style="width: 140px" @change="changeLang">
            <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
          </el-select>
          <el-select
            :model-value="codeFont"
            style="width: 130px"
            title="编辑器字体"
            @change="(f: string) => saveEditorPrefs({ codeFont: f })"
          >
            <el-option value="Consolas" label="Consolas" />
            <el-option value="Menlo" label="Menlo" />
            <el-option value="Monaco" label="Monaco" />
          </el-select>
          <el-input-number
            :model-value="codeFontSize"
            :min="12"
            :max="28"
            :step="1"
            controls-position="right"
            style="width: 100px"
            title="编辑器字号（12-28，可输入或 +/- 微调）"
            @change="(n: number | undefined) => n && saveEditorPrefs({ codeFontSize: n })"
          />
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
          <CodeEditor
            v-model="code"
            class="wb-editor"
            :language="lang"
            :autocomplete="acOn"
            :font-size="codeFontSize"
            :font-family="codeFont"
            :tab-indent="true"
            height="100%"
            placeholder="在此编写或粘贴你的代码"
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

.wb-editor {
  flex: 1;
  height: 100%;
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
}
</style>

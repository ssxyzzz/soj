<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import CodeEditor from '@/components/CodeEditor.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import SelfTestPanel from '@/components/SelfTestPanel.vue'
import { authApi } from '@/api'
import { useUserStore } from '@/stores/user'
import { autocompleteOn, defaultLang, languageOptions, mergedPrefs, templateFor } from '@/utils/code'
import type { ProblemDetail, UserPrefs } from '@/types'

// 在线编程（v0.4.6 右列入口）：全屏覆盖层，左侧题面（ProblemStatement 与详情页主列同卡复用）、
// 右侧代码，中间分隔条可拖动调整两栏宽度；提交动作由父组件接手。
const props = defineProps<{
  modelValue: boolean
  title: string
  problem: ProblemDetail // 左栏题面与详情页主列同卡渲染（含标题/限制/文件 IO 提示）
  fileIo?: boolean // 本场启用文件 IO 时左栏与主列同款提示
  disabled?: boolean
  disabledReason?: string
  initialCode?: string // 函数题学生模板（v0.9.1）：打开时预填，优先于个人默认模板
  langOnly?: string | string[] // 语言锁（函数题 = 'cpp'；交互题 = ['cpp','c']）
  submitting?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'submit', lang: string, code: string): void
}>()

const lang = ref(defaultLang()) // 个人设置的默认语言优先

// 语言候选：无锁 = 全量；有锁 = 锁定的那几种（函数题 cpp / 交互题 cpp+c）
const allowedLangs = computed(() => {
  const lock = props.langOnly
  if (!lock) return languageOptions
  const list = Array.isArray(lock) ? lock : [lock]
  return languageOptions.filter((o) => list.includes(o.value))
})
const code = ref('')
const leftPct = ref(46)
// v3 wb-title：题号 mono 前缀与题名分色（'#1003 三个数的和' → mono #1003 + 藏蓝题名）
const titleId = computed(() => `#${props.problem.id}`)
const titleText = computed(() => props.title.replace(/^#\d+\s*/, ''))
const divider = ref<HTMLElement>()
const userStore = useUserStore()
const acOn = computed(() => autocompleteOn())
const codeFont = computed(() => userStore.prefs.codeFont || 'Consolas')
const codeFontSize = computed(() => userStore.prefs.codeFontSize || 14)

// 自测（v0.23）：stdio 传统题可用；面板悬在编辑器上方，自测不计入任何统计
const selfTestOpen = ref(false)
const selfTestEligible = computed(() => props.problem.challenge?.eligible ?? false)

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
  // 全屏期间锁定底层页面滚动：覆盖层只是 fixed 盖在题目页上，底层文档照旧可滚，
  // 窗口右缘会挂着一条无用的页面滚动条（2026-10-29 用户反馈）。overflow hidden
  // 不动 scrollTop，退出后底层页面原地保留浏览位置；卸载路径在 onBeforeUnmount 兜底
  document.documentElement.style.overflow = open ? 'hidden' : ''
  if (open) {
    const lock = props.langOnly
    if (lock) lang.value = Array.isArray(lock) ? lock[0] : lock
    code.value = code.value || props.initialCode || templateFor(lang.value)
  }
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
  // 全屏开着时卸载（如切路由）：滚动锁兜底恢复，避免底层页面被永久锁死
  document.documentElement.style.overflow = ''
})
</script>

<template>
  <teleport to="body">
    <div v-if="modelValue" class="workbench">
      <header class="wb-header">
        <span class="wb-title"><span class="mono wb-no">{{ titleId }}</span> {{ titleText }}</span>
        <div class="wb-actions">
          <el-select :model-value="lang" style="width: 140px" @change="changeLang">
            <el-option v-for="o in allowedLangs" :key="o.value" :value="o.value" :label="o.label" />
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
            :max="48"
            :step="1"
            controls-position="right"
            style="width: 100px"
            title="编辑器字号（12-48，可输入或 +/- 微调）"
            @change="(n: number | undefined) => n && saveEditorPrefs({ codeFontSize: n })"
          />
          <el-button v-if="selfTestEligible" :type="selfTestOpen ? 'primary' : 'default'" @click="selfTestOpen = !selfTestOpen">
            {{ selfTestOpen ? '收起自测' : '自测' }}
          </el-button>
          <el-button type="primary" :disabled="disabled" :loading="submitting" @click="doSubmit">提交</el-button>
          <el-button @click="emit('update:modelValue', false)">退出全屏（Esc）</el-button>
        </div>
      </header>
      <div v-if="disabled" class="wb-disabled">{{ disabledReason }}</div>
      <div class="wb-body">
        <section class="wb-left" :style="{ width: leftPct + '%' }">
          <ProblemStatement :problem="problem" :file-io="fileIo" />
        </section>
        <div ref="divider" class="wb-divider" @mousedown="onDragStart" title="拖动调整分栏" />
        <section class="wb-right">
          <div class="wb-right-col">
            <SelfTestPanel
              v-if="selfTestOpen"
              class="wb-selftest"
              :problem-id="problem.id"
              :language="lang"
              :code="code"
            />
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
          </div>
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
  background: var(--soj-bg-bottom);
  display: flex;
  flex-direction: column;
}

/* v3 wb-header：56 定高毛玻璃（白.9 + blur12） */
.wb-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 14px;
  height: 56px;
  padding: 0 20px;
  background: rgba(251, 253, 255, 0.9);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--soj-line);
  flex: none;
}

.wb-title {
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
  font-size: 15px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.wb-title .wb-no {
  color: var(--soj-blue);
}

.wb-actions {
  display: flex;
  gap: 8px;
  flex: none;
}

/* 工具栏整行等高：select/字号框走全局 --el-component-size 40px，
   按钮弃用 small 档（34px）并钉在 40px，与左侧控件完全对齐（2026-10-26 验收调整） */
.wb-actions .el-button {
  height: 40px;
}

.wb-disabled {
  padding: 8px 20px;
  background: var(--el-color-warning-light-9);
  color: var(--el-color-warning);
  flex: none;
}

.wb-body {
  flex: 1;
  display: flex;
  min-height: 0;
}

.wb-left {
  overflow: auto;
  padding: 22px 28px;
  min-width: 0;
}

.wb-divider {
  flex: none;
  width: 6px;
  cursor: col-resize;
  background: var(--el-fill-color);
  transition: background 0.2s;
}

.wb-divider:hover {
  background: var(--el-color-primary-light-5);
}

.wb-right {
  flex: 1;
  min-width: 0;
  display: flex;
}

/* 自测面板悬在编辑器上方：面板定高收缩，编辑器吃剩余高度 */
.wb-right-col {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-height: 0;
}
.wb-selftest {
  flex: none;
  max-height: 300px;
  overflow: auto;
}
.wb-editor {
  flex: 1;
  height: 100%;
  min-height: 0;
}
</style>

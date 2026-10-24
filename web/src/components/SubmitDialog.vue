<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import CodeEditor from '@/components/CodeEditor.vue'
import { autocompleteOn, defaultLang, languageOptions, templateFor } from '@/utils/code'
import { useUserStore } from '@/stores/user'

// 提交弹窗（v3-full：窄版 720 + 深色代码窗，赛事做题页「提交」按钮通道）：语言选择 +
// 代码编辑，提交动作由父组件通过 submit 事件接手（练习走 /submissions，比赛/考试走各自通道）。
const props = defineProps<{
  modelValue: boolean
  title: string
  disabled?: boolean
  disabledReason?: string
  submitting?: boolean
  initialCode?: string // 函数题学生模板（v0.9.1）：打开时预填，优先于个人默认模板
  langOnly?: string | string[] // 语言锁（函数题 = 'cpp'；交互题 = ['cpp','c']）
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'submit', lang: string, code: string): void
}>()

const lang = ref(defaultLang()) // 个人设置的默认语言优先
const code = ref('')
const userStore = useUserStore()
const acOn = computed(() => autocompleteOn())
const codeFont = computed(() => userStore.prefs.codeFont || 'Consolas')
const codeFontSize = computed(() => userStore.prefs.codeFontSize || 14)

watch(() => props.modelValue, (open) => {
  if (open) {
    const lock = props.langOnly
    if (lock) lang.value = Array.isArray(lock) ? lock[0] : lock
    code.value = props.initialCode || templateFor(lang.value)
  }
})

function changeLang(l: string) {
  // 仅在代码还是初始模板（未编辑）时跟随切换，避免覆盖已粘贴的代码
  if (!code.value.trim() || code.value === templateFor(lang.value)) code.value = templateFor(l)
  lang.value = l
}

// 语言候选：无锁 = 全量；有锁 = 锁定的那几种（函数题 cpp / 交互题 cpp+c）
const allowedLangs = computed(() => {
  const lock = props.langOnly
  if (!lock) return languageOptions
  const list = Array.isArray(lock) ? lock : [lock]
  return languageOptions.filter((o) => list.includes(o.value))
})

function doSubmit() {
  if (props.disabled) return
  if (!code.value.trim()) {
    ElMessage.warning('代码不能为空')
    return
  }
  emit('submit', lang.value, code.value)
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="`提交 · ${title}`"
    width="720"
    top="6vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="bar">
      <span class="bar-label">语言</span>
      <el-select :model-value="lang" style="width: 240px" @change="changeLang">
        <el-option v-for="o in allowedLangs" :key="o.value" :value="o.value" :label="o.label" />
      </el-select>
      <span v-if="disabled" class="dim">{{ disabledReason }}</span>
    </div>
    <CodeEditor
      v-model="code"
      :language="lang"
      :autocomplete="acOn"
      :font-size="codeFontSize"
      :font-family="codeFont"
      height="460px"
      placeholder="在此编写或粘贴你的代码（偏好里设置的代码模板已填入）"
    />
    <p class="hint">提交间隔受全站限速约束；评测在 go-judge 沙箱中运行。</p>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :disabled="disabled" :loading="submitting" @click="doSubmit">提交评测</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

/* v3 frow 语言行：13/600 标签列 */
.bar-label {
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-ink-2);
}

.hint {
  margin: 12px 0 0;
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
}
</style>

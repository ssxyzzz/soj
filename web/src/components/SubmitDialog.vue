<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import { defaultLang, languageOptions, templateFor } from '@/utils/code'

// 提交弹窗（v0.4.6 右列入口）：语言选择 + 代码粘贴，提交动作由父组件
// 通过 submit 事件接手（练习走 /submissions，比赛/考试走各自通道）。
const props = defineProps<{
  modelValue: boolean
  title: string
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

watch(() => props.modelValue, (open) => {
  if (open) code.value = templateFor(lang.value)
})

function changeLang(l: string) {
  // 仅在代码还是初始模板（未编辑）时跟随切换，避免覆盖已粘贴的代码
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
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="`提交代码 · ${title}`"
    width="720"
    top="6vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="bar">
      <el-select :model-value="lang" style="width: 160px" @change="changeLang">
        <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
      </el-select>
      <span v-if="disabled" class="dim">{{ disabledReason }}</span>
    </div>
    <el-input
      v-model="code"
      type="textarea"
      :rows="16"
      class="mono"
      spellcheck="false"
      placeholder="在此编写或粘贴你的代码（系统不提供默认模板）"
    />
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :disabled="disabled" :loading="submitting" @click="doSubmit">提交</el-button>
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

.dim {
  color: #909399;
  font-size: 13px;
}

.mono :deep(textarea) {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
  line-height: 1.5;
}
</style>

<script setup lang="ts">
// AI 生成作业/考试总结弹窗（v0.97；点名表 + 风格选择）：打开先拉点名表——服务端
// 权威剔除老师、默认勾选至少做出 1 题的学生（考试=有提交者，0 题学生列出供手勾），
// 生成只按勾选名单出数据，名单外的老师/缺席同学进不了 prompt，想提也提不了。
// 风格（受众=家长）弹窗内选，默认专业贴心；结果纯文本复制即走（不落库）。
import { computed, ref, watch } from 'vue'

import { copyText } from '@/utils/clipboard'
import type { RollcallStudent } from '@/types'

// 小结风格（与后端 summaryStyles 枚举一一对应；人设说明在后端 prompt 里）
const STYLES = [
  { id: 'professional', label: '专业贴心' },
  { id: 'warm', label: '温暖鼓励' },
  { id: 'lively', label: '活泼亲切' },
  { id: 'formal', label: '严谨汇报' },
  { id: 'strict', label: '严师督促' },
]

const props = defineProps<{
  modelValue: boolean
  scene: string // 场景名（作业/考试），标题文案用
  loadStudents: () => Promise<RollcallStudent[]> // 宿主页注入的点名表拉取
  generate: (style: string, uids: number[], notes: string) => Promise<string> // 宿主页注入的生成调用（含 130s 长超时）
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const style = ref('professional')
const students = ref<RollcallStudent[]>([])
const defaultUids = ref<number[]>([])
const rollLoading = ref(false)
const notes = ref('')
const summary = ref('')
const busy = ref(false)

const selCount = computed(() => students.value.filter((s) => s.checked).length)

// 每次打开重置点名表与结果（风格选择保留为会话内偏好）；即用即走不留上一次的结果
watch(
  () => props.modelValue,
  async (v) => {
    if (!v) return
    notes.value = ''
    summary.value = ''
    students.value = []
    defaultUids.value = []
    rollLoading.value = true
    try {
      students.value = await props.loadStudents()
      defaultUids.value = students.value.filter((s) => s.checked).map((s) => s.uid)
    } catch {
      students.value = [] // 失败拦截器已统一 toast，列表留空可关弹窗重开
    } finally {
      rollLoading.value = false
    }
  },
)

function restoreDefault() {
  for (const s of students.value) s.checked = defaultUids.value.includes(s.uid)
}

async function run() {
  if (busy.value || selCount.value === 0) return
  busy.value = true
  try {
    summary.value = await props.generate(
      style.value,
      students.value.filter((s) => s.checked).map((s) => s.uid),
      notes.value.trim(),
    )
  } catch {
    // 失败拦截器已统一 toast；保留弹窗与勾选/说明，便于直接改词重试
  } finally {
    busy.value = false
  }
}

function copy() {
  copyText(summary.value, '小结已复制，去粘贴吧')
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="`AI 生成${scene}总结`"
    width="640"
    top="6vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="基于当前成绩实时数据生成微信群风格小结，数字与人名全部来自真实统计；只统计勾选的学生，复制即走。"
    />

    <div class="style-row">
      <span class="style-label">小结风格</span>
      <el-select v-model="style" size="small" style="width: 180px">
        <el-option v-for="s in STYLES" :key="s.id" :value="s.id" :label="s.label" />
      </el-select>
    </div>

    <!-- 点名表：勾选=计入总结；默认勾选至少做出 1 题的同学（老师由服务端剔除） -->
    <div class="rollcall-head">
      <span class="rollcall-title">点名表<small>勾选=计入总结；默认勾选至少做出1题的同学，老师已自动排除</small></span>
      <span class="rollcall-ops">
        <el-link type="primary" :underline="false" @click="restoreDefault">恢复默认</el-link>
        <span class="rollcall-count">已勾选 {{ selCount }}/{{ students.length }}</span>
      </span>
    </div>
    <div v-loading="rollLoading" class="rollcall-list">
      <!-- 行用 div：外层再包 label 会与 el-checkbox 自带的 label 嵌套成无效 HTML，点击不切换 -->
      <div v-for="s in students" :key="s.uid" class="rollcall-row">
        <el-checkbox v-model="s.checked" />
        <span class="rollcall-name">{{ s.name }}</span>
        <span class="rollcall-hint">{{ s.hint }}</span>
      </div>
      <div v-if="!rollLoading && !students.length" class="summary-empty">班内暂无可点名的学生</div>
    </div>

    <el-form label-position="top" class="summary-form">
      <el-form-item label="教师补充说明（选填）">
        <el-input
          v-model="notes"
          type="textarea"
          :rows="2"
          maxlength="500"
          show-word-limit
          placeholder="想强调的点写这里，例：今天重点讲了打擂台找最值的思路"
        />
      </el-form-item>
    </el-form>

    <div v-if="summary" class="summary-result">{{ summary }}</div>
    <div v-else class="summary-empty">
      {{ busy ? '正在生成，约需十几秒，请稍候…' : `点击「生成总结」基于勾选学生的成绩数据生成` }}
    </div>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">关闭</el-button>
      <el-button v-if="summary" type="success" :disabled="busy" @click="copy">复制全文</el-button>
      <el-button type="primary" :loading="busy" :disabled="selCount === 0" @click="run">
        {{ summary ? '重新生成' : '生成总结' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
/* 风格选择行 */
.style-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-top: var(--soj-sp-12);
}

.style-label {
  font-weight: var(--soj-fw-emphasis);
}

/* 点名表：限高滚动 + 行内勾选（全班名册，默认只勾做过题的） */
.rollcall-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-8);
  margin-top: var(--soj-sp-12);
}

.rollcall-title {
  font-weight: var(--soj-fw-emphasis);
}

.rollcall-title small {
  font-weight: normal;
  color: var(--el-text-color-secondary);
  margin-left: var(--soj-sp-8);
}

.rollcall-ops {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
}

.rollcall-count {
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-secondary);
}

.rollcall-list {
  max-height: 220px;
  overflow-y: auto;
  border: 1px solid var(--soj-line);
  border-radius: var(--soj-radius-card);
  margin-top: var(--soj-sp-8);
  min-height: 64px;
}

.rollcall-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  padding: 4px var(--soj-sp-12);
}

.rollcall-row:hover {
  background: var(--soj-surface-2);
}

.rollcall-name {
  min-width: 88px;
}

.rollcall-hint {
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-secondary);
}

.summary-form {
  margin-top: var(--soj-sp-12);
}

/* 结果区：纯文本 pre-wrap 展示（微信群粘贴保换行），限高滚动 */
.summary-result {
  white-space: pre-wrap;
  word-break: break-word;
  background: var(--soj-surface-2);
  border: 1px solid var(--soj-line);
  border-radius: var(--soj-radius-card);
  padding: var(--soj-sp-12) var(--soj-sp-16);
  max-height: 40vh;
  overflow-y: auto;
  line-height: 1.8;
}

.summary-empty {
  color: var(--el-text-color-secondary);
  text-align: center;
  padding: var(--soj-sp-20) 0;
}
</style>

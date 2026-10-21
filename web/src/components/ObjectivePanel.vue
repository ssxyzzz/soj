<script setup lang="ts">
// 客观题作答弹窗（v0.9）：单选/多选/判断为选项列表，填空为逐空输入（题干 ____ 对应第 N 空）。
// 题库与比赛/考试做题页共用；提交动作由父组件接手（与 SubmitDialog 同一交互位）。
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import type { ObjectiveQuestionView } from '@/types'

const props = defineProps<{
  modelValue: boolean
  title: string
  question: ObjectiveQuestionView
  disabled?: boolean
  disabledReason?: string
  submitting?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'submit', answer: number[], blanks: string[]): void
}>()

const answer = ref<number[]>([])
const blanks = ref<string[]>([])

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      answer.value = []
      blanks.value = new Array(props.question.blank_count || 0).fill('')
    }
  },
)

function toggle(i: number, on: boolean) {
  if (on) {
    answer.value = [...answer.value, i].sort((a, b) => a - b)
  } else {
    answer.value = answer.value.filter((x) => x !== i)
  }
}

function optionLabel(i: number) {
  return String.fromCharCode(65 + i)
}

function doSubmit() {
  if (props.disabled) return
  const kind = props.question.kind
  if (kind === 'blank') {
    if (blanks.value.some((b) => !b.trim())) {
      ElMessage.warning('请填写全部空位')
      return
    }
    emit('submit', [], blanks.value.map((b) => b))
    return
  }
  if (kind === 'multiple' && answer.value.length === 0) {
    ElMessage.warning('请至少选择一个答案')
    return
  }
  if (kind !== 'multiple' && answer.value.length !== 1) {
    ElMessage.warning('请选择一个答案')
    return
  }
  emit('submit', [...answer.value], [])
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="`提交答案 · ${title}`"
    width="720"
    top="8vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="obj-form">
      <!-- 填空：逐空输入（题干 ____ 对应第 N 空） -->
      <template v-if="question.kind === 'blank'">
        <div v-for="i in question.blank_count" :key="i" class="blank-row">
          <span class="blank-idx">第 {{ i }} 空</span>
          <el-input
            v-model="blanks[i - 1]"
            :placeholder="`填入第 ${i} 空的答案`"
            maxlength="500"
            @keyup.enter="doSubmit"
          />
        </div>
        <div class="dim blank-tip">题干中的 ____ 空位与上方输入框按顺序对应</div>
      </template>

      <!-- 选择/判断：选项列表 -->
      <template v-else>
        <el-radio-group
          v-if="question.kind !== 'multiple'"
          :model-value="answer[0]"
          class="opt-list"
          @update:model-value="(v: string | number | boolean | undefined) => { answer = v == null ? [] : [Number(v)] }"
        >
          <el-radio v-for="(opt, i) in question.options" :key="i" :value="i" class="opt-item" border>
            <span class="opt-key">{{ optionLabel(i) }}.</span> {{ opt }}
          </el-radio>
        </el-radio-group>
        <div v-else class="opt-list">
          <el-checkbox
            v-for="(opt, i) in question.options"
            :key="i"
            :model-value="answer.includes(i)"
            class="opt-item"
            border
            @change="(v: string | number | boolean) => toggle(i, !!v)"
          >
            <span class="opt-key">{{ optionLabel(i) }}.</span> {{ opt }}
          </el-checkbox>
        </div>
        <div v-if="question.kind === 'multiple'" class="dim blank-tip">多选为全对制：与正确答案完全一致才得分</div>
      </template>

      <div v-if="disabled" class="dim">{{ disabledReason }}</div>
    </div>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" plain :disabled="disabled" :loading="submitting" @click="doSubmit">提交</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.obj-form {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.opt-list {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}

.opt-item {
  height: auto;
  margin-right: 0;
  padding: 10px 14px;
  white-space: normal;
}

.opt-key {
  font-weight: var(--soj-fw-emphasis);
  margin-right: 4px;
}

.blank-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.blank-idx {
  flex: 0 0 auto;
  color: var(--el-text-color-secondary);
}

.blank-tip {
  margin-top: 6px;
}
</style>

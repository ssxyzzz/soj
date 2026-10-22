<script setup lang="ts">
// 客观题作答卡（v0.9.3 多小题 + 内联化）：整卷逐题渲染——每题题干 + 作答控件，
// 底部一次提交。题库详情与比赛/考试做题页共用；提交动作由父组件接手。
import { reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'

import MdView from '@/components/MdView.vue'
import type { ObjectiveAnswer, ObjectiveQuestionView } from '@/types'

const props = defineProps<{
  questions: ObjectiveQuestionView[]
  disabled?: boolean
  disabledReason?: string
  submitting?: boolean
}>()

const emit = defineEmits<{
  (e: 'submit', answers: ObjectiveAnswer[]): void
}>()

// 逐题作答状态：选择题 answers[i] 为索引数组，填空题 blanks[i] 为逐空文本
const picks = reactive<number[][]>([])
const fills = reactive<string[][]>([])

function reset() {
  picks.length = 0
  fills.length = 0
  for (const q of props.questions) {
    picks.push([])
    fills.push(new Array(q.blank_count || 0).fill(''))
  }
}

watch(() => props.questions, reset, { immediate: true, deep: false })

function toggle(qi: number, i: number, on: boolean) {
  const cur = picks[qi]
  picks[qi] = on ? [...cur, i].sort((a, b) => a - b) : cur.filter((x) => x !== i)
}

function setSingle(qi: number, v?: string | number | boolean) {
  picks[qi] = v == null ? [] : [Number(v)]
}

function optionLabel(i: number) {
  return String.fromCharCode(65 + i)
}

function kindLabel(kind: string) {
  const map: Record<string, string> = { single: '单选题', multiple: '多选题', judge: '判断题', blank: '填空题' }
  return map[kind] ?? '客观题'
}

function doSubmit() {
  if (props.disabled) return
  const answers: ObjectiveAnswer[] = []
  for (let i = 0; i < props.questions.length; i++) {
    const q = props.questions[i]
    if (q.kind === 'blank') {
      if (fills[i].some((b) => !b.trim())) {
        ElMessage.warning(`第 ${i + 1} 题请填写全部空位`)
        return
      }
      answers.push({ blanks: fills[i].map((b) => b) })
    } else {
      if (picks[i].length === 0) {
        ElMessage.warning(`第 ${i + 1} 题请选择答案`)
        return
      }
      if (q.kind !== 'multiple' && picks[i].length !== 1) {
        ElMessage.warning(`第 ${i + 1} 题只能选择一个答案`)
        return
      }
      answers.push({ answer: [...picks[i]] })
    }
  }
  emit('submit', answers)
}
</script>

<template>
  <el-card class="obj-answer-card">
    <template #header>
      <div class="obj-head">
        <span>作答（共 {{ questions.length }} 题）</span>
        <span class="dim obj-note">提交即判分，按题给分；满分后可查看解析</span>
      </div>
    </template>

    <div v-for="(q, qi) in questions" :key="qi" class="obj-item">
      <div class="obj-item-head">
        <span class="obj-idx">第 {{ qi + 1 }} 题</span>
        <el-tag size="small" class="ptype-tag">{{ kindLabel(q.kind) }}</el-tag>
        <span class="dim obj-score">{{ q.score }} 分</span>
      </div>
      <div v-if="q.stem" class="obj-stem"><MdView :content="q.stem" /></div>

      <!-- 填空：逐空输入 -->
      <div v-if="q.kind === 'blank'" class="blank-list">
        <div v-for="i in q.blank_count" :key="i" class="blank-row">
          <span class="blank-idx">第 {{ i }} 空</span>
          <el-input
            v-model="fills[qi][i - 1]"
            :placeholder="`填入第 ${i} 空的答案`"
            maxlength="500"
            :disabled="disabled"
          />
        </div>
      </div>

      <!-- 选择/判断 -->
      <template v-else>
        <el-radio-group
          v-if="q.kind !== 'multiple'"
          :model-value="picks[qi][0]"
          class="opt-list"
          :disabled="disabled"
          @update:model-value="(v?: string | number | boolean) => setSingle(qi, v)"
        >
          <el-radio v-for="(opt, i) in q.options" :key="i" :value="i" class="opt-item" border>
            <span class="opt-key">{{ optionLabel(i) }}.</span> {{ opt }}
          </el-radio>
        </el-radio-group>
        <div v-else class="opt-list">
          <el-checkbox
            v-for="(opt, i) in q.options"
            :key="i"
            :model-value="picks[qi].includes(i)"
            class="opt-item"
            border
            :disabled="disabled"
            @change="(v?: string | number | boolean) => toggle(qi, i, !!v)"
          >
            <span class="opt-key">{{ optionLabel(i) }}.</span> {{ opt }}
          </el-checkbox>
        </div>
      </template>
    </div>

    <div class="obj-foot">
      <span v-if="disabled" class="dim">{{ disabledReason }}</span>
      <span v-else class="dim">多选为全对制：与正确答案完全一致才得分</span>
      <el-button type="primary" :disabled="disabled" :loading="submitting" @click="doSubmit">
        提交答案
      </el-button>
    </div>
  </el-card>
</template>

<style scoped>
.obj-answer-card {
  margin-top: 16px;
}

.obj-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-weight: var(--soj-fw-emphasis);
}

.obj-note {
  font-weight: var(--soj-fw-regular);
}

.obj-item {
  padding: 14px 0;
  border-bottom: 1px dashed var(--el-border-color-lighter);
}

.obj-item:last-of-type {
  border-bottom: 0;
}

.obj-item-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.obj-idx {
  font-weight: var(--soj-fw-emphasis);
}

.obj-score {
  margin-left: auto;
}

.obj-stem {
  margin-bottom: 12px;
}

/* 题干段落首行按中文排版缩进两格（语文式） */
.obj-stem :deep(.vditor-reset > p) {
  text-indent: 2em;
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

.blank-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.blank-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.blank-idx {
  flex: 0 0 auto;
  color: var(--el-text-color-secondary);
}

.obj-foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}
</style>

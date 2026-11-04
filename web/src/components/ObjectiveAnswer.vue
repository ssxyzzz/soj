<script setup lang="ts">
// 客观题作答卡（v0.9.3 多小题 + 内联化；v0.63 渐进挂载）：整卷逐题渲染——每题题干 +
// 作答控件，底部一次提交。题库详情与比赛/考试做题页共用；提交动作由父组件接手。
// v0.63 挂载冻结扫尾批：题干 MdView（KaTeX/hljs 增强）+ 每题 4~8 个带边框选项控件
// 在 N 题整卷一次挂载是作答页冻结根源——改**渐进挂载**：题头（轻）全渲染常驻，题干与
// 控件只在滚到视口 ±1200px 时挂载，挂过不卸（作答态在组件 state 不在 DOM，卸载本就
// 无损；不卸则滚动零跳动、向上滚零重挂）。作答 UX 零变化。
import { nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
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

// ---- 渐进挂载（v0.63）：视口附近的题才挂题干+控件，挂过保留 ----
const rootEl = ref<HTMLElement>()
const mountedQ = reactive(new Set<number>())
let io: IntersectionObserver | null = null

function observeItems() {
  if (!io || !rootEl.value) return
  for (const el of rootEl.value.querySelectorAll<HTMLElement>('.obj-item')) {
    io.observe(el)
  }
}

onMounted(() => {
  io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (e.isIntersecting) mountedQ.add(Number((e.target as HTMLElement).dataset.qi))
      }
    },
    { rootMargin: '1200px 0px' }, // 上下预挂一屏多，滚到前已就绪
  )
  observeItems()
})
onBeforeUnmount(() => io?.disconnect())

function reset() {
  picks.length = 0
  fills.length = 0
  for (const q of props.questions) {
    picks.push([])
    fills.push(new Array(q.blank_count || 0).fill(''))
  }
  mountedQ.clear() // 换卷/重置：挂载窗口回到视口
  void nextTick(observeItems)
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

    <div ref="rootEl" class="obj-list">
    <div v-for="(q, qi) in questions" :key="qi" class="obj-item" :data-qi="qi">
      <div class="obj-item-head">
        <span class="obj-idx">第 {{ qi + 1 }} 题</span>
        <el-tag size="small" class="ptype-tag">{{ kindLabel(q.kind) }}</el-tag>
        <span class="dim obj-score">{{ q.score }} 分</span>
      </div>
      <!-- v0.63 渐进挂载：题干与控件滚到附近才建（挂过不卸）；未挂时只留轻占位 -->
      <template v-if="mountedQ.has(qi)">
      <div v-if="q.stem" class="obj-stem"><MdView :html="q.stem_html" /></div>

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
      </template>
      <div v-else class="obj-pending dim">滚动到此处展示本题</div>
    </div>
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
  margin-top: var(--soj-sp-16);
}

/* 未挂载题的轻占位（v0.63 渐进挂载）：给滚动一个最小高度感，不与真实高度对齐（挂载即替换） */
.obj-pending {
  padding: var(--soj-sp-10) 0;
  font-size: var(--soj-fs-13);
}

.obj-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-12);
  font-weight: var(--soj-fw-emphasis);
}

.obj-note {
  font-weight: var(--soj-fw-regular);
}

.obj-item {
  padding: var(--soj-sp-14) 0;
  border-bottom: 1px dashed var(--el-border-color-lighter);
}

.obj-item:last-of-type {
  border-bottom: 0;
}

.obj-item-head {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-bottom: var(--soj-sp-8);
}

.obj-idx {
  font-weight: var(--soj-fw-emphasis);
}

.obj-score {
  margin-left: auto;
}

.obj-stem {
  margin-bottom: var(--soj-sp-12);
}

/* 题干段落首行按中文排版缩进两格（语文式） */
.obj-stem :deep(.vditor-reset > p) {
  text-indent: 2em;
}

.opt-list {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: var(--soj-sp-10);
}

.opt-item {
  height: auto;
  margin-right: 0;
  padding: var(--soj-sp-10) var(--soj-sp-14);
  white-space: normal;
}

.opt-key {
  font-weight: var(--soj-fw-emphasis);
  margin-right: var(--soj-sp-4);
}

.blank-list {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-10);
}

.blank-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
}

.blank-idx {
  flex: 0 0 auto;
  color: var(--el-text-color-secondary);
}

.obj-foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--soj-sp-12);
  margin-top: var(--soj-sp-16);
  padding-top: var(--soj-sp-12);
  border-top: 1px solid var(--el-border-color-lighter);
}
</style>

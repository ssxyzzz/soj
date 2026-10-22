<script setup lang="ts">
// 客观题参考答案与解析卡（v0.9.3 独立成卡）：放在作答区下方。
// 内容来自 GetProblem 的 questions_full——该字段仅满分 ∨ 老师群体门槛下后端才下发，
// 所以这张卡天然只在「已解锁」时出现；未解锁时父组件不渲染（thinking 见调用处）。
import { computed } from 'vue'

import MdView from '@/components/MdView.vue'
import type { ObjectiveQuestionFull } from '@/types'

const props = defineProps<{ questions: ObjectiveQuestionFull[] }>()

function kindLabel(kind: string) {
  const map: Record<string, string> = { single: '单选题', multiple: '多选题', judge: '判断题', blank: '填空题' }
  return map[kind] ?? '客观题'
}

const rows = computed(() =>
  props.questions.map((q, i) => {
    let text = ''
    if (q.kind === 'blank') {
      text = (q.blanks ?? []).map((b, k) => `第${k + 1}空 ${b.accept.join(' / ')}`).join('；')
    } else if (q.kind === 'judge') {
      text = (q.answer ?? [])[0] === 0 ? '对' : '错'
    } else {
      text = (q.answer ?? []).map((a) => String.fromCharCode(65 + a)).join('、')
    }
    return {
      index: i + 1,
      kind: q.kind,
      kindText: kindLabel(q.kind),
      score: q.score ?? 0,
      text,
      explanation: q.explanation ?? '',
    }
  }),
)
</script>

<template>
  <el-card class="obj-solution-card">
    <template #header>
      <span>参考答案与解析</span>
    </template>

    <div v-for="r in rows" :key="r.index" class="sol-item">
      <div class="sol-head">
        <span class="sol-idx">第 {{ r.index }} 题</span>
        <el-tag size="small" class="ptype-tag">{{ r.kindText }}</el-tag>
        <span class="dim">{{ r.score }} 分</span>
        <span class="sol-answer">参考答案：{{ r.text || '—' }}</span>
      </div>
      <MdView v-if="r.explanation" :content="r.explanation" />
    </div>
  </el-card>
</template>

<style scoped>
.obj-solution-card {
  margin-top: 16px;
}

.sol-item + .sol-item {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px dashed var(--el-border-color-lighter);
}

.sol-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.sol-idx {
  font-weight: var(--soj-fw-emphasis);
}

.sol-answer {
  margin-left: auto;
  color: var(--el-text-color-regular);
}
</style>

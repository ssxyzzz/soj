<script setup lang="ts">
// 客观题小题编辑器（v0.9.3）：一道客观题 = 多道小问（单选/多选/判断/填空混合）。
// 负责小题列表的增删/移动与每题内容（题干/选项/答案/填空/解析/分值），
// 校验（含填空占位数）后由父组件序列化为 question_json 提交。
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowDown, ArrowUp, Delete, Plus } from '@element-plus/icons-vue'

import { problemApi } from '@/api'
import { downloadFile } from '@/api/download'
import MdEditor from '@/components/MdEditor.vue'
import type { ObjectiveKind, ObjectiveQuestionDraft, ObjectiveQuestionFull } from '@/types'

const props = defineProps<{ modelValue: ObjectiveQuestionDraft[]; pid?: number }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: ObjectiveQuestionDraft[]): void }>()

const yamlInput = ref<HTMLInputElement>()

const kindLabel: Record<ObjectiveKind, string> = {
  single: '单选题',
  multiple: '多选题',
  judge: '判断题',
  blank: '填空题',
}

function blankDraft(kind: ObjectiveKind = 'single'): ObjectiveQuestionDraft {
  return {
    stem: '',
    kind,
    options: kind === 'judge' ? ['对', '错'] : ['', '', '', ''],
    answer: [],
    blanks: kind === 'blank' ? [{ accept: [], case_sensitive: false }] : [],
    explanation: '',
    score: 0,
  }
}

function update(list: ObjectiveQuestionDraft[]) {
  emit('update:modelValue', list)
}

// ---- YAML 导入/导出（v0.10）----
// 导入只解析并覆盖当前表单，仍需「确认修改」落库；导出读取已保存的小题定义。
function pickYaml() {
  yamlInput.value?.click()
}

function toDraft(q: ObjectiveQuestionFull): ObjectiveQuestionDraft {
  return {
    stem: q.stem ?? '',
    kind: q.kind,
    options: q.options ?? (q.kind === 'judge' ? ['对', '错'] : []),
    answer: q.answer ?? [],
    blanks: (q.blanks ?? []).map((b) => ({
      accept: [...b.accept],
      case_sensitive: !!b.case_sensitive,
    })),
    explanation: q.explanation ?? '',
    score: q.score ?? 0,
  }
}

async function onYamlFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (file.size > 256 << 10) {
    ElMessage.error('YAML 超过 256KB 上限')
    return
  }
  try {
    const r = await problemApi.parseObjectiveYaml(await file.text())
    const list = r.questions.map(toDraft)
    update(list)
    ElMessage.success(`已导入 ${list.length} 道小题，请确认后保存`)
  } catch {
    // 解析/校验错误由 axios 拦截器统一提示
  }
}

function exportYaml() {
  if (!props.pid) return
  downloadFile(`/problems/${props.pid}/objective/questions.yaml`, `problem-${props.pid}-questions.yaml`)
}

// 统一经不可变更新回流（避免直接改 prop）
function patch(qi: number, fn: (q: ObjectiveQuestionDraft) => void) {
  const list = props.modelValue.map((q, i) => {
    if (i !== qi) return q
    const copy: ObjectiveQuestionDraft = {
      ...q,
      options: [...q.options],
      answer: [...q.answer],
      blanks: q.blanks.map((b) => ({ ...b, accept: [...b.accept] })),
    }
    fn(copy)
    return copy
  })
  update(list)
}

function setScore(qi: number, v?: number) {
  patch(qi, (q) => (q.score = v ?? 0))
}

function setField(qi: number, key: 'stem' | 'explanation', v: string) {
  patch(qi, (q) => (q[key] = v))
}

function setOption(qi: number, oi: number, v: string) {
  patch(qi, (q) => (q.options[oi] = v))
}

function setAnswer(qi: number, answer: number[]) {
  patch(qi, (q) => (q.answer = answer))
}

function add() {
  update([...props.modelValue, blankDraft()])
}

function remove(i: number) {
  if (props.modelValue.length <= 1) {
    ElMessage.warning('至少保留一道小题')
    return
  }
  const list = [...props.modelValue]
  list.splice(i, 1)
  update(list)
}

function move(i: number, delta: number) {
  const j = i + delta
  if (j < 0 || j >= props.modelValue.length) return
  const list = [...props.modelValue]
  ;[list[i], list[j]] = [list[j], list[i]]
  update(list)
}

// 改题型：重置互斥状态（选项/答案/空位）
function changeKind(i: number, kind: ObjectiveKind) {
  const list = [...props.modelValue]
  list[i] = { ...list[i], kind, answer: [] }
  if (kind === 'judge') list[i].options = ['对', '错']
  else if (kind === 'blank') {
    list[i].options = []
    list[i].blanks = [{ accept: [], case_sensitive: false }]
  } else {
    if (list[i].options.length < 2) list[i].options = ['', '', '', '']
    list[i].blanks = []
  }
  update(list)
}

function addOption(i: number) {
  const list = [...props.modelValue]
  list[i].options = [...list[i].options, '']
  update(list)
}

function removeOption(qi: number, oi: number) {
  const list = [...props.modelValue]
  list[qi].options.splice(oi, 1)
  list[qi].answer = list[qi].answer.filter((a) => a !== oi).map((a) => (a > oi ? a - 1 : a))
  update(list)
}

function toggleAnswer(qi: number, oi: number, on: boolean) {
  const list = [...props.modelValue]
  const cur = list[qi].answer
  list[qi].answer = on ? [...cur, oi].sort((a, b) => a - b) : cur.filter((a) => a !== oi)
  if (list[qi].kind === 'single' || list[qi].kind === 'judge') list[qi].answer = [oi]
  update(list)
}

function addBlank(qi: number) {
  const list = [...props.modelValue]
  list[qi].blanks = [...list[qi].blanks, { accept: [], case_sensitive: false }]
  update(list)
}

function removeBlank(qi: number, bi: number) {
  const list = [...props.modelValue]
  list[qi].blanks.splice(bi, 1)
  update(list)
}

// 空位的可接受答案用 | 分隔编辑
function acceptText(q: ObjectiveQuestionDraft, bi: number): string {
  return (q.blanks[bi]?.accept ?? []).join(' | ')
}

function setAcceptText(qi: number, bi: number, text: string) {
  const list = [...props.modelValue]
  const b = { ...list[qi].blanks[bi] }
  b.accept = text.split('|').map((x) => x.trim()).filter((x) => x !== '')
  list[qi].blanks[bi] = b
  update(list)
}

function setCaseSensitive(qi: number, bi: number, v: boolean) {
  const list = [...props.modelValue]
  list[qi].blanks[bi] = { ...list[qi].blanks[bi], case_sensitive: v }
  update(list)
}

const scoreSum = computed(() => props.modelValue.reduce((n, q) => n + (q.score || 0), 0))
const countBlanks = (md: string) => (md.match(/_{4,}/g) ?? []).length

// 校验整卷（供父组件在提交前调用）；返回 null = 通过
function validate(): string | null {
  const list = props.modelValue
  if (!list.length) return '至少需要一道小题'
  for (let i = 0; i < list.length; i++) {
    const q = list[i]
    const tag = `第 ${i + 1} 题`
    if (!q.stem.trim()) return `${tag}：请填写题干`
    if (q.kind === 'blank') {
      if (!q.blanks.length) return `${tag}：至少需要一个空位`
      if (q.blanks.some((b) => b.accept.length === 0)) return `${tag}：每个空位至少要有一个可接受答案`
      const slots = countBlanks(q.stem)
      if (slots !== q.blanks.length) {
        return `${tag}：题干里有 ${slots} 个空位（____），但定义了 ${q.blanks.length} 个空`
      }
    } else {
      const opts = q.options.map((t) => t.trim()).filter((t) => t !== '')
      if (opts.length < 2) return `${tag}：至少需要两个非空选项`
      if (q.answer.length === 0) return `${tag}：请选择正确答案`
      if (q.kind !== 'multiple' && q.answer.length !== 1) return `${tag}：只能有一个正确答案`
      const nonEmptyIdx = new Set(q.options.map((t, k) => (t.trim() !== '' ? k : -1)).filter((k) => k >= 0))
      if (q.answer.some((a) => !nonEmptyIdx.has(a))) return `${tag}：答案指向了空选项，请重新勾选`
    }
  }
  const sum = list.reduce((n, q) => n + (q.score || 0), 0)
  if (list.some((q) => (q.score || 0) <= 0)) return '每道小题的分值必须为正数（留空会自动等分，但这里请先填好）'
  if (sum !== 100) return `各小题分值合计必须为 100，当前为 ${sum}`
  return null
}

defineExpose({ validate, blankDraft })
</script>

<template>
  <div class="obj-editor">
    <div class="obj-editor-head">
      <span class="obj-editor-title">小题列表（共 {{ modelValue.length }} 题，分值合计 {{ scoreSum }}）</span>
      <span class="obj-editor-actions">
        <input
          ref="yamlInput"
          type="file"
          accept=".yaml,.yml,application/x-yaml,text/yaml"
          hidden
          @change="onYamlFile"
        />
        <el-button size="small" plain title="导入 YAML：覆盖当前小题列表，导入后仍需保存" @click="pickYaml">
          导入 YAML
        </el-button>
        <el-button
          v-if="pid"
          size="small"
          plain
          title="导出已保存的小题定义（含答案/解析）"
          @click="exportYaml"
        >
          导出 YAML
        </el-button>
        <el-button size="small" type="primary" :icon="Plus" @click="add">添加小题</el-button>
      </span>
    </div>

    <el-card v-for="(q, qi) in modelValue" :key="qi" class="obj-q" shadow="never">
      <template #header>
        <div class="obj-q-head">
          <span class="obj-q-idx">第 {{ qi + 1 }} 题</span>
          <el-radio-group
            size="small"
            :model-value="q.kind"
            @change="(v?: string | number | boolean) => changeKind(qi, v as ObjectiveKind)"
          >
            <el-radio-button v-for="(lbl, k) in kindLabel" :key="k" :value="k">{{ lbl }}</el-radio-button>
          </el-radio-group>
          <span class="obj-q-ops">
            <el-input-number
              :model-value="q.score"
              :min="1"
              :max="100"
              :controls="false"
              size="small"
              class="obj-score-input"
              @update:model-value="(v?: number) => setScore(qi, v)"
            />
            <span class="dim">分</span>
            <el-button text :icon="ArrowUp" :disabled="qi === 0" @click="move(qi, -1)" />
            <el-button text :icon="ArrowDown" :disabled="qi === modelValue.length - 1" @click="move(qi, 1)" />
            <el-button text type="danger" :icon="Delete" @click="remove(qi)" />
          </span>
        </div>
      </template>

      <el-form-item label="题干" required>
        <MdEditor
          :model-value="q.stem"
          variant="compact"
          auto-height
          :height="'120px'"
          @update:model-value="(v: string) => setField(qi, 'stem', v)"
        />
        <div v-if="q.kind === 'blank'" class="dim obj-hint">
          用连续 4 个以上下划线（____）标记空位，第 N 个空位对应下方第 N 空的答案
        </div>
      </el-form-item>

      <!-- 选择/判断：选项与正确答案 -->
      <el-form-item v-if="q.kind !== 'blank'">
        <template #label>
          <span>{{ q.kind === 'judge' ? '正确答案' : '选项与正确答案' }}</span>
        </template>
        <div class="obj-options">
          <template v-if="q.kind === 'judge'">
            <el-radio-group
              :model-value="q.answer[0]"
              @update:model-value="(v?: string | number | boolean) => setAnswer(qi, v == null ? [] : [Number(v)])"
            >
              <el-radio :value="0">对</el-radio>
              <el-radio :value="1">错</el-radio>
            </el-radio-group>
          </template>
          <template v-else>
            <div v-for="(opt, oi) in q.options" :key="oi" class="obj-opt-row">
              <el-radio
                v-if="q.kind === 'single'"
                :model-value="q.answer[0] === oi ? oi : undefined"
                :value="oi"
                @change="setAnswer(qi, [oi])"
              >
                {{ String.fromCharCode(65 + oi) }}
              </el-radio>
              <el-checkbox
                v-else
                :model-value="q.answer.includes(oi)"
                @change="(v?: string | number | boolean) => toggleAnswer(qi, oi, !!v)"
              >
                {{ String.fromCharCode(65 + oi) }}
              </el-checkbox>
              <el-input
                :model-value="q.options[oi]"
                :placeholder="`选项 ${String.fromCharCode(65 + oi)} 内容`"
                maxlength="200"
                @update:model-value="(v: string) => setOption(qi, oi, v)"
              />
              <el-button v-if="q.options.length > 2" text type="danger" @click="removeOption(qi, oi)">删除</el-button>
            </div>
            <el-button text type="primary" :disabled="q.options.length >= 8" @click="addOption(qi)">+ 添加选项</el-button>
          </template>
        </div>
      </el-form-item>

      <!-- 填空：逐空可接受答案 -->
      <el-form-item v-else>
        <template #label>
          <span>空位答案</span>
        </template>
        <div class="obj-blanks">
          <div v-for="(b, bi) in q.blanks" :key="bi" class="obj-blank-row">
            <span class="obj-blank-idx">第 {{ bi + 1 }} 空</span>
            <el-input
              :model-value="acceptText(q, bi)"
              placeholder="可接受答案，多个用 | 分隔（如：42 | 四十二）"
              maxlength="500"
              @update:model-value="(v: string) => setAcceptText(qi, bi, v)"
            />
            <el-checkbox
              :model-value="!!b.case_sensitive"
              @change="(v?: string | number | boolean) => setCaseSensitive(qi, bi, !!v)"
            >
              区分大小写
            </el-checkbox>
            <el-button text type="danger" :disabled="q.blanks.length <= 1" @click="removeBlank(qi, bi)">删除</el-button>
          </div>
          <el-button text type="primary" @click="addBlank(qi)">+ 添加空位</el-button>
        </div>
      </el-form-item>

      <el-form-item label="解析（可选）">
        <MdEditor
          :model-value="q.explanation"
          variant="compact"
          auto-height
          :height="'96px'"
          @update:model-value="(v: string) => setField(qi, 'explanation', v)"
        />
      </el-form-item>
    </el-card>
  </div>
</template>

<style scoped>
.obj-editor {
  width: 100%;
}

.obj-editor-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.obj-editor-title {
  font-weight: var(--soj-fw-emphasis);
}

.obj-editor-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.obj-q {
  margin-bottom: 14px;
  border: 1px solid var(--el-border-color-lighter);
}

.obj-q :deep(.el-card__header) {
  padding: 10px 14px;
  background: var(--el-fill-color-lighter);
}

.obj-q-head {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.obj-q-idx {
  font-weight: var(--soj-fw-emphasis);
}

.obj-q-ops {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 4px;
}

.obj-score-input {
  width: 64px;
}

.obj-options,
.obj-blanks {
  width: 100%;
}

.obj-opt-row,
.obj-blank-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  width: 100%;
}

.obj-blank-idx {
  flex: 0 0 auto;
  color: var(--el-text-color-secondary);
}

.obj-hint {
  margin-top: 6px;
}
</style>

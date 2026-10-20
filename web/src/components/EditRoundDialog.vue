<script setup lang="ts">
// 比赛/考试编辑弹窗（v0.7；v0.7.2 说明拆独立入口）：
// - 完整模式 = 名称/起止时间（不再含题目与说明）；
// - intro 模式 = 「编辑说明」专用（富文本单字段）；
// - problems 模式 = 「调整题目」专用（题库选题 + 拖动排序 + 删除）。
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { contestApi, examApi, type RoundKind } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import ProblemPicker from '@/components/ProblemPicker.vue'
import { MAX_RICHTEXT_BYTES, richtextBytes } from '@/utils/richtext'
import type { ContestDetail, PickedProblem } from '@/types'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    kind: RoundKind
    round: ContestDetail
    mode?: 'full' | 'problems' | 'intro'
  }>(),
  { mode: 'full' },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'saved'): void
}>()

const api = computed(() => (props.kind === 'exam' ? examApi : contestApi))
const name = computed(() => (props.kind === 'exam' ? '考试' : '比赛'))
const titleText = computed(() =>
  props.mode === 'problems' ? `调整${name.value}题目` : props.mode === 'intro' ? `编辑${name.value}说明` : `编辑${name.value}`,
)

const title = ref('')
const range = ref<Date[]>([])
const intro = ref('')
const editList = ref<PickedProblem[]>([])
const saving = ref(false)

function reset() {
  title.value = props.round.title
  range.value = [new Date(props.round.start), new Date(props.round.end)]
  intro.value = props.round.intro_html || props.round.intro || ''
  const source = props.round.edit_problems ?? props.round.problems ?? []
  editList.value = source.map((p) => ({ id: p.id, title: p.title, hidden: 'hidden' in p ? p.hidden : false }))
}

// 全量编辑只有「时间」会影响赛中作答/榜单；仅改名/改说明不弹确认。
function timeChanged(): boolean {
  if (range.value.length !== 2) return false
  const start = new Date(range.value[0]).getTime()
  const end = new Date(range.value[1]).getTime()
  return start !== new Date(props.round.start).getTime() || end !== new Date(props.round.end).getTime()
}

async function save() {
  if (props.mode === 'problems') {
    if (!editList.value.length) {
      ElMessage.warning(`${name.value}至少需要一道题目`)
      return
    }
  } else if (props.mode === 'intro') {
    if (!intro.value.trim()) {
      ElMessage.warning(`${name.value}说明不能为空`)
      return
    }
    if (richtextBytes(intro.value) > MAX_RICHTEXT_BYTES) {
      ElMessage.error(`${name.value}说明过长（上限 256KB）`)
      return
    }
  } else {
    if (!title.value.trim()) {
      ElMessage.warning(`请填写${name.value}名称`)
      return
    }
    if (range.value.length !== 2) {
      ElMessage.warning('请选择起止时间')
      return
    }
  }
  const needConfirm =
    props.round.status !== 'scheduled' &&
    (props.mode === 'problems' || (props.mode === 'full' && timeChanged()))
  if (needConfirm) {
    try {
      await ElMessageBox.confirm(
        props.mode === 'problems'
          ? `${name.value}已开始，修改题目会影响进行中的作答与榜单，确定保存？`
          : `${name.value}已开始，修改起止时间会影响进行中的作答与榜单，确定保存？`,
        '赛中修改确认',
        { type: 'warning', confirmButtonText: '确定保存', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
  }
  saving.value = true
  try {
    const body: {
      title?: string
      start?: string
      end?: string
      intro_html?: string
      problem_ids?: number[]
    } =
      props.mode === 'problems'
        ? { problem_ids: editList.value.map((p) => p.id) }
        : props.mode === 'intro'
          ? { intro_html: intro.value }
          : {
              title: title.value.trim(),
              start: new Date(range.value[0]).toISOString(),
              end: new Date(range.value[1]).toISOString(),
            }
    await api.value.update(props.round.id, body)
    ElMessage.success(`${name.value}已保存`)
    emit('saved')
    emit('update:modelValue', false)
  } finally {
    saving.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="titleText"
    width="960"
    top="5vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <template v-if="mode === 'full'">
      <el-form label-position="top">
        <el-form-item :label="`${name}名称`" required>
          <el-input v-model="title" maxlength="64" />
        </el-form-item>
        <el-form-item label="起止时间（最长 7 天）" required>
          <el-date-picker
            v-model="range"
            type="datetimerange"
            start-placeholder="开始"
            end-placeholder="结束"
            style="width: 100%"
          />
        </el-form-item>
      </el-form>
    </template>

    <template v-else-if="mode === 'intro'">
      <el-form label-position="top">
        <el-form-item :label="`${name}说明`" required>
          <MdEditor v-model="intro" height="150px" variant="compact" />
        </el-form-item>
      </el-form>
    </template>

    <template v-else>
      <ProblemPicker v-model="editList" />
      <div class="dim">拖动 ⠿ 调整顺序，右侧「删除」移除；保存后一次性生效。</div>
    </template>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>






</style>

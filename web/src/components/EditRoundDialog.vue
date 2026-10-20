<script setup lang="ts">
// 比赛/考试编辑弹窗（v0.7；v0.7.2 说明拆独立入口）：
// - 完整模式 = 名称/起止时间（不再含题目与说明）；
// - intro 模式 = 「编辑说明」专用（富文本单字段）；
// - problems 模式 = 「调整题目」专用（题库选题 + 拖动排序 + 删除）。
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { contestApi, examApi, problemApi, type RoundKind } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import { MAX_RICHTEXT_BYTES, richtextBytes } from '@/utils/richtext'
import type { ContestDetail } from '@/types'

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
const editList = ref<{ id: number; title: string; hidden?: boolean }[]>([])
const pickPid = ref<number | null>(null)
const problemOptions = ref<{ id: number; title: string }[]>([])
const saving = ref(false)
const dragIndex = ref<number | null>(null)

function reset() {
  title.value = props.round.title
  range.value = [new Date(props.round.start), new Date(props.round.end)]
  intro.value = props.round.intro_html || props.round.intro || ''
  const source = props.round.edit_problems ?? props.round.problems ?? []
  editList.value = source.map((p) => ({ id: p.id, title: p.title, hidden: 'hidden' in p ? p.hidden : false }))
  pickPid.value = null
}

async function loadOptions() {
  try {
    const r = await problemApi.list(1, 100)
    problemOptions.value = r.list.filter((p) => !p.hidden).map((p) => ({ id: p.id, title: p.title }))
  } catch {
    problemOptions.value = []
  }
}

function addPicked() {
  if (!pickPid.value) {
    ElMessage.warning('请先从题库选择题目')
    return
  }
  if (editList.value.some((p) => p.id === pickPid.value)) {
    ElMessage.warning('该题已在本场里')
    return
  }
  const opt = problemOptions.value.find((p) => p.id === pickPid.value)
  editList.value.push({ id: pickPid.value, title: opt?.title ?? `#${pickPid.value}` })
  pickPid.value = null
}

function removeAt(i: number) {
  editList.value.splice(i, 1)
}

function onDragStart(i: number) {
  dragIndex.value = i
}

function onDrop(i: number) {
  if (dragIndex.value == null || dragIndex.value === i) return
  const [moved] = editList.value.splice(dragIndex.value, 1)
  editList.value.splice(i, 0, moved)
  dragIndex.value = null
}

function onDragOver(e: DragEvent) {
  e.preventDefault()
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
    if (open) {
      reset()
      if (props.mode === 'problems') loadOptions()
    }
  },
  { immediate: true },
)

const listStyle = { maxHeight: '300px' }
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="titleText"
    width="760"
    top="6vh"
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
          <MdEditor v-model="intro" height="300px" variant="compact" />
        </el-form-item>
      </el-form>
    </template>

    <template v-else>
      <div class="pick-row">
        <el-select v-model="pickPid" filterable placeholder="从题库选择题目加入" style="flex: 1">
          <el-option v-for="p in problemOptions" :key="p.id" :value="p.id" :label="`#${p.id} ${p.title}`" />
        </el-select>
        <el-button type="primary" @click="addPicked">添加</el-button>
      </div>
      <div class="adjust-list" :style="listStyle">
        <div
          v-for="(p, i) in editList"
          :key="p.id"
          class="adjust-row"
          draggable="true"
          @dragstart="onDragStart(i)"
          @drop="onDrop(i)"
          @dragover="onDragOver"
        >
          <span class="drag-handle">⠿</span>
          <span class="adjust-order">T{{ i + 1 }}</span>
          <span class="adjust-title">#{{ p.id }} {{ p.title }}</span>
          <el-tag v-if="p.hidden" size="small" type="danger">隐藏</el-tag>
          <el-button size="small" type="danger" link @click="removeAt(i)">删除</el-button>
        </div>
        <el-empty v-if="!editList.length" description="还没有题目，从上方题库添加" :image-size="40" />
      </div>
      <div class="dim">拖动 ⠿ 调整顺序，右侧「删除」移除；保存后一次性生效。</div>
    </template>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.pick-row {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.adjust-list {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
  overflow: auto;
  margin-bottom: 8px;
}

.adjust-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--el-fill-color);
  cursor: grab;
  background: var(--el-bg-color);
}

.adjust-row:hover {
  background: var(--el-fill-color-light);
}

.drag-handle {
  color: var(--el-text-color-placeholder);
  cursor: grab;
}

.adjust-order {
  width: 32px;
  color: var(--el-text-color-secondary);
}

.adjust-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

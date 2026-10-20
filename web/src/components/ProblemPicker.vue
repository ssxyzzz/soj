<script setup lang="ts">
// 统一选题器（v0.7.3）：创建比赛 / 发起考试 / 布置作业 / 调整题目四处共用。
// 左列 = 候选多选（搜索框走题库 q 参数：标题关键字/题号；TagFilter 标签多选；
// 无分页、搜索驱动、单次 100 条；排除隐藏题维持现口径）；
// 右列 = 已选清单（拖动 ⠿ 排序 / 删除，沿用调整题目弹窗交互）。
// v-model 为带标题的已选数组（保序）；已选中的隐藏题（组织者 edit_problems）原样保留展示。
import { onMounted, ref, watch } from 'vue'

import { problemApi } from '@/api'
import TagFilter from '@/components/problem/TagFilter.vue'
import type { PickedProblem, ProblemListItem } from '@/types'

const props = defineProps<{ modelValue: PickedProblem[] }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: PickedProblem[]): void }>()

const q = ref('')
const tags = ref<number[]>([])
const candidates = ref<ProblemListItem[]>([])
const loading = ref(false)
const dragIndex = ref<number | null>(null)

let debounce: ReturnType<typeof setTimeout> | undefined

async function search() {
  loading.value = true
  try {
    const r = await problemApi.list(1, 100, q.value.trim() || undefined, tags.value)
    // 排除隐藏题（admin/root 视角列表会带 hidden 题，选题一律不可选）
    candidates.value = r.list.filter((p) => !p.hidden)
  } catch {
    candidates.value = []
  } finally {
    loading.value = false
  }
}

// 搜索驱动：输入/标签变化 400ms 防抖后拉候选
watch([q, tags], () => {
  if (debounce) clearTimeout(debounce)
  debounce = setTimeout(search, 400)
})

const pickedIds = () => new Set(props.modelValue.map((p) => p.id))

function isPicked(id: number): boolean {
  return pickedIds().has(id)
}

function toggle(p: ProblemListItem) {
  const next = [...props.modelValue]
  const i = next.findIndex((x) => x.id === p.id)
  if (i >= 0) next.splice(i, 1)
  else next.push({ id: p.id, title: p.title })
  emit('update:modelValue', next)
}

function removeAt(i: number) {
  const next = [...props.modelValue]
  next.splice(i, 1)
  emit('update:modelValue', next)
}

function onDragStart(i: number) {
  dragIndex.value = i
}

function onDrop(i: number) {
  if (dragIndex.value == null || dragIndex.value === i) return
  const next = [...props.modelValue]
  const [moved] = next.splice(dragIndex.value, 1)
  next.splice(i, 0, moved)
  dragIndex.value = null
  emit('update:modelValue', next)
}

function onDragOver(e: DragEvent) {
  e.preventDefault()
}

onMounted(search)
</script>

<template>
  <div class="picker">
    <div class="picker-search">
      <el-input v-model="q" placeholder="搜索标题关键字或题号" clearable />
    </div>
    <TagFilter :selected="tags" @change="(ids: number[]) => (tags = ids)" />
    <div class="picker-cols">
      <div class="picker-col">
        <div class="col-head">候选题目（点击选择）</div>
        <div v-loading="loading" class="cand-list">
          <div
            v-for="p in candidates"
            :key="p.id"
            class="cand-row"
            :class="{ picked: isPicked(p.id) }"
            @click="toggle(p)"
          >
            <el-checkbox :model-value="isPicked(p.id)" @click.stop @change="toggle(p)" />
            <span class="cand-title">#{{ p.id }} {{ p.title }}</span>
          </div>
          <el-empty v-if="!loading && !candidates.length" description="没有匹配的题目" :image-size="40" />
        </div>
      </div>
      <div class="picker-col">
        <div class="col-head">已选 {{ modelValue.length }} 题（拖动 ⠿ 排序）</div>
        <div class="picked-list">
          <div
            v-for="(p, i) in modelValue"
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
          <el-empty v-if="!modelValue.length" description="还没有题目，从左侧候选添加" :image-size="40" />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.picker-search {
  margin-bottom: 8px;
}

.picker-cols {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-top: 8px;
}

.col-head {
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-small);
  margin-bottom: 6px;
}

.cand-list,
.picked-list {
  height: 320px;
  overflow-y: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
  background: var(--el-bg-color);
}

.cand-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  cursor: pointer;
  border-bottom: 1px solid var(--el-fill-color);
}

.cand-row:hover {
  background: var(--el-fill-color-light);
}

.cand-row.picked {
  background: var(--el-color-primary-light-9);
}

.cand-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.adjust-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 12px;
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

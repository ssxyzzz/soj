<script setup lang="ts">
// 题解弹窗（v0.5e ② 拆分自 ProblemDetailView，v0.5b-1 实现）：
// 置顶优先 10 条/页；发表/编辑走编辑器弹窗；置顶/删他人需 solution.manage（后端下发 can_manage）。
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { problemApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import { fmtDate } from '@/utils/format'
import { useUserStore } from '@/stores/user'
import type { SolutionItem } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problemId: number
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const userStore = useUserStore()

const PAGE_SIZE = 10
const solutions = ref<SolutionItem[]>([])
const solutionsTotal = ref(0)
const solutionsPage = ref(1)
const canManageSolutions = ref(false)
const showSolutionEditor = ref(false)
const solutionEditing = ref<SolutionItem | null>(null)
const solutionDraft = ref('')

async function loadSolutions(p = 1) {
  const r = await problemApi.solutions(props.problemId, p, PAGE_SIZE)
  solutions.value = r.list
  solutionsTotal.value = r.total
  canManageSolutions.value = r.can_manage
  solutionsPage.value = p
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) loadSolutions(1)
  },
)

function openWriteSolution() {
  solutionEditing.value = null
  solutionDraft.value = ''
  showSolutionEditor.value = true
}

function openEditSolution(s: SolutionItem) {
  solutionEditing.value = s
  solutionDraft.value = s.content
  showSolutionEditor.value = true
}

async function saveSolution() {
  if (!solutionDraft.value.trim()) {
    ElMessage.warning('题解内容不能为空')
    return
  }
  if (solutionEditing.value) {
    await problemApi.updateSolution(props.problemId, solutionEditing.value.id, solutionDraft.value)
    ElMessage.success('题解已更新')
  } else {
    await problemApi.postSolution(props.problemId, solutionDraft.value)
    ElMessage.success('题解已发布')
  }
  showSolutionEditor.value = false
  await loadSolutions()
}

async function removeSolution(s: SolutionItem) {
  await ElMessageBox.confirm('确定删除这条题解？删除后不可恢复。', '删除题解', {
    type: 'warning',
    confirmButtonText: '删除',
    cancelButtonText: '取消',
  })
  await problemApi.deleteSolution(props.problemId, s.id)
  ElMessage.success('题解已删除')
  await loadSolutions()
}

async function togglePin(s: SolutionItem) {
  await problemApi.pinSolution(props.problemId, s.id, !s.pinned)
  await loadSolutions()
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="题解"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="dlg-toolbar">
      <span class="dim">共 {{ solutionsTotal }} 篇</span>
      <el-button type="primary" plain @click="openWriteSolution">发表题解</el-button>
    </div>
    <div v-for="s in solutions" :key="s.id" class="solution-item">
      <div class="solution-meta">
        <el-tag v-if="s.pinned" size="small" type="warning">置顶</el-tag>
        <span class="sub-user">{{ s.nickname }}</span>
        <span class="dim">
          {{ fmtDate(s.created_at) }}
          <template v-if="s.updated_at !== s.created_at"> · 已编辑</template>
        </span>
        <span class="solution-actions">
          <el-button v-if="canManageSolutions" link type="primary" @click="togglePin(s)">
            {{ s.pinned ? '取消置顶' : '置顶' }}
          </el-button>
          <el-button v-if="s.uid === userStore.uid" link type="primary" @click="openEditSolution(s)">
            编辑
          </el-button>
          <el-button
            v-if="s.uid === userStore.uid || canManageSolutions"
            link
            type="danger"
            @click="removeSolution(s)"
          >
            删除
          </el-button>
        </span>
      </div>
      <MdView :content="s.content" />
    </div>
    <el-empty v-if="!solutions.length" description="还没有题解，快来发第一篇" :image-size="48" />
    <div v-if="solutionsTotal > PAGE_SIZE" class="side-pager">
      <el-pagination
        background
        layout="prev, pager, next"
        :total="solutionsTotal"
        :page-size="PAGE_SIZE"
        :current-page="solutionsPage"
        @current-change="(p: number) => loadSolutions(p)"
      />
    </div>

    <el-dialog
      v-model="showSolutionEditor"
      :title="solutionEditing ? '编辑题解' : '发表题解'"
      width="1020"
      top="4vh"
      append-to-body
    >
      <MdEditor v-model="solutionDraft" variant="compact" height="420px" />
      <template #footer>
        <el-button @click="showSolutionEditor = false">取消</el-button>
        <el-button type="primary" plain @click="saveSolution">发布</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<style scoped>
.dlg-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.solution-item + .solution-item {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.solution-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}

.sub-user {
  font-weight: 600;
}

.solution-actions {
  margin-left: auto;
  display: flex;
  gap: 4px;
}

/* .side-pager 与 main.css 全局重复，批三删本地 */
</style>

<script setup lang="ts">
import { useRouter } from 'vue-router'

import ScoreCell from './ScoreCell.vue'
import type { ScoreBoardView, ScoreCellView, ScoreProblemView, ScoreRowView } from './types'

withDefaults(
  defineProps<{
    modelValue: boolean
    board: ScoreBoardView | null
    title?: string
    loading?: boolean
    canExport?: boolean
    exporting?: boolean
    myUid?: number | null
  }>(),
  {
    title: '成绩表',
    loading: false,
    canExport: false,
    exporting: false,
    myUid: null,
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'refresh'): void
  (e: 'export'): void
  (e: 'inspect', payload: { uid: number; name: string; pid: number; title: string }): void
}>()

const router = useRouter()

function cellOf(row: ScoreRowView, p: ScoreProblemView): ScoreCellView {
  return (
    row.cells.find((c) => c.pid === p.pid) ?? {
      pid: p.pid,
      score: 0,
      errors: 0,
      fullScore: p.fullScore,
      tries: 0,
      state: 'untried',
    }
  )
}

function goProblem(p: ScoreProblemView) {
  if (p.route) router.push(p.route)
}

function onInspect(row: ScoreRowView, p: ScoreProblemView) {
  emit('inspect', { uid: row.uid, name: row.name, pid: p.pid, title: p.title })
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="toolbar">
      <span class="dim">共 {{ board?.rows.length ?? 0 }} 人 · {{ board?.problems.length ?? 0 }} 题</span>
      <span class="actions">
        <el-button size="small" :loading="loading" @click="emit('refresh')">刷新</el-button>
        <el-button
          v-if="canExport"
          size="small"
          type="primary"
          plain
          :loading="exporting"
          @click="emit('export')"
        >
          导出 CSV
        </el-button>
      </span>
    </div>

    <div v-loading="loading">
      <el-table
        v-if="board?.rows.length"
        stripe
        :data="board.rows"
        row-key="uid"
        class="score-table"
      >
        <el-table-column label="排名" width="54" align="center" fixed="left">
          <template #default="{ row }">{{ row.rank ?? '—' }}</template>
        </el-table-column>
        <el-table-column label="昵称" min-width="110" fixed="left">
          <template #default="{ row }">
            <span class="name" :class="{ me: row.uid === myUid }" :title="row.name">{{ row.name }}</span>
          </template>
        </el-table-column>
        <el-table-column label="总分" width="64" align="center" fixed="left">
          <template #default="{ row }"><span class="total">{{ row.total }}</span></template>
        </el-table-column>
        <el-table-column
          v-for="p in board.problems"
          :key="p.pid"
          :label="p.label"
          min-width="52"
          align="center"
        >
          <template #header>
            <el-tooltip :content="p.title" placement="top" :show-after="200">
              <el-link v-if="p.route" type="primary" @click="goProblem(p)">
                {{ p.label }}
              </el-link>
              <span v-else>{{ p.label }}</span>
            </el-tooltip>
          </template>
          <template #default="{ row }">
            <ScoreCell
              :cell="cellOf(row as ScoreRowView, p)"
              :can-inspect="board.canInspect"
              @inspect="onInspect(row as ScoreRowView, p)"
            />
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else description="暂无成绩" :image-size="60" />
    </div>
  </el-dialog>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

/* T1/T2 默认不铺下划线，悬停再提示可点（密集表头降低视觉噪音） */
.score-table :deep(.el-link) {
  text-decoration: none;
}

.score-table :deep(.el-link:hover) {
  text-decoration: underline;
}

.name {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
}

.name.me {
  color: var(--el-color-primary);
  font-weight: 600;
}

.total {
  color: var(--el-color-primary);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
</style>

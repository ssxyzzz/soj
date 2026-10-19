<script setup lang="ts">
import type { ScoreBoardView } from './types'

withDefaults(
  defineProps<{
    board: ScoreBoardView | null
    title?: string
    loading?: boolean
    visible?: boolean
    emptyText?: string
    myUid?: number | null
    canRefresh?: boolean
  }>(),
  {
    title: '成绩表',
    loading: false,
    visible: true,
    emptyText: '暂无成绩',
    myUid: null,
    canRefresh: true,
  },
)

const emit = defineEmits<{ (e: 'open'): void; (e: 'refresh'): void }>()
</script>

<template>
  <el-card class="score-summary" shadow="never">
    <template #header>
      <div class="head">
        <span>{{ title }}</span>
        <span class="actions">
          <el-button
            v-if="canRefresh"
            link
            type="primary"
            size="small"
            :loading="loading"
            @click="emit('refresh')"
          >
            刷新
          </el-button>
          <el-button
            v-if="visible && board?.rows.length"
            link
            type="primary"
            size="small"
            @click="emit('open')"
          >
            查看全部
          </el-button>
        </span>
      </div>
    </template>

    <div v-if="!visible" class="empty">{{ emptyText }}</div>
    <div v-else v-loading="loading" class="list">
      <div v-if="!board?.rows.length" class="empty">暂无成绩</div>
      <div
        v-for="row in board?.rows ?? []"
        :key="row.uid"
        class="row"
        :class="{ me: row.uid === myUid }"
        @click="emit('open')"
      >
        <span class="rank">{{ row.rank ?? '—' }}</span>
        <span class="name" :title="row.name">{{ row.name }}</span>
        <span class="total">{{ row.total }}</span>
        <span class="progress">
          <el-progress
            :percentage="row.progress"
            :show-text="false"
            :stroke-width="6"
            :status="row.progress >= 100 ? 'success' : undefined"
          />
          <span class="progress-text">{{ row.solved }}/{{ row.totalProblems }}</span>
        </span>
      </div>
    </div>
  </el-card>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.actions {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.list {
  max-height: 320px;
  overflow-y: auto;
}

.empty {
  padding: 8px 0;
  color: var(--el-text-color-secondary);
}

.row {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr) 38px 84px;
  gap: 6px;
  align-items: center;
  padding: 3px 4px;
  margin: 0 -4px;
  border-radius: 4px;
  cursor: pointer;
}

.row + .row {
  margin-top: 2px;
}

.row:hover {
  background: var(--el-fill-color-light);
}

.row.me {
  background: var(--el-color-primary-light-9);
}

.rank {
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
  text-align: right;
}

.name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.total {
  color: var(--el-color-primary);
  font-weight: var(--soj-fw-emphasis);
  font-variant-numeric: tabular-nums;
  text-align: right;
}

.progress {
  display: flex;
  align-items: center;
  gap: 4px;
}

.progress :deep(.el-progress) {
  flex: 1;
  min-width: 0;
}

.progress-text {
  flex: none;
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-small);
  font-variant-numeric: tabular-nums;
}
</style>

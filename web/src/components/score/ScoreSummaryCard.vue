<script setup lang="ts">
import TierBadge from '@/components/TierBadge.vue'
import type { ScoreBoardView } from './types'

// 前三名勋章（与排名页 RankView 同一套）
const medals = ['🥇', '🥈', '🥉']

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
      <!-- 三列（v0.7.2 定稿）：排名（前三勋章）/ 昵称 / 分数；点行查看完整成绩表 -->
      <div
        v-for="row in board?.rows ?? []"
        :key="row.uid"
        class="row"
        :class="{ me: row.uid === myUid }"
        @click="emit('open')"
      >
        <span class="rank">
          <span v-if="row.rank != null && row.rank > 0 && row.rank <= 3">{{ medals[row.rank - 1] }}</span>
          <span v-else>{{ row.rank ?? '—' }}</span>
        </span>
        <span class="name" :title="row.name">{{ row.name }}</span>
        <TierBadge v-if="row.level" :tier="row.level" kind="level" size="sm" />
        <span class="total">{{ row.total }}</span>
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
  grid-template-columns: 28px minmax(0, 1fr) 44px;
  gap: 6px;
  align-items: center;
  padding: 3px 4px;
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
  text-align: center;
  font-variant-numeric: tabular-nums;
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
</style>

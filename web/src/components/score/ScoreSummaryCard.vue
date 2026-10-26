<script setup lang="ts">
import RailCard from '@/components/RailCard.vue'
import UserNameLevel from '@/components/UserNameLevel.vue'
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
  <RailCard class="score-summary">
    <template #head>
      <div class="head">
        <div>
          <div class="rail-ov-inherit">榜单</div>
          <div class="rail-t-inherit">{{ title }}</div>
        </div>
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
      <!-- v3 sum-li：排名（前三勋章）/ 昵称·等级一体 / 分数；点行查看完整成绩表 -->
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
        <!-- uid 可点进公开主页；.stop 防止冒泡触发行的「查看完整成绩表」 -->
        <UserNameLevel :name="row.name" :level="row.level" :uid="row.uid" @click.stop />
        <span class="total">{{ row.total }}</span>
      </div>
    </div>
  </RailCard>
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
  /* v3 .sum-li：排名 / 昵称(可截断) / 分数；圆角 10 hover 浅蓝 */
  grid-template-columns: 24px minmax(0, 1fr) 48px;
  gap: 10px;
  align-items: center;
  padding: 7px 10px;
  border-radius: 10px;
  cursor: pointer;
}

.row + .row {
  margin-top: 2px;
}

.row:hover {
  background: var(--soj-surface-2);
}

.row.me {
  background: var(--soj-blue-soft);
}

/* RailCard 头插槽里的 ov/title 沿用 RailCard 同款视觉 */
.rail-ov-inherit {
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  letter-spacing: 0.16em;
  color: var(--soj-teal);
  text-transform: uppercase;
}

.rail-t-inherit {
  font-size: 15px;
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
}

.rank {
  text-align: center;
  font-variant-numeric: tabular-nums;
}

.total {
  color: var(--soj-blue);
  font-weight: var(--soj-fw-bold);
  font-variant-numeric: tabular-nums;
  text-align: right;
  font-family: var(--font-mono);
}
</style>

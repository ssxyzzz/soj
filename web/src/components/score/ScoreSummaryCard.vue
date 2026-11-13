<script setup lang="ts">
import { computed } from 'vue'

import RailCard from '@/components/RailCard.vue'
import UserNameLevel from '@/components/UserNameLevel.vue'
import type { ScoreBoardView } from './types'

// 前三名勋章（与排名页 RankView 同一套）
const medals = ['🥇', '🥈', '🥉']

const props = withDefaults(
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

// 副列截断前 N（v0.63 挂载冻结扫尾批）：原 CSS max-height 只是滚动，DOM 全量挂载——
// 百人场次即百行 UserNameLevel（含常驻动画徽章）。前 10 足够「瞟一眼榜」，
// 完整榜走「查看全部」弹窗（弹窗自身已分页）。若「我」在前 10 外则追加我的行钉底。
const TOP_N = 10
const shownRows = computed(() => {
  const rows = props.board?.rows ?? []
  if (rows.length <= TOP_N) return rows
  const me = rows.find((r) => r.uid === props.myUid)
  const top = rows.slice(0, TOP_N)
  return me && !top.includes(me) ? [...top, me] : top
})
const totalRows = computed(() => props.board?.rows.length ?? 0)
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
      <!-- v3 sum-li：排名（前三勋章）/ 昵称·等级一体 / 分数；点行查看完整成绩表。
           v0.63：仅挂前 10（+榜外的我），完整榜进弹窗 -->
      <div
        v-for="row in shownRows"
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
      <div v-if="totalRows > shownRows.length" class="more-hint">共 {{ totalRows }} 人 · 点任意行查看完整成绩表</div>
    </div>
  </RailCard>
</template>
<style scoped>
.actions {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-4);
}

/* 不设高度帽（v0.96.1 用户拍板「滚动条删掉全部显示」）：挂载行本就有 TOP_N 截断
   （前 10 + 榜外的我），卡片自然长高全部可见；完整榜仍走「查看全部」弹窗 */

.empty {
  padding: var(--soj-sp-8) 0;
  color: var(--el-text-color-secondary);
}

/* 截断提示（v0.63）：榜脚灰字，点行即进完整弹窗 */
.more-hint {
  padding: var(--soj-sp-6) var(--soj-sp-10) 0;
  font-size: var(--soj-fs-12);
  color: var(--el-text-color-secondary);
}

.row {
  display: grid;
  /* v3 .sum-li：排名 / 昵称(可截断) / 分数；圆角 10 hover 浅蓝 */
  grid-template-columns: 24px minmax(0, 1fr) 48px;
  gap: var(--soj-sp-10);
  align-items: center;
  padding: var(--soj-sp-8) var(--soj-sp-10); /* 7px→sp-8 就近归并 */
  border-radius: 10px;
  cursor: pointer;
}

.row + .row {
  margin-top: var(--soj-sp-2);
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
  font-size: var(--soj-fs-body);
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

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { TableInstance } from 'element-plus'

import { rankApi } from '@/api'
import TierBadge from '@/components/TierBadge.vue'
import { useListQuery } from '@/composables/useListQuery'
import { useLevelsStore } from '@/stores/levels'
import { useUserStore } from '@/stores/user'
import type { RankItem } from '@/types'
import {
  DEFAULT_RANK_SORT,
  RANK_SORT_LABELS,
  buildRankView,
  isRankSortKey,
  rankEmptyText,
  rankPageRows,
  type RankSortKey,
} from '@/utils/rankSort'

const levels = useLevelsStore()
levels.ensure()
const userStore = useUserStore()
const router = useRouter()

/**
 * 排行榜（v0.100 四榜合一）：一张表，六列可点表头排序。
 * 打开页面拉一次整表，之后不监听、不轮询、不自动换表——排序、过滤、名次全在内存算，
 * 换列不请求。
 */
const pageSize = 100 // 每页人数（前端切片）
const all = ref<RankItem[]>([]) // 服务端整表（只读，不随排序改动）
const loading = ref(false)
const sortKey = ref<RankSortKey>(DEFAULT_RANK_SORT)
const page = ref(1)
const tableRef = ref<TableInstance>()

// 列上只留降序：点表头即按该列排名，同列再点不翻转（榜单场景无升序需求）
const DESC_ONLY: Array<'ascending' | 'descending' | null> = ['descending']

// URL 即状态（v0.39）：排序列与页码进 query，返回不丢现场
const lq = useListQuery()
function syncQuery() {
  lq.sync({
    sort: sortKey.value === DEFAULT_RANK_SORT ? undefined : sortKey.value,
    page: page.value > 1 ? page.value : undefined,
  })
}

async function load() {
  loading.value = true
  try {
    const res = await rankApi.top()
    all.value = res.items
    const maxPage = Math.max(1, Math.ceil(res.items.length / pageSize))
    if (page.value > maxPage) page.value = maxPage
  } finally {
    loading.value = false
  }
}

// 当前视图：按排序列过滤 → 排序 → 编名次
const view = computed(() => buildRankView(all.value, sortKey.value))
const total = computed(() => view.value.length)
// 表格行：本页 + 「我」不在本页时置顶的一行
const tableRows = computed(() =>
  rankPageRows(view.value, all.value, userStore.uid, page.value, pageSize),
)

const isMe = (row: RankItem) => row.user_id === userStore.uid
// 「我」未上榜的原因：排连击/坚持列时本列为 0 被滤掉；或还没有通过题目、整表里没有我
const meNote = computed(() => {
  if (!userStore.uid || loading.value || !all.value.length) return ''
  if (view.value.some(isMe)) return ''
  return all.value.some(isMe)
    ? `我未上榜——「${RANK_SORT_LABELS[sortKey.value]}」只列大于 0 的人`
    : '我暂未上榜——通过题目后进入排行榜'
})

// 前三名领奖台：跟随当前排序列，只在第一页出现
const podium = computed(() => (page.value === 1 ? view.value.slice(0, 3) : []))
// 领奖台顺序 2-1-3（冠军居中）；人数自适应——单人独占中列、两人 2 左 1 中
const podiumOrder = computed<RankItem[]>(() =>
  [podium.value[1], podium.value[0], podium.value[2]].filter((p): p is RankItem => !!p),
)
// 领奖台数值行：等级列展示解题数（等级徽章已在上方），其余列展示本列值
const podiumLabel = computed(() =>
  sortKey.value === 'level' ? RANK_SORT_LABELS.solved : RANK_SORT_LABELS[sortKey.value],
)
const podiumUnit = computed(() => (sortKey.value.startsWith('streak') ? ' 天' : ''))
function podiumValue(p: RankItem): number {
  return sortKey.value === 'level' ? p.solved : (p[sortKey.value] ?? 0)
}

// 点表头切换排序列。点已排序列的箭头仍可能把表头排序清空，此时把指示钉回降序
function onSortChange({ prop, order }: { prop: string | null; order: string | null }) {
  if (!isRankSortKey(prop)) return
  sortKey.value = prop
  page.value = 1
  syncQuery()
  if (!order) void nextTick(() => tableRef.value?.sort(prop, 'descending'))
}

function onPageChange(p: number) {
  page.value = p
  syncQuery()
}

const avColors = ['#5a6b4f', '#8a5a33', '#3358b8', '#237e6b', '#7c3aed']
function avColor(rank: number): string {
  // 第 2、3 名头像底色（冠军用渐变）；领奖台只有前三，下标取 rank 即可
  return avColors[rank % avColors.length]
}

function rowClass({ row }: { row: RankItem }): { 'me-row': boolean } {
  return { 'me-row': isMe(row) }
}

onMounted(() => {
  const sort = lq.str('sort')
  if (isRankSortKey(sort)) sortKey.value = sort
  page.value = lq.num('page')
  void load()
})
</script>

<template>
  <!-- 容器与其他页面一致（2026-10-26 用户拍板：960 窄版退役，领奖台/表格铺标准 1440） -->
  <div class="page">
    <div class="page-head">
      <div class="kicker">LEADERBOARD</div>
      <h1 class="page-title">
        排行榜<small>点表头切换排序 · 严格序：当前列 → 等级 → 经验 → 解题数 · 连击与坚持只列大于 0 的人</small>
      </h1>
    </div>

    <!-- 领奖台（当前排序列前三；人数自适应：单人居中、两人 2 左 1 中、三人 2-1-3）；整卡可点进公开主页 -->
    <div v-if="podium.length" class="podium" v-loading="loading">
      <div
        v-for="p in podiumOrder"
        :key="p.rank"
        class="card pcard"
        :class="{ p1: p.rank === 1, p2: p.rank === 2, p3: p.rank === 3, solo: podiumOrder.length === 1 }"
        title="查看主页"
        @click="router.push(`/users/${p.user_id}`)"
      >
        <div class="rank-no">{{ p.rank }}</div>
        <div class="pav" :style="p.rank === 1 ? undefined : { background: avColor(p.rank ?? 0) }">
          {{ (p.nickname || p.username).slice(0, 1) }}
        </div>
        <div class="pname">{{ p.nickname || p.username }}</div>
        <TierBadge v-if="p.level" :tier="p.level" kind="level" size="md" />
        <div class="psolved">{{ podiumLabel }} <b>{{ podiumValue(p) }}</b>{{ podiumUnit }}</div>
      </div>
    </div>

    <el-card class="tbl-card">
      <div v-if="meNote" class="my-note">{{ meNote }}</div>
      <el-table
        v-if="total"
        ref="tableRef"
        v-loading="loading"
        stripe
        :data="tableRows"
        :row-class-name="rowClass as never"
        :default-sort="{ prop: sortKey, order: 'descending' }"
        @sort-change="onSortChange"
      >
        <el-table-column label="排名" width="88">
          <template #default="{ row }">
            <span class="mono">{{ row.rank ?? '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="选手" min-width="220">
          <template #default="{ row }">
            <router-link class="player" :to="`/users/${row.user_id}`">
              <b>{{ row.nickname || row.username }}</b>
              <span class="dim">@{{ row.username }}<template v-if="row.user_id === userStore.uid"> · 我</template></span>
            </router-link>
          </template>
        </el-table-column>
        <el-table-column
          v-for="key in (['level', 'solved', 'combo_cur', 'combo_best', 'streak_days', 'streak_best'] as const)"
          :key="key"
          :prop="key"
          :label="RANK_SORT_LABELS[key]"
          :width="key === 'level' ? 150 : 110"
          sortable="custom"
          :sort-orders="DESC_ONLY"
        >
          <template #default="{ row }">
            <!-- 表内静态化（v0.63）：百行 × Lv≥3 特效链是常驻重绘源；领奖台保留特效 -->
            <TierBadge v-if="key === 'level' && row.level" :tier="row.level" kind="level" :fx="false" />
            <span v-else-if="key === 'level'" class="dim">—</span>
            <span v-else-if="row[key]" class="mono">{{ row[key] }}</span>
            <span v-else class="dim">—</span>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else-if="!loading" :description="rankEmptyText(sortKey)" />
      <!-- 超过一页才出翻页器；翻页换的是表格内容，领奖台只在第一页出现 -->
      <div v-if="total > pageSize" class="pager">
        <el-pagination
          background
          layout="prev, pager, next, total"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          @current-change="onPageChange"
        />
      </div>
    </el-card>

    <div class="rank-footer">
      <span>SOJ · 在线判题系统</span>
    </div>
  </div>
</template>

<style scoped>
/* v3 领奖台：三卡并排、冠军居中放大（渐变数字 + 渐变头像 + 淡蓝顶光） */
.podium {
  display: grid;
  grid-template-columns: 1fr 1.22fr 1fr; /* 冠军居中列加宽（配合 2-1-3 顺序） */
  gap: var(--soj-sp-16);
  margin-bottom: var(--soj-sp-20);
  align-items: end;
}

.pcard {
  padding: var(--soj-sp-20);
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--soj-sp-8);
  cursor: pointer;
  transition: transform 0.18s, box-shadow 0.18s;
}

.pcard:hover {
  transform: translateY(-3px);
  box-shadow: var(--soj-shadow-card-hover);
}

/* 单人领奖台：独占中列（冠军位） */
.pcard.solo {
  grid-column: 2;
}

.pcard.p1 {
  padding: var(--soj-sp-32) var(--soj-sp-20) var(--soj-sp-32);
  background:
    radial-gradient(300px 120px at 50% 0%, rgb(var(--soj-blue-rgb) / 0.08), transparent 70%), var(--el-bg-color);
  border-color: rgb(var(--soj-blue-rgb) / 0.22);
}

.rank-no {
  font-size: 40px;
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -2px;
  line-height: 1;
  color: var(--soj-ink-3);
}

.p1 .rank-no {
  font-size: 52px;
  background: linear-gradient(180deg, var(--soj-blue-hi), var(--soj-teal));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

/* 尺寸层级 1>2>3：冠军头像放大，二三名收敛 */
.pav {
  width: 54px;
  height: 54px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--soj-fs-20);
  font-weight: var(--soj-fw-heavy);
  margin-top: var(--soj-sp-6);
  background: var(--soj-surface-3);
  color: var(--soj-white);
  box-shadow: 0 8px 18px color-mix(in srgb, var(--soj-navy) 16%, transparent);
}

.p1 .pav {
  width: 66px;
  height: 66px;
  font-size: var(--soj-fs-22);
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-teal));
}

.p2 .pav,
.p3 .pav {
  width: 48px;
  height: 48px;
  font-size: var(--soj-fs-18);
}

.pname {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
}

.p2 .pname,
.p3 .pname {
  font-size: var(--soj-fs-14);
}

.psolved {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.psolved b {
  color: var(--soj-blue);
  font-weight: var(--soj-fw-bold);
}

/* 选手列：粗昵称 + @handle 弱化（v0.17 起整格为链接，可点进公开主页） */
.player {
  display: inline-flex;
  align-items: baseline;
  gap: var(--soj-sp-8);
  min-width: 0;
  text-decoration: none;
  max-width: 100%;
}

.player:hover b {
  color: var(--soj-blue);
}

.player b {
  color: var(--soj-navy);
  font-weight: var(--soj-fw-bold);
}

.player .dim {
  font-size: var(--soj-fs-13);
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 表头只降序：EP 表头默认上/下两个三角，榜单没有升序——藏掉上三角；下三角落到文字行中线。
   三角容器撑到表头单元格行高、顶对齐行框（容器中线 = 文字行中线，不随字体 x 高度漂移）；
   三角墨迹只在盒子上半（EP 用上边框画），减去半个墨迹高才是墨迹居中；再补半像素——
   中文字面中线比行框中线低约半像素 */
.tbl-card :deep(.caret-wrapper) {
  height: 23px; /* EP 表头单元格行高（.el-table .cell line-height） */
  vertical-align: top;
}

.tbl-card :deep(.sort-caret.ascending) {
  display: none;
}

.tbl-card :deep(.sort-caret.descending) {
  top: calc(50% - 2px);
  bottom: auto;
}

/* 「我未上榜」提示行：置顶行承担名次展示，这里只说原因 */
.my-note {
  margin-bottom: var(--soj-sp-10);
  font-size: var(--soj-fs-14);
  color: var(--el-text-color-secondary);
}

.rank-footer {
  display: flex;
  justify-content: center;
  gap: var(--soj-sp-16);
  flex-wrap: wrap;
  margin-top: var(--soj-sp-20);
  padding-top: var(--soj-sp-20);
  border-top: 1px solid var(--soj-line);
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* 表格内分页器（与其他列表页一致：右对齐） */
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--soj-sp-16);
  padding-bottom: var(--soj-sp-2);
}

@media (max-width: 900px) {
  .podium {
    grid-template-columns: 1fr;
    gap: var(--soj-sp-12);
  }
}
</style>

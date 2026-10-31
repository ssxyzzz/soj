<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { rankApi } from '@/api'
import TierBadge from '@/components/TierBadge.vue'
import { usePagedList } from '@/composables/usePagedList'
import { useListQuery } from '@/composables/useListQuery'
import { useLevelsStore } from '@/stores/levels'
import { useUserStore } from '@/stores/user'
import type { RankItem } from '@/types'

const levels = useLevelsStore()
levels.ensure()
const userStore = useUserStore()
const router = useRouter()

// me = 观察者条目（含榜外名次），随每页请求一并返回，置顶展示用
const me = ref<RankItem | null>(null)
const { list, total, page, pageSize, loading, load } = usePagedList<RankItem>(async (p) => {
  const res = await rankApi.top(p)
  me.value = res.me
  return { list: res.items, total: res.total }
}, { pageSize: 100 })

// URL 即状态（v0.39）：页码进 query，返回不丢页（仅页码，榜单无筛选）
const lq = useListQuery()

// v3-full 排名页：前三名领奖台卡（渐变大数字 + 首字头像）；仅第一页展示；
// 连击/坚持榜用 gameRows 前三（行形状对齐 rank/solved 字段供模板复用）
const podium = computed<Record<string, unknown>[]>(() => {
  if (board.value === 'combo' || board.value === 'streak') {
    return gameRows.value.slice(0, 3).map((r) => ({
      rank: r.rank, user_id: r.user_id, username: r.username,
      nickname: r.nickname, value: r.value, solved: r.value, best: r.best,
    })) as unknown as Record<string, unknown>[]
  }
  return page.value === 1 ? (list.value as unknown as Record<string, unknown>[]).slice(0, 3) : []
})
// 领奖台展示顺序：2 - 1 - 3（冠军居中）；人数自适应——单人独占中列、两人 2 左 1 中
const podiumOrder = computed(() =>
  (podium.value as Record<string, number | string>[])
    .slice(0, 3)
    .map((r) => r as never)
    .filter(Boolean) as { rank: number; user_id: number; username: string; nickname: string; solved: number; level?: number; value?: number; best?: number }[],
)
// 表格：自己的条目置顶（含榜外名次），后面跟当前页榜单——方便查看自己
const tableRows = computed(() => (me.value ? [me.value, ...list.value] : list.value))

// 页头标题/副标题随榜切换（v0.41 多榜）
const pageTitle = computed(() =>
  ({ level: '等级排名', solve: '解题排名', combo: '连击排名', streak: '坚持排名' })[board.value],
)
const pageSub = computed(() =>
  ({
    level: '按等级与经验排序 · 每页 100 名',
    solve: '按通过题数排序 · 每页 100 名',
    combo: '连续 AC 不失误，同题重交不涨 · 当前连击',
    streak: '每天至少 AC 一题，断更清零 · 当前天数',
  })[board.value],
)

// ---- 多榜制（v0.41）：等级/刷题共用 /api/rank 数据（客户端重排），连击/坚持走 game-rank ----
const board = ref<'level' | 'solve' | 'combo' | 'streak'>('level')
const gameRows = ref<{ rank: number; user_id: number; username: string; nickname: string; value: number; best: number }[]>([])
const gameMeRank = ref(0)
const gameMeValue = ref(0)
const gameLoading = ref(false)

async function loadGameBoard() {
  gameLoading.value = true
  try {
    const kind: 'combo' | 'streak' = board.value === 'streak' ? 'streak' : 'combo'
    const r = await import('@/api').then((m) => m.gameApi.gameRank(kind))
    gameRows.value = r.list
    gameMeRank.value = r.my_rank ?? 0
    gameMeValue.value = r.my_value ?? 0
  } finally {
    gameLoading.value = false
  }
}

function switchBoard(b: 'level' | 'solve' | 'combo' | 'streak') {
  board.value = b
  if (b === 'combo' || b === 'streak') void loadGameBoard()
}

const levelRows = computed(() => {
  const rows = tableRows.value
  if (board.value !== 'level') return rows
  return [...rows].sort((a, b) => (b.level ?? 0) - (a.level ?? 0) || b.solved - a.solved)
})
const displayRows = computed<Record<string, unknown>[]>(() => {
  if (board.value === 'combo' || board.value === 'streak') return gameRows.value as unknown as Record<string, unknown>[]
  return levelRows.value as unknown as Record<string, unknown>[]
})
const boardTitle = computed(() =>
  ({ level: '等级榜', solve: '刷题榜', combo: '连击榜', streak: '坚持榜' })[board.value],
)

function onPageChange(p: number) {
  page.value = p
  lq.sync({ page: p > 1 ? p : undefined })
  load()
}

const avColors = ['#5a6b4f', '#8a5a33', '#3358b8', '#237e6b', '#7c3aed']
function avColor(i: number): string {
  return avColors[i % avColors.length]
}

function rowClass({ row }: { row: RankItem }): { 'me-row': boolean } {
  return { 'me-row': row.user_id === userStore.uid }
}

onMounted(() => {
  page.value = lq.num('page')
  load()
})
</script>

<template>
  <!-- 容器与其他页面一致（2026-10-26 用户拍板：960 窄版退役，领奖台/表格铺标准 1440） -->
  <div class="page">
    <div class="page-head">
      <div class="kicker">LEADERBOARD</div>
      <h1 class="page-title">{{ pageTitle }}<small>{{ pageSub }}</small></h1>
    </div>

    <!-- 领奖台（前三；人数自适应：单人居中、两人 2 左 1 中、三人 2-1-3）；整卡可点进公开主页 -->
    <div v-if="podium.length" class="podium" v-loading="loading || gameLoading">
      <div
        v-for="p in podiumOrder"
        :key="p.rank"
        class="card pcard"
        :class="{ p1: p.rank === 1, solo: podiumOrder.length === 1 }"
        title="查看主页"
        @click="router.push(`/users/${p.user_id}`)"
      >
        <div class="rank-no">{{ p.rank }}</div>
        <div class="pav" :style="p.rank === 1 ? undefined : { background: avColor(p.rank) }">
          {{ (p.nickname || p.username).slice(0, 1) }}
        </div>
        <div class="pname">{{ p.nickname || p.username }}</div>
        <TierBadge v-if="p.level" :tier="p.level" kind="level" size="md" />
        <div v-if="board === 'combo'" class="psolved">连击 <b>{{ p.value ?? p.solved }}</b></div>
        <div v-else-if="board === 'streak'" class="psolved">坚持 <b>{{ p.value ?? p.solved }}</b> 天</div>
        <div v-else class="psolved">解题 <b>{{ p.solved }}</b></div>
      </div>
    </div>

    <el-card class="tbl-card">
      <div class="board-tabs">
        <button
          v-for="b in (['level', 'solve', 'combo', 'streak'] as const)"
          :key="b"
          class="board-tab"
          :class="{ on: board === b }"
          @click="switchBoard(b)"
        >
          {{ { level: '🏆 等级榜', solve: '⚔️ 刷题榜', combo: '🔥 连击榜', streak: '📅 坚持榜' }[b] }}
        </button>
      </div>
      <div v-if="(board === 'combo' || board === 'streak') && gameMeRank" class="my-game-rank">
        我的{{ boardTitle }}名次：<b>#{{ gameMeRank }}</b>（当前 {{ gameMeValue }}）
      </div>
      <el-table
        v-loading="gameLoading"
        stripe
        :data="displayRows"
        :row-class-name="rowClass as never"
      >
        <el-table-column label="排名" width="88">
          <template #default="{ row }">
            <span class="mono">{{ row.rank }}</span>
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
        <template v-if="board === 'combo' || board === 'streak'">
          <el-table-column :label="board === 'combo' ? '当前连击' : '连续天数'" width="130">
            <template #default="{ row }">
              <span class="mono game-val">{{ row.value }}</span>
            </template>
          </el-table-column>
          <el-table-column label="历史最佳" width="110">
            <template #default="{ row }">
              <span class="mono dim">{{ row.best }}</span>
            </template>
          </el-table-column>
        </template>
        <template v-else>
          <el-table-column label="等级" width="150">
            <template #default="{ row }">
              <TierBadge v-if="row.level" :tier="row.level" kind="level" />
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="解题数" width="100">
            <template #default="{ row }">
              <span class="mono">{{ row.solved }}</span>
            </template>
          </el-table-column>
        </template>
      </el-table>
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
      <el-empty
        v-if="!loading && (board === 'combo' || board === 'streak' ? !displayRows.length : !tableRows.length)"
        :description="board === 'combo' ? '还没有连击记录——连续 AC 不失误就能上榜' : board === 'streak' ? '还没有坚持记录——每天至少 AC 一题开始积累' : '还没有人通过题目，快去抢占第一！'"
      />
    </el-card>

    <div class="rank-footer">
      <span>SOJ · 在线判题系统</span>
      <span>等级实时聚合，调整难度全员等级自动跟随</span>
    </div>
  </div>
</template>

<style scoped>
/* v3 领奖台：三卡并排、冠军居中放大（渐变数字 + 渐变头像 + 淡蓝顶光） */
.podium {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-bottom: 22px;
  align-items: end;
}

.pcard {
  padding: 22px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  transition: transform 0.18s, box-shadow 0.18s;
}

.pcard:hover {
  transform: translateY(-3px);
  box-shadow:
    0 2px 4px rgba(11, 31, 69, 0.04),
    0 18px 44px rgba(11, 31, 69, 0.11);
}

/* 单人领奖台：独占中列（冠军位） */
.pcard.solo {
  grid-column: 2;
}

.pcard.p1 {
  padding: 30px 22px 26px;
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

.pav {
  width: 54px;
  height: 54px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  font-weight: var(--soj-fw-heavy);
  margin-top: 6px;
  background: var(--soj-surface-3);
  color: #fff;
  box-shadow: 0 8px 18px rgba(11, 31, 69, 0.16);
}

.p1 .pav {
  width: 66px;
  height: 66px;
  font-size: 24px;
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-teal));
}

.pname {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
}

.psolved {
  font-size: 12.5px;
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
  gap: 8px;
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

.rank-footer {
  display: flex;
  justify-content: center;
  gap: 18px;
  flex-wrap: wrap;
  margin-top: 22px;
  padding-top: 22px;
  border-top: 1px solid var(--soj-line);
  font-size: 12.5px;
  color: var(--soj-ink-3);
}

/* 表格内分页器（与其他列表页一致：右对齐） */
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
  padding-bottom: 2px;
}

@media (max-width: 900px) {
  .podium {
    grid-template-columns: 1fr;
    gap: 12px;
  }
}
</style>

<style scoped>
/* 多榜制（v0.41）：四榜切换胶囊 */
.board-tabs {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}

.board-tab {
  border: 1px solid var(--el-border-color);
  background: transparent;
  border-radius: 999px;
  padding: 6px 16px;
  font-size: 14px;
  cursor: pointer;
  color: var(--el-text-color-regular);
}

.board-tab.on {
  background: var(--soj-blue);
  border-color: var(--soj-blue);
  color: #fff;
  font-weight: 700;
}

.my-game-rank {
  margin-bottom: 10px;
  font-size: 14px;
  color: var(--el-text-color-secondary);
}

.game-val {
  font-weight: 800;
  color: var(--soj-blue);
  font-size: 16px;
}
</style>

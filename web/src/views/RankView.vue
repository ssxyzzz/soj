<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { rankApi } from '@/api'
import TierBadge from '@/components/TierBadge.vue'
import { useLevelsStore } from '@/stores/levels'
import { useUserStore } from '@/stores/user'
import type { RankItem } from '@/types'

const levels = useLevelsStore()
levels.ensure()
const userStore = useUserStore()
const router = useRouter()

const list = ref<RankItem[]>([])
const me = ref<RankItem | null>(null)
const loading = ref(false)

// v3-full 排名页：前三名领奖台卡（渐变大数字 + 首字头像）
const podium = computed(() => list.value.slice(0, 3))
// 领奖台展示顺序：2 - 1 - 3（冠军居中）；人数自适应——单人独占中列、两人 2 左 1 中
const podiumOrder = computed(() => [podium.value[1], podium.value[0], podium.value[2]].filter(Boolean))
// 表格：自己的条目置顶（含榜外名次），后面跟完整榜单 1..50——方便查看自己
const tableRows = computed(() => (me.value ? [me.value, ...list.value] : list.value))

const avColors = ['#5a6b4f', '#8a5a33', '#3358b8', '#237e6b', '#7c3aed']
function avColor(i: number): string {
  return avColors[i % avColors.length]
}

function rowClass({ row }: { row: RankItem }): { 'me-row': boolean } {
  return { 'me-row': row.user_id === userStore.uid }
}

onMounted(async () => {
  loading.value = true
  try {
    const res = await rankApi.top()
    list.value = res.items
    me.value = res.me
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <!-- 容器与其他页面一致（2026-10-26 用户拍板：960 窄版退役，领奖台/表格铺标准 1440） -->
  <div class="page">
    <div class="page-head">
      <div class="kicker">LEADERBOARD</div>
      <h1 class="page-title">解题排名<small>按通过题数排序 · 前 50 名</small></h1>
    </div>

    <!-- 领奖台（前三；人数自适应：单人居中、两人 2 左 1 中、三人 2-1-3）；整卡可点进公开主页 -->
    <div v-if="podium.length" class="podium" v-loading="loading">
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
        <div class="psolved">解题 <b>{{ p.solved }}</b></div>
      </div>
    </div>

    <el-card v-if="tableRows.length" class="tbl-card">
      <el-table
        stripe
        :data="tableRows"
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
      </el-table>
      <el-empty v-if="!loading && !tableRows.length" description="还没有人通过题目，快去抢占第一！" />
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
    radial-gradient(300px 120px at 50% 0%, rgba(36, 85, 218, 0.08), transparent 70%), var(--el-bg-color);
  border-color: rgba(36, 85, 218, 0.22);
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

@media (max-width: 900px) {
  .podium {
    grid-template-columns: 1fr;
    gap: 12px;
  }
}
</style>

<script setup lang="ts">
// 用户公开主页（v0.17，plan §2.9）：/users/:id 登录可见，本人与他人统一视图。
// 头部资料卡（昵称/等级徽章+距下一级进度/注册/学校年级/全站排名/统计）+
// 主列按难度完成（D1–D10 计数卡 + 时间窗 seg + 分组折叠列表，首 AC 全量下发、
// 窗口本地切）+ 副列近期提交（published 投影，评测详情弹窗复用）与比赛历史
//（名次仅揭榜后显示，赛时/隐藏期不漏）。隐藏题已按观察者权限在后端剔除。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import RailCard from '@/components/RailCard.vue'
import SubStatus from '@/components/SubStatus.vue'
import SubmissionDetailDialog from '@/components/SubmissionDetailDialog.vue'
import TierBadge from '@/components/TierBadge.vue'
import PageShell from '@/components/PageShell.vue'
import { userApi } from '@/api'
import { useLevelsStore } from '@/stores/levels'
import type { UserPage, UserPageFirstAC } from '@/types'
import { fmtDate, fmtRel } from '@/utils/format'

const route = useRoute()
const router = useRouter()
const levels = useLevelsStore()
levels.ensure()

const page = ref<UserPage | null>(null)
const loading = ref(true)
const notFound = ref(false)

async function load() {
  loading.value = true
  notFound.value = false
  page.value = null
  try {
    page.value = await userApi.profile(Number(route.params.id))
  } catch (e) {
    if ((e as { response?: { status?: number } })?.response?.status === 404) notFound.value = true
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(() => route.params.id, load)

const avatarChar = computed(() => (page.value?.nickname || page.value?.username || '?').slice(0, 1).toUpperCase())

// ---- 等级进度（距下一级）：xp 相对当前级阈值；Lv.10 满格 ----
const lvPct = computed(() => {
  const lv = page.value?.level
  if (!lv) return 0
  const thr = levels.cfg?.thresholds
  if (!thr || lv.next_xp == null) return 100
  const cur = lv.level >= 2 ? (thr[lv.level - 2] ?? 0) : 0
  const span = lv.next_xp - cur
  return span > 0 ? Math.min(100, Math.max(0, Math.round(((lv.xp - cur) / span) * 100))) : 100
})

const acRate = computed(() => {
  const r = page.value?.stats.ac_rate ?? 0
  return `${Math.round(r * 1000) / 10}%`
})

// ---- 时间窗（滚动窗口，首 AC 时间驱动计数卡与分组列表） ----
const WINDOWS = [
  { key: 'all', label: '全部', ms: 0 },
  { key: 'y', label: '近1年', ms: 365 * 864e5 },
  { key: 'm', label: '近1月', ms: 30 * 864e5 },
  { key: 'w', label: '近1周', ms: 7 * 864e5 },
  { key: 'd', label: '近1天', ms: 864e5 },
] as const
type WinKey = (typeof WINDOWS)[number]['key']
const win = ref<WinKey>('all')
const winMs = computed(() => WINDOWS.find((w) => w.key === win.value)?.ms ?? 0)

const filtered = computed(() =>
  (page.value?.first_ac ?? []).filter(
    (e) => !winMs.value || Date.now() - Date.parse(e.first_ac_at) <= winMs.value,
  ),
)
const counts = computed(() => {
  const c = Array<number>(10).fill(0)
  for (const e of filtered.value) {
    if (e.difficulty >= 1 && e.difficulty <= 10) c[e.difficulty - 1]++
  }
  return c
})
// 分组（非空难度档）；柱状图与下方题目列表共用
const groups = computed(() => {
  const gs: { d: number; items: UserPageFirstAC[] }[] = []
  for (let d = 1; d <= 10; d++) {
    const items = filtered.value.filter((e) => e.difficulty === d)
    if (items.length) gs.push({ d, items })
  }
  return gs
})

// 柱状图选中档（2026-10-26 难度完成图改版）：点柱子/计数卡切换下方题目列表；
// 切时间窗或换用户后选中档无数据时自动落回首个非空档
const sel = ref(0)
const effSel = computed(() => (groups.value.some((g) => g.d === sel.value) ? sel.value : groups.value[0]?.d ?? 0))
const selItems = computed(() => groups.value.find((g) => g.d === effSel.value)?.items ?? [])

// 柱高按真实做题数线性等比（2026-10-27 用户拍板，弃最高档归一口径）：
// 每题 1px、初始4px、240px 封顶（236 题满高；到顶后靠柱顶数字区分）；零档 4px 灰桩。
function barH(d: number): string {
  const c = counts.value[d - 1]
  if (!c) return '4px'
  return `${Math.min(c+4, 240)}px`
}

// ---- 比赛历史行状态 ----
function contestTag(ct: NonNullable<UserPage['contests']>[number]) {
  if (ct.status === 'running') return { label: '进行中', cls: 'running' }
  if (ct.status === 'scheduled') return { label: '未开始', cls: 'scheduled' }
  if (ct.result === 'published') return { label: '已公布', cls: 'ended' }
  return { label: '成绩未公布', cls: 'scheduled' }
}

// ---- 近期提交详情弹窗 ----
const showDetail = ref(false)
const detailId = ref<number | null>(null)
function openDetail(id: number) {
  detailId.value = id
  showDetail.value = true
}
</script>

<template>
  <div v-loading="loading">
    <template v-if="page">
      <PageShell>
        <!-- 页头放主列内（与各页一致——放 PageShell 外会脱离 1440 容器贴到窗口左缘） -->
        <template #standalone>
          <div class="page-head">
            <div class="kicker">PROFILE</div>
            <h1 class="page-title">用户主页<small>公开资料与做题足迹</small></h1>
          </div>

          <!-- 头部资料卡：昵称 + 等级 + 进度条 + 资料 + 统计 -->
          <div class="card hero">
            <img v-if="page.avatar_url" :src="page.avatar_url" alt="" class="avatar avatar-img" />
            <div v-else class="avatar">{{ avatarChar }}</div>
            <div class="who">
              <div class="name-row">
                <h2>{{ page.nickname || page.username }}</h2>
                <TierBadge v-if="page.level" :tier="page.level.level" kind="level" size="md" />
              </div>
              <div class="meta">
                <span class="mono">@{{ page.username }}</span>
                <span>注册于 {{ fmtDate(page.created_at) }}</span>
                <span v-if="page.gender">{{ page.gender }}</span>
                <span v-if="page.region">{{ page.region }}</span>
                <span v-if="page.school">{{ page.school }}</span>
                <span v-if="page.grade">{{ page.grade }}</span>
                <span v-if="page.qq">QQ {{ page.qq }}</span>
              </div>
              <div v-if="page.level" class="lv-progress">
                <div class="lv-bar">
                  <div class="lv-in" :style="{ width: `${lvPct}%` }" />
                </div>
                <span class="lv-txt">
                  <template v-if="page.level.next_xp != null">
                    Lv.{{ page.level.level }} · {{ page.level.xp }}/{{ page.level.next_xp }} XP（{{ lvPct }}%）
                  </template>
                  <template v-else>Lv.{{ page.level.level }} · 满级 {{ page.level.xp }} XP</template>
                </span>
              </div>
            </div>
            <div class="hero-stats">
              <div class="stat">
                <b class="mono">{{ page.stats.submissions }}</b>
                <span>提交</span>
              </div>
              <div class="stat">
                <b class="mono">{{ page.stats.accepted }}</b>
                <span>AC</span>
              </div>
              <div class="stat" :title="page.stats.answers_accepted ? '讨论区被采纳的解答数' : ''">
                <b v-if="page.stats.answers_accepted" class="mono">{{ page.stats.answers_accepted }}</b>
                <b v-else>—</b>
                <span>解答</span>
              </div>
              <div class="stat">
                <b class="mono">{{ acRate }}</b>
                <span>AC 率</span>
              </div>
              <div class="stat">
                <b v-if="page.rank?.solved" class="mono">{{ page.rank.rank }}</b>
                <b v-else>—</b>
                <span>全站排名</span>
              </div>
            </div>
          </div>
        </template>

        <!-- 主列：难度完成图（核心区） -->
        <div class="card done-card">
          <div class="done-head">
            <h3>难度完成图</h3>
            <div class="soj-seg seg">
              <button
                v-for="w in WINDOWS"
                :key="w.key"
                :class="{ on: win === w.key }"
                @click="win = w.key"
              >
                {{ w.label }}
              </button>
            </div>
          </div>

          <!-- 柱状图（完成数按档位色轴；柱下难度勋章；点柱子切换下方题目列表） -->
          <div class="dchart">
            <button
              v-for="d in 10"
              :key="d"
              class="dbar"
              :class="[`diff-${d}`, { sel: effSel === d, zero: !counts[d - 1] }]"
              :title="counts[d - 1] ? `D${d} · ${counts[d - 1]} 题` : `D${d} 无记录`"
              :disabled="!counts[d - 1]"
              @click="sel = d"
            >
              <span class="v mono">{{ counts[d - 1] || '' }}</span>
              <i class="bar" :style="{ height: barH(d) }" />
              <TierBadge :tier="d" kind="difficulty" size="sm" />
            </button>
          </div>

          <el-empty v-if="!effSel" description="该时间窗内还没有 AC 记录" :image-size="60" />
          <div v-else class="dlist">
            <div class="dlist-head">
              <TierBadge :tier="effSel" kind="difficulty" size="sm" />
              <span class="dcount mono">{{ selItems.length }} 题</span>
            </div>
            <div class="glist">
              <div v-for="e in selItems" :key="e.pid" class="prow">
                <router-link class="plink" :to="`/problems/${e.pid}`">
                  <span class="mono pid">P{{ e.pid }}</span> {{ e.title }}
                </router-link>
                <el-tag v-if="e.hidden" size="small" type="danger" effect="plain" class="hid">隐藏</el-tag>
                <span class="when mono">{{ fmtDate(e.first_ac_at) }}</span>
              </div>
            </div>
          </div>
        </div>

        <template #aside>
          <!-- 近期提交：published 投影，点击开评测详情弹窗（权限同详情接口） -->
          <RailCard ov="记录" title="近期提交" class="aside-card">
            <template v-if="page.recent?.length">
              <div
                v-for="s in page.recent"
                :key="s.id"
                class="soj-mini-li"
                @click="openDetail(s.id)"
              >
                <SubStatus :status="s.status" :score="s.score" />
                <span class="grow rtitle">{{ s.title }}</span>
                <span class="t">{{ fmtRel(s.created_at) }}</span>
              </div>
              <p class="hint">仅展示公开态提交，点击查看评测详情。</p>
            </template>
            <p v-else class="hint">还没有公开的提交记录。</p>
          </RailCard>

          <!-- 比赛历史：名次仅揭榜后显示；进行中/成绩未公布只出状态 -->
          <RailCard ov="赛程" title="比赛历史" class="aside-card">
            <template v-if="page.contests?.length">
              <div
                v-for="ct in page.contests"
                :key="ct.id"
                class="soj-mini-li crow"
                @click="router.push(`/contests/${ct.id}`)"
              >
                <div class="grow">
                  <div class="ctitle">{{ ct.title }}</div>
                  <div class="cmeta">
                    <span class="mini-tag" :class="contestTag(ct).cls">{{ contestTag(ct).label }}</span>
                    <span class="mono">{{ fmtDate(ct.start) }}</span>
                  </div>
                </div>
                <div v-if="ct.result === 'published' && ct.rank" class="rank-cell">
                  <b class="mono">#{{ ct.rank }}</b>
                  <span class="mono">{{ ct.score }} 分 · {{ ct.total_users }} 人</span>
                </div>
              </div>
            </template>
            <p v-else class="hint">还没有参加过比赛。</p>
          </RailCard>
        </template>
      </PageShell>
    </template>

    <el-empty v-else-if="notFound" description="用户不存在或已注销" :image-size="90">
      <el-button type="primary" @click="router.push('/rank')">去看看排名</el-button>
    </el-empty>
  </div>

  <SubmissionDetailDialog v-model="showDetail" :submission-id="detailId" />
</template>

<style scoped>
/* 头部资料卡：头像首字 + 昵称行 + 资料 meta + 等级进度 + 四格统计 */
.hero {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  padding: 22px 24px;
}

.avatar {
  flex: none;
  width: 64px;
  height: 64px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 28px;
  font-weight: 800;
  color: #fff;
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-blue-lo));
}

/* QQ 头像（v0.23 绑定开关开启时）：图片版首字头像同位同径 */
.avatar-img {
  object-fit: cover;
  border: 1px solid var(--soj-line);
  background: #fff;
}

.who {
  flex: 1 1 auto;
  min-width: 0;
}

.name-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.name-row h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 800;
  color: var(--soj-navy, #0b1f45);
}

.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
  margin-top: 6px;
  font-size: 13px;
  color: var(--soj-ink-3, #5b6b85);
}

.lv-progress {
  margin-top: 12px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.lv-bar {
  flex: 0 1 320px;
  height: 8px;
  border-radius: 999px;
  background: #e8eef8;
  overflow: hidden;
}

.lv-in {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--soj-blue-hi), var(--soj-blue-lo));
  transition: width 0.4s;
}

.lv-txt {
  font-size: 12px;
  color: var(--soj-ink-3, #5b6b85);
  white-space: nowrap;
}

.hero-stats {
  flex: none;
  display: grid;
  grid-template-columns: repeat(4, minmax(72px, auto));
  gap: 8px 22px;
  text-align: center;
  align-self: center;
}

.stat b {
  display: block;
  font-size: 20px;
  color: var(--soj-navy, #0b1f45);
}

.stat span {
  font-size: 12px;
  color: var(--soj-ink-3, #5b6b85);
}

/* 按难度完成卡 */
.done-card {
  padding: 20px 24px;
}

.done-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.done-head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 800;
  color: var(--soj-navy, #0b1f45);
}

.seg {
  align-self: center;
}

/* 柱状图（难度完成图，2026-10-26）：柱色走 .diff-N 的 --dc 色轴，点柱切换下方题目列表 */
.dchart {
  margin-top: 16px;
  display: flex;
  align-items: stretch;
  gap: 10px;
}

.dbar {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  border: 0;
  background: transparent;
  padding: 6px 2px;
  font: inherit;
  color: var(--dc, var(--soj-blue));
  cursor: pointer;
}

.dbar .v {
  min-height: 18px;
  font-size: 13px;
  font-weight: 600;
  color: var(--soj-navy, #0b1f45);
}

.dbar .bar {
  width: 36px;
  border-radius: 8px 8px 3px 3px;
  background: var(--dc, var(--soj-blue));
  opacity: 0.5;
  transition: opacity 0.15s;
}

.dbar:not(.zero):hover .bar {
  opacity: 0.8;
}

.dbar.sel .bar {
  opacity: 1;
  outline: 2px solid var(--soj-blue-line);
  outline-offset: 2px;
}

.dbar.zero {
  cursor: default;
}

.dbar.zero .bar {
  background: #e3eaf6;
  opacity: 1;
}

.dbar.zero .v {
  visibility: hidden;
}

/* 选中档题目列表 */
.dlist {
  margin-top: 14px;
}

.dlist-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 2px 10px;
}

.dcount {
  color: var(--soj-ink-3, #5b6b85);
  font-size: 12px;
}

.glist {
  padding: 2px 6px;
}

.prow {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px;
  border-bottom: 1px dashed #edf2fa;
  font-size: 14px;
}

.prow:last-child {
  border-bottom: none;
}

.plink {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--soj-navy, #22334d);
  text-decoration: none;
}

.plink:hover {
  color: var(--soj-blue);
}

.pid {
  color: var(--soj-ink-3, #5b6b85);
  font-size: 12px;
  margin-right: 2px;
}

.hid {
  flex: none;
}

.when {
  flex: none;
  font-size: 12px;
  color: var(--soj-ink-3, #5b6b85);
}

/* 副列 */
.aside-card {
  margin-bottom: 16px;
}

.rtitle {
  font-size: 13px;
}

.hint {
  margin: 10px 0 0;
  font-size: 12px;
  color: var(--soj-ink-3, #5b6b85);
}

/* 比赛历史行 */
.crow {
  display: flex;
  gap: 10px;
  align-items: center;
}

.ctitle {
  font-size: 14px;
  font-weight: 600;
  color: var(--soj-navy, #22334d);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cmeta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
  font-size: 12px;
  color: var(--soj-ink-3, #5b6b85);
}

/* 迷你状态胶囊（比赛时段/结果） */
.mini-tag {
  display: inline-flex;
  align-items: center;
  font-size: 11px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 999px;
  white-space: nowrap;
}

.mini-tag.running {
  color: #157347;
  background: rgba(76, 175, 80, 0.12);
}

.mini-tag.scheduled {
  color: #8a6d1a;
  background: rgba(233, 201, 99, 0.16);
}

.mini-tag.ended {
  color: var(--soj-blue-lo);
  background: rgb(var(--soj-blue-hi-rgb) / 0.1);
}

.rank-cell {
  flex: none;
  text-align: right;
}

.rank-cell b {
  display: block;
  font-size: 16px;
  color: var(--soj-blue);
}

.rank-cell span {
  font-size: 11px;
  color: var(--soj-ink-3, #5b6b85);
}

/* 窄屏：计数卡 5×2 折行、统计下沉整行 */
@media (max-width: 992px) {
  .hero {
    flex-wrap: wrap;
  }

  .hero-stats {
    width: 100%;
    justify-content: space-between;
  }
}
</style>

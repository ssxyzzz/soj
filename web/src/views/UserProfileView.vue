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
import { gameApi, userApi } from '@/api'
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
// 游戏感批（v0.41）：本人主页加载成就墙 / 知识星图 / 错题本（公开主页不显示）
const isOwnProfile = computed(() => Number(route.params.id) === (Number(localStorage.getItem('soj_uid')) || -1))
const achs = ref<{ code: string; name: string; desc: string; tier: string; icon: string; unlocked_at?: string }[]>([])
const achUnlocked = ref(0)
const stars = ref<{ tag_id: number; name: string; solved: number; lit: boolean }[]>([])
const wrongbook = ref<{ pid: number; title: string; difficulty: number; best_score: number }[]>([])

async function loadGame() {
  if (!isOwnProfile.value) return
  try {
    const [a, sm, wb] = await Promise.all([
      gameApi.achievements(),
      gameApi.starmap(),
      gameApi.wrongbook(1),
    ])
    achs.value = a.list
    achUnlocked.value = a.unlocked
    stars.value = sm.list
    wrongbook.value = wb.list
  } catch {
    /* 静默：游戏区块不阻塞主页 */
  }
}

onMounted(() => {
  load()
  void loadGame()
})
watch(() => route.params.id, load)

const avatarChar = computed(() => (page.value?.nickname || page.value?.username || '?').slice(0, 1).toUpperCase())
// QQ 头像外域失败兜底（v0.37）：qlogo 被墙时裂图——@error 回退首字头像（换人访问随 load 重置）
const avatarBroken = ref(false)
watch(() => route.params.id, () => {
  avatarBroken.value = false
})
function onAvatarError() {
  avatarBroken.value = true
}

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
// 组内截断（v0.63 挂载冻结扫尾批）：重度用户单难度档 AC 数百题整组渲染无虚拟化——
// 前 GROUP_N 条 + 展开按钮；切档/换窗收起复位
const GROUP_N = 50
const groupExpanded = ref(false)
const shownItems = computed(() =>
  groupExpanded.value ? selItems.value : selItems.value.slice(0, GROUP_N),
)
watch([sel, win, () => page.value?.id], () => {
  groupExpanded.value = false
})

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
            <img v-if="page.avatar_url && !avatarBroken" :src="page.avatar_url" alt="" class="avatar avatar-img" loading="lazy" decoding="async" @error="onAvatarError" />
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
                <b v-else class="mono">—</b>
                <span>解答</span>
              </div>
              <div class="stat">
                <b class="mono">{{ acRate }}</b>
                <span>AC 率</span>
              </div>
              <div class="stat">
                <b v-if="page.rank?.solved" class="mono">{{ page.rank.rank }}</b>
                <b v-else class="mono">—</b>
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
              <div v-for="e in shownItems" :key="e.pid" class="prow">
                <router-link class="plink" :to="`/problems/${e.pid}`">
                  <span class="mono pid">P{{ e.pid }}</span> {{ e.title }}
                </router-link>
                <el-tag v-if="e.hidden" size="small" type="danger" effect="plain" class="hid">隐藏</el-tag>
                <span class="when mono">{{ fmtDate(e.first_ac_at) }}</span>
              </div>
              <!-- v0.63 截断展开钮：重度用户单档数百题不整组挂载 -->
              <button v-if="selItems.length > GROUP_N" type="button" class="gmore" @click="groupExpanded = !groupExpanded">
                {{ groupExpanded ? '收起' : `展开全部 ${selItems.length} 题` }}
              </button>
            </div>
          </div>
        </div>

        <!-- 成就墙（v0.41，仅本人）：战利品陈列格，四档配色 -->
        <div v-if="isOwnProfile && achs.length" class="card ach-card">
          <div class="done-head">
            <h3>成就墙</h3>
            <div class="ach-meter" :title="`已解锁 ${achUnlocked} / ${achs.length}`">
              <div class="ach-meter-track">
                <div class="ach-meter-fill" :style="{ width: (achUnlocked / achs.length) * 100 + '%' }" />
              </div>
              <span class="mono">{{ achUnlocked }}/{{ achs.length }}</span>
            </div>
          </div>
          <div class="ach-grid">
            <div
              v-for="a in achs"
              :key="a.code"
              class="ach-tile"
              :class="[a.tier, { on: a.unlocked_at }]"
              :title="a.unlocked_at ? `${a.desc} · ${a.unlocked_at.slice(0, 10)} 解锁` : a.desc"
            >
              <span class="ach-icon">{{ a.icon }}</span>
              <span class="ach-name">{{ a.name }}</span>
              <span class="ach-state">{{ a.unlocked_at ? '已达成' : '未解锁' }}</span>
            </div>
          </div>
        </div>

        <!-- 知识星图（v0.41，仅本人）：知识点 AC≥3 点亮 -->
        <div v-if="isOwnProfile && stars.length" class="card star-card">
          <div class="done-head">
            <h3><TipLabel label="知识星图" tip="知识点 AC ≥ 3 题点亮" /></h3>
            <span class="dim">已点亮 {{ stars.filter((x) => x.lit).length }} / {{ stars.length }} 站</span>
          </div>
          <div class="star-grid">
            <div v-for="s in stars" :key="s.tag_id" class="star" :class="{ lit: s.lit }">
              <span class="star-dot">{{ s.lit ? '✦' : '✧' }}</span>
              <span class="star-name">{{ s.name || '未命名知识点' }}</span>
              <span class="star-n mono">{{ s.solved }} 题</span>
            </div>
          </div>
        </div>

        <!-- 错题本（v0.41，仅本人）：复仇入口 -->
        <div v-if="isOwnProfile && wrongbook.length" class="card wb-card">
          <div class="done-head">
            <h3><TipLabel label="错题本" tip="每日 1 次复仇 · 24h 前错过的题经验 ×1.5" /></h3>
          </div>
          <div class="wb-list">
            <router-link v-for="w in wrongbook" :key="w.pid" class="wb-row" :to="`/problems/${w.pid}`">
              <TierBadge :tier="w.difficulty || 3" kind="difficulty" size="sm" />
              <span class="wb-title">{{ w.title }}</span>
              <span class="mono pid">P{{ w.pid }}</span>
              <span class="wb-score">上次 {{ w.best_score }} 分</span>
              <span class="wb-go">去复仇 →</span>
            </router-link>
          </div>
        </div>

        <template #aside>
          <!-- 近期提交：published 投影，点击开评测详情弹窗（权限同详情接口） -->
          <RailCard ov="记录" class="aside-card">
            <template #head>
              <div class="rail-ov">记录</div>
              <div class="rail-t"><TipLabel label="近期提交" tip="仅展示公开态提交，点击行查看评测详情" /></div>
            </template>
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
  gap: var(--soj-sp-20);
  align-items: flex-start;
  padding: var(--soj-sp-20) var(--soj-sp-24);
}

.avatar {
  flex: none;
  width: 64px;
  height: 64px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: var(--soj-fs-28);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-white);
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-blue-lo));
}

/* QQ 头像（v0.23 绑定开关开启时）：图片版首字头像同位同径 */
.avatar-img {
  object-fit: cover;
  border: 1px solid var(--soj-line);
  background: var(--soj-white);
}

.who {
  flex: 1 1 auto;
  min-width: 0;
}

.name-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
}

.name-row h2 {
  margin: 0;
  font-size: var(--soj-fs-20);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
}

.meta {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline; /* @root 是等宽字体，度量与正文不同，需显式按基线对齐 */
  gap: var(--soj-sp-4) var(--soj-sp-14);
  margin-top: var(--soj-sp-6);
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

.lv-progress {
  margin-top: var(--soj-sp-12);
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
}

.lv-bar {
  flex: 0 1 320px;
  height: 8px;
  border-radius: 999px;
  background: var(--soj-surface-2);
  overflow: hidden;
}

.lv-in {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--soj-blue-hi), var(--soj-blue-lo));
  transition: width 0.4s;
}

.lv-txt {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  white-space: nowrap;
}

.hero-stats {
  flex: none;
  display: grid;
  grid-template-columns: repeat(4, minmax(72px, auto));
  gap: var(--soj-sp-8) var(--soj-sp-20);
  text-align: center;
  align-self: center;
}

.stat b {
  display: block;
  font-size: var(--soj-fs-20);
  color: var(--soj-navy);
}

.stat span {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* 按难度完成卡 */
.done-card {
  padding: var(--soj-sp-20) var(--soj-sp-24);
}

.done-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-12);
  flex-wrap: wrap;
}

.done-head h3 {
  margin: 0;
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
}

.seg {
  align-self: center;
}

/* 柱状图（难度完成图，2026-10-26）：柱色走 .diff-N 的 --dc 色轴，点柱切换下方题目列表 */
.dchart {
  margin-top: var(--soj-sp-16);
  display: flex;
  align-items: stretch;
  gap: var(--soj-sp-10);
}

.dbar {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-end;
  gap: var(--soj-sp-8);
  border: 0;
  background: transparent;
  padding: var(--soj-sp-6) var(--soj-sp-2);
  font: inherit;
  color: var(--dc, var(--soj-blue));
  cursor: pointer;
}

.dbar .v {
  min-height: 18px;
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-navy);
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
  background: var(--soj-surface-2);
  opacity: 1;
}

.dbar.zero .v {
  visibility: hidden;
}

/* 选中档题目列表 */
.dlist {
  margin-top: var(--soj-sp-14);
}

.dlist-head {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: 0 var(--soj-sp-2) var(--soj-sp-10);
}

.dcount {
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-12);
}

.glist {
  padding: var(--soj-sp-2) var(--soj-sp-6);
}

/* 截断展开钮（v0.63）：整行文本钮 */
.gmore {
  display: block;
  width: 100%;
  border: 0;
  background: none;
  padding: var(--soj-sp-10);
  font: inherit;
  font-size: var(--soj-fs-13);
  color: var(--soj-blue);
  cursor: pointer;
}

.gmore:hover {
  text-decoration: underline;
}

.prow {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: var(--soj-sp-8) var(--soj-sp-10);
  border-bottom: 1px dashed var(--soj-surface-2);
  font-size: var(--soj-fs-14);
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
  color: var(--soj-navy);
  text-decoration: none;
}

.plink:hover {
  color: var(--soj-blue);
}

.pid {
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-12);
  margin-right: var(--soj-sp-2);
}

.hid {
  flex: none;
}

.when {
  flex: none;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* 副列 */
.aside-card {
  margin-bottom: var(--soj-sp-16);
}

.rtitle {
  font-size: var(--soj-fs-13);
}

.hint {
  margin: var(--soj-sp-10) 0 0;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* 比赛历史行 */
.crow {
  display: flex;
  gap: var(--soj-sp-10);
  align-items: center;
}

.ctitle {
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-navy);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cmeta {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-top: var(--soj-sp-4);
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* 迷你状态胶囊（比赛时段/结果） */
.mini-tag {
  display: inline-flex;
  align-items: center;
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  padding: var(--soj-sp-2) var(--soj-sp-8);
  border-radius: 999px;
  white-space: nowrap;
}

.mini-tag.running {
  color: var(--soj-success-deep);
  background: color-mix(in srgb, var(--soj-success) 12%, transparent);
}

/* stylelint-disable color-no-hex -- 琥珀金状态胶囊定值，v0.57 豁免（无就近 token，暂缓归并） */
.mini-tag.scheduled {
  color: #8a6d1a;
  background: rgba(233, 201, 99, 0.16);
}
/* stylelint-enable color-no-hex */

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
  font-size: var(--soj-fs-16);
  color: var(--soj-blue);
}

.rank-cell span {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
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

<style scoped>
/* ---- 游戏感三卡（v0.41）：成就墙 / 知识星图 / 错题本 ---- */
/* 主列卡片统一呼吸间距（PageShell 槽内无 gap，靠卡片自身 margin） */
.hero,
.done-card,
.ach-card,
.star-card,
.wb-card {
  margin-bottom: var(--soj-sp-16);
}

.ach-card,
.star-card,
.wb-card {
  padding: var(--soj-sp-20) var(--soj-sp-24);
}

/* 成就进度条：细线计量，钩住不施压 */
.ach-meter {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  min-width: 180px;
}

.ach-meter-track {
  flex: 1;
  height: 6px;
  border-radius: 999px;
  background: var(--el-fill-color);
  overflow: hidden;
}

.ach-meter-fill {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--soj-blue), var(--soj-blue-hi));
  transition: width 0.6s ease;
}

.ach-meter .mono {
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-secondary);
}

/* 战利品陈列格：竖排瓦片（图标/名称/状态），四档配色随解锁点亮 */
.ach-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(168px, 1fr));
  gap: var(--soj-sp-12);
  margin-top: var(--soj-sp-16);
}

.ach-tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--soj-sp-4);
  padding: var(--soj-sp-14) var(--soj-sp-10) var(--soj-sp-12);
  border-radius: 12px;
  border: 1px dashed var(--el-border-color-lighter);
  text-align: center;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}

.ach-tile:hover {
  transform: translateY(-2px);
}

.ach-tile .ach-icon {
  font-size: var(--soj-fs-22);
  line-height: 1.2;
  filter: grayscale(1);
  opacity: 0.55;
}

.ach-tile .ach-name {
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-text-color-secondary);
}

.ach-tile .ach-state {
  font-size: var(--soj-fs-12);
  color: var(--el-text-color-placeholder);
}

.ach-tile.on {
  border-style: solid;
}

.ach-tile.on .ach-icon {
  filter: none;
  opacity: 1;
}

.ach-tile.on .ach-name {
  color: var(--el-text-color-primary);
}

.ach-tile.on .ach-state {
  color: var(--soj-blue);
  font-weight: var(--soj-fw-emphasis);
}

/* stylelint-disable color-no-hex -- 成就奖章四档皮肤定值（铜/金/彩），v0.57 豁免 token 化；
   可就近归并的银/金底已迁 token，余值成套保留不拆色组 */
.ach-tile.on.bronze {
  border-color: #dcb58a;
  background: #fdf8f1;
}

.ach-tile.on.silver {
  border-color: var(--soj-ink-3);
  background: var(--soj-surface-2);
}

.ach-tile.on.gold {
  border-color: #e7c268;
  background: var(--soj-tle-bg);
}

.ach-tile.on.color {
  border-color: #c4b5fd;
  background: #f7f4ff;
}
/* stylelint-enable color-no-hex */

.ach-tile.on.gold .ach-icon,
.ach-tile.on.color .ach-icon {
  text-shadow: 0 0 10px rgba(226, 169, 59, 0.35);
}

/* 知识星图：星座感药丸（点亮蓝晕 / 未点亮虚线） */
.star-grid {
  display: flex;
  flex-wrap: wrap;
  gap: var(--soj-sp-10);
  margin-top: var(--soj-sp-16);
}

.star {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-8);
  padding: var(--soj-sp-8) var(--soj-sp-14);
  border-radius: 999px;
  border: 1px dashed var(--el-border-color);
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-secondary);
}

.star.lit {
  border: 1px solid var(--soj-blue);
  background: var(--soj-blue-soft);
  color: var(--soj-blue);
  font-weight: var(--soj-fw-emphasis);
}

.star.lit .star-dot {
  text-shadow: 0 0 8px color-mix(in srgb, var(--soj-blue) 50%, transparent);
}

.star-dot { font-size: var(--soj-fs-14); }
.star-n { font-size: var(--soj-fs-12); opacity: 0.75; }

/* 错题本：宽行 + 难度徽章 + 悬停浮出「去复仇」 */
.wb-list {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-4);
  margin-top: var(--soj-sp-16);
}

.wb-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  padding: var(--soj-sp-12) var(--soj-sp-14);
  border-radius: 10px;
  text-decoration: none;
  color: var(--el-text-color-primary);
  transition: background 0.15s ease;
}

.wb-row:hover {
  background: var(--soj-blue-soft);
}

.wb-title {
  font-weight: var(--soj-fw-emphasis);
  font-size: var(--soj-fs-14);
}

.wb-row .pid {
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-12);
}

.wb-score {
  margin-left: auto;
  font-size: var(--soj-fs-12);
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.wb-go {
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-blue);
  opacity: 0;
  transform: translateX(-4px);
  transition: all 0.15s ease;
  white-space: nowrap;
}

.wb-row:hover .wb-go {
  opacity: 1;
  transform: translateX(0);
}

@media (prefers-reduced-motion: reduce) {
  .ach-tile,
  .ach-meter-fill,
  .wb-row,
  .wb-go {
    transition: none;
  }
  .ach-tile:hover {
    transform: none;
  }
}
</style>

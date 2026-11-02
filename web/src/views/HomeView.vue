<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import HomeAmbient from '@/components/HomeAmbient.vue'
import MdView from '@/components/MdView.vue'
import TierBadge from '@/components/TierBadge.vue'

import { classApi, contestApi, noticeApi, siteApi } from '@/api'
import { authApi, gameApi } from '@/api'
import { fmtDay, fmtShort } from '@/utils/format'
import { pickDailyQuote } from '@/utils/gameText'
import { useUserStore } from '@/stores/user'
import type { ClassCard, ContestListItem } from '@/types'

const router = useRouter()
const userStore = useUserStore()

// 游戏感批（v0.41）：每日钩子
const daily = ref<{ enabled: boolean; pid?: number; title?: string; difficulty?: number; done?: boolean; solved_today?: number } | null>(null)
const revenge = ref<{ pid: number; title: string; difficulty: number; best_score: number }[]>([])
const revengeUsed = ref(false)
const streakDays = ref(0)
const dailyQuote = pickDailyQuote()
const classes = ref<ClassCard[]>([])
const notice = ref('')
const noticeHtml = ref('')

// v0.19 三期（对齐 v3-full 首页）：hero 搜索 + 站点统计行 + 今日一题 + 最近赛事卡
const q = ref('')
const stats = ref<Awaited<ReturnType<typeof siteApi.stats>> | null>(null)
const contests = ref<ContestListItem[]>([])

const today = computed(() => {
  const n = new Date()
  return `${n.getMonth() + 1} 月 ${n.getDate()} 日`
})

const contestLabel: Record<ContestListItem['status'], string> = {
  scheduled: '报名中',
  running: '进行中',
  ended: '已结束',
}

function submitSearch() {
  const kw = q.value.trim()
  router.push(kw ? { path: '/problems', query: { kw } } : { path: '/problems' })
}

onMounted(async () => {
  // v0.37 四块并行（原串行 await 四连：公网链路每请求 ~100ms 双跳，尾块要等
  // ~400ms——allSettled 后整页数据一次到位，各块失败仍互不影响）
  const tasks: Promise<void>[] = [
    // 站点统计 + 今日一题（后端 60s 缓存；失败整块隐藏，不影响页面其余部分）
    siteApi
      .stats()
      .then((s) => {
        stats.value = s
      })
      .catch(() => {
        stats.value = null
      }),
    noticeApi
      .get()
      .then((n) => {
        notice.value = n.content.trim()
      noticeHtml.value = n.html ?? ''
      })
      .catch(() => {
        // 公告加载失败不影响首页其他内容
      }),
    // 每日一题（v0.41：/api/daily 确定性选题，带 ×2 与完成态）
    gameApi
      .daily()
      .then((d) => {
        daily.value = d.enabled ? d : null
      })
      .catch(() => {
        daily.value = null
      }),
  ]
  // 登录用户：复仇推荐 + 坚持火焰
  if (userStore.token) {
    tasks.push(
      gameApi
        .revenge()
        .then((r) => {
          revenge.value = r.list
          revengeUsed.value = r.used
        })
        .catch(() => {
          revenge.value = []
        }),
      authApi
        .getProfile()
        .then((p: { game?: { streak_days?: number } }) => {
          streakDays.value = p.game?.streak_days ?? 0
        })
        .catch(() => {}),
    )
  }
  // 游客可见面收紧（v0.7.3）：班级/比赛接口需登录，游客跳过加载（区块自然隐藏）；
  // 比赛列表 401 会把人从公开首页弹去登录页（2026-10-25 修复⑬），同样只对登录者拉
  if (userStore.token) {
    tasks.push(
      classApi
        .list()
        .then((list) => {
          classes.value = list.slice(0, 3)
        })
        .catch(() => {
          // 班级加载失败不影响首页其他内容
        }),
    )
    tasks.push(
      contestApi
        .list()
        .then((all) => {
          // 最近赛事：进行中/待开赛优先，不足补已结束，最多 3 张
          const pri = (s: ContestListItem['status']) => (s === 'running' ? 0 : s === 'scheduled' ? 1 : 2)
          contests.value = [...all].sort((a, b) => pri(a.status) - pri(b.status)).slice(0, 3)
        })
        .catch(() => {
          contests.value = []
        }),
    )
  }
  await Promise.allSettled(tasks)
})
</script>

<template>
  <div class="page narrow">
    <!-- simple408 同款鼠标氛围（光斑跟随 + 涟漪）：仅主页挂载，其他页面保持纯净 -->
    <HomeAmbient />
    <div v-if="notice" class="announce">
      <span class="announce-tag">公告</span>
      <div class="announce-body"><MdView :html="noticeHtml" /></div>
    </div>

    <!-- v0.19 三期 hero：kicker + 大标题渐变点睛 + 搜索胶囊（orb 渐变圆钮）+ 胶囊按钮组 -->
    <section class="hero">
      <div class="kicker hero-kicker">SANDBOX JUDGED · INSTANT SCORED</div>
      <h1 class="title">每一次提交，<br />都是一次<em>成长</em>。</h1>
      <p class="subtitle">面向班级与社团的顶级轻量 Online Judge：提交代码，沙箱判题，实时出分。</p>
      <div class="hero-search">
        <label class="searchbar">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
          <input v-model="q" placeholder="搜索题号、关键词或知识点，回车直达" @keyup.enter="submitSearch" />
        </label>
        <button class="orb" title="搜索" @click="submitSearch">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M5 12h13M13 6l6 6-6 6" /></svg>
        </button>
      </div>
      <div class="actions">
        <el-button size="large" @click="router.push('/rank')">查看排名</el-button>
        <el-button size="large" @click="router.push('/classes')">进入班级</el-button>
      </div>
    </section>

    <!-- 统计行（公开统计接口，60s 缓存；接口不可用时整块隐藏） -->
    <div v-if="stats" class="statline">
      <div class="cell"><b>{{ stats.problem_count }}</b><span>收录题目</span></div>
      <div class="cell"><b>{{ stats.user_count }}</b><span>注册选手</span></div>
      <div class="cell"><b>{{ stats.today_submissions }}</b><span>今日提交</span></div>
      <div class="cell">
        <b>{{ (stats.avg_judge_ms / 1000).toFixed(1) }}<small>s</small></b><span>平均判题时长</span>
      </div>
    </div>

    <!-- 今日游戏区（v0.41）：每日一句 / 今日挑战 / 复仇之战 —— 统一卡片语言 -->
    <div class="game-sec">
      <div class="gq-row">
        <span class="gq-text">「{{ dailyQuote }}」</span>
        <span v-if="streakDays > 0" class="gq-flame" title="连续有 AC 的天数">🔥 连续 {{ streakDays }} 天</span>
      </div>

      <div v-if="daily" class="gcard" @click="router.push(`/problems/${daily.pid}`)">
        <div class="gc-main">
          <div class="gc-cap">
            <span class="gc-title">🎯 今日挑战</span>
            <span class="gc-date">{{ today }}</span>
            <el-tag v-if="!daily.done" size="small" type="warning" effect="plain">首 AC 掉落 ×2</el-tag>
            <el-tag v-else size="small" type="success" effect="plain">已完成 ✓</el-tag>
          </div>
          <div class="gc-name">{{ daily.pid }} {{ daily.title }}</div>
          <div class="gc-meta">
            <TierBadge :tier="daily.difficulty || 3" kind="difficulty" size="sm" />
            <span v-if="daily.solved_today != null">今日 {{ daily.solved_today }} 人已通过</span>
          </div>
        </div>
        <el-button type="primary">{{ daily.done ? '再做一遍' : '接受挑战' }}</el-button>
      </div>
      <div v-else-if="stats?.featured" class="gcard" @click="router.push(`/problems/${stats.featured.id}`)">
        <div class="gc-main">
          <div class="gc-cap">
            <span class="gc-title">📖 今日一题</span>
            <span class="gc-date">{{ today }}</span>
          </div>
          <div class="gc-name">{{ stats.featured.id }} {{ stats.featured.title }}</div>
          <div class="gc-meta">
            <TierBadge :tier="stats.featured.difficulty || 3" kind="difficulty" size="sm" />
            <span v-if="stats.featured.accept_rate > 0">通过率 {{ stats.featured.accept_rate.toFixed(0) }}%</span>
          </div>
        </div>
        <el-button type="primary">去做这道题</el-button>
      </div>

      <div v-if="revenge.length" class="gcard gc-block">
        <div class="gc-cap">
          <span class="gc-title">⚔️ 复仇之战</span>
          <span class="gc-date">{{ revengeUsed ? '今日复仇已完成 · 明天再来' : '每日一题 ×1.5 · 24 小时前错过的题' }}</span>
        </div>
        <div class="gc-list">
          <button v-for="r in revenge" :key="r.pid" class="gc-row" :disabled="revengeUsed" @click.stop="router.push(`/problems/${r.pid}`)">
            <b class="gc-row-name">{{ r.pid }} {{ r.title }}</b>
            <span class="gc-row-meta">上次 {{ r.best_score }} 分</span>
            <span class="gc-row-go">{{ revengeUsed ? '已复仇 ✓' : '去复仇 →' }}</span>
          </button>
        </div>
      </div>
    </div>

    <!-- 最近赛事：进行中/待开赛优先，不足补已结束；无赛事整块隐藏 -->
    <section v-if="contests.length" class="home-sec">
      <div class="sec-head"><h2>最近赛事</h2><a @click="router.push('/contests')">全部赛事 →</a></div>
      <div class="cgrid">
        <div v-for="c in contests" :key="c.id" class="card ccard">
          <span class="mini-tag" :class="c.status">{{ contestLabel[c.status] }}</span>
          <h3>{{ c.title }}</h3>
          <div class="meta">{{ fmtShort(c.start) }} ~ {{ fmtShort(c.end) }}<br />{{ c.problem_count }} 题</div>
          <el-button type="primary" size="small" style="align-self: flex-start" @click="router.push(`/contests/${c.id}`)">
            进入赛场
          </el-button>
        </div>
      </div>
    </section>

    <!-- 我的班级（登录可见） -->
    <section v-if="classes.length" class="home-sec">
      <div class="sec-head"><h2>我的班级</h2><a @click="router.push('/classes')">全部班级 →</a></div>
      <div class="cgrid">
        <div v-for="c in classes" :key="c.id" class="card ccard">
          <div class="clname-row">
            <h3 @click="router.push(`/classes/${c.id}`)">{{ c.name }}</h3>
            <span v-if="c.private" class="mini-tag lock">🔒 私密</span>
            <span v-else-if="c.joined" class="mini-tag joined">✓ 已加入</span>
          </div>
          <p class="clintro">{{ c.intro.trim() || '暂无简介' }}</p>
          <div class="meta">
            <span>{{ c.member_count }} 名成员</span>·<span>{{ c.assignment_count }} 个作业</span>·<span>创建于 {{ fmtDay(c.created_at) }}</span>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
/* v3 .announce：渐变横条公告（蓝→青极浅）+ 蓝底「公告」胶囊 */
.announce {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  padding: var(--soj-sp-12) var(--soj-sp-20);
  margin-bottom: var(--soj-sp-32);
  border-radius: 14px;
  background: linear-gradient(90deg, rgb(var(--soj-blue-rgb) / 0.07), rgb(var(--soj-teal-rgb) / 0.05));
  border: 1px solid rgb(var(--soj-blue-rgb) / 0.16);
  font-size: var(--soj-fs-14);
}

.announce-tag {
  flex: 0 0 auto;
  padding: var(--soj-sp-2) var(--soj-sp-10);
  border-radius: 999px;
  background: var(--soj-blue);
  color: var(--soj-white);
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  letter-spacing: 0.1em;
}

.announce-body {
  flex: 1;
  min-width: 0;
}

.announce-body :deep(.vditor-reset) {
  font-size: var(--soj-fs-14);
  line-height: 1.6;
}

.announce-body :deep(p) {
  margin: 0;
}

/* ---- hero：开放式居中区块，画布网格即背景 ---- */
.hero {
  text-align: center;
  padding: var(--soj-sp-32) 0 var(--soj-sp-8);
  max-width: 760px;
  margin: 0 auto;
}

/* hero 居中版 kicker：左右对称短线（全局 .kicker 只有左线） */
.hero-kicker {
  margin-bottom: var(--soj-sp-10);
}

.hero-kicker::after {
  content: '';
  width: 22px;
  height: 2px;
  border-radius: 2px;
  background: linear-gradient(90deg, var(--soj-teal), var(--soj-blue-hi));
}

/* v3 hero 标题：--soj-fs-hero 藏蓝 800 + -2.2 负字距；「成长」青蓝渐变点睛 */
.title {
  margin: 0 0 var(--soj-sp-16);
  font-size: var(--soj-fs-hero);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -2.2px;
  line-height: 1.16;
  color: var(--soj-navy);
}

.title em {
  font-style: normal;
  background: linear-gradient(100deg, var(--soj-blue-hi), var(--soj-teal));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.subtitle {
  color: var(--soj-ink-2);
  margin: 0 0 var(--soj-sp-32);
  max-width: 560px;
  margin-left: auto;
  margin-right: auto;
  line-height: 1.85;
}

/* v3 搜索胶囊 + 渐变圆钮（orb） */
.hero-search {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--soj-sp-12);
}

.searchbar {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-10);
  width: min(480px, 100%);
  height: 56px;
  padding-left: var(--soj-sp-20);
  background: var(--el-bg-color);
  border: 1px solid color-mix(in srgb, var(--soj-navy) 13%, transparent);
  border-radius: 999px;
  padding: 0 var(--soj-sp-8) 0 var(--soj-sp-20);
  color: var(--el-text-color-secondary);
  transition: border-color 0.15s, box-shadow 0.15s;
  cursor: text;
}

.searchbar:focus-within {
  border-color: var(--soj-blue);
  box-shadow: 0 0 0 4px var(--soj-blue-soft);
}

.searchbar svg {
  width: 17px;
  height: 17px;
  flex: none;
}

.searchbar input {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  font: inherit;
  font-size: var(--soj-fs-body);
  color: var(--el-text-color-primary);
}

.searchbar input::placeholder {
  color: var(--el-text-color-placeholder);
}

.orb {
  border: 0;
  width: 52px;
  height: 52px;
  border-radius: 50%;
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--soj-white);
  background: linear-gradient(180deg, var(--soj-blue-hi), var(--soj-blue-lo));
  box-shadow: 0 8px 18px rgb(var(--soj-blue-rgb) / 0.32);
  transition: transform 0.18s;
}

.orb:hover {
  transform: scale(1.06);
}

.orb svg {
  width: 20px;
  height: 20px;
}

.actions {
  display: flex;
  justify-content: center;
  gap: var(--soj-sp-12);
  margin-top: var(--soj-sp-16);
}

/* ---- 统计行：--soj-fs-stat 藏蓝数字 + 竖线分隔 ---- */
.statline {
  display: flex;
  justify-content: center;
  max-width: 820px;
  margin: var(--soj-sp-32) auto var(--soj-sp-6);
}

.statline .cell {
  flex: 1;
  text-align: center;
  position: relative;
}

.statline .cell + .cell::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 1px;
  height: 44px;
  background: color-mix(in srgb, var(--soj-navy) 13%, transparent);
}

.statline b {
  display: block;
  font-size: var(--soj-fs-stat);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -1px;
  line-height: 1.2;
  color: var(--soj-navy);
}

.statline b small {
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-bold);
  color: var(--el-text-color-secondary);
  margin-left: var(--soj-sp-2);
}

.statline span {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

/* ---- 今日游戏区（v0.41）：统一卡片语言（每日一句 / 今日挑战 / 复仇之战）---- */
.game-sec {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-12);
  margin: var(--soj-sp-24) 0 var(--soj-sp-6);
}

/* 每日一句横条：与卡片同宽同半径，弱一级（蓝软底） */
.gq-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-12);
  padding: var(--soj-sp-12) var(--soj-sp-16);
  border-radius: 14px;
  background: var(--soj-blue-soft);
}

.gq-text {
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-blue);
}

.gq-flame {
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-warning-deep);
  white-space: nowrap;
}

/* 游戏卡：与 .card 同族（白底 + soj-line + 双层影），横条布局 */
.gcard {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-20);
  padding: var(--soj-sp-16) var(--soj-sp-20);
  border-radius: 16px;
  border: 1px solid var(--soj-line);
  background: var(--el-bg-color);
  box-shadow: var(--soj-shadow-card);
  cursor: pointer;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}

.gcard:hover {
  transform: translateY(-2px);
  box-shadow: var(--soj-shadow-card-hover);
}

.gc-block {
  display: block;
  cursor: default;
}

.gc-cap {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
}

.gc-title {
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
}

.gc-date {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.gc-name {
  font-size: var(--soj-fs-18);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -0.3px;
  color: var(--soj-navy);
  margin: var(--soj-sp-4) 0 var(--soj-sp-4);
}

.gc-meta {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.gcard :deep(.el-button) {
  margin-left: auto;
  flex-shrink: 0;
}

/* 复仇行 */
.gc-list {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-6);
  margin-top: var(--soj-sp-10);
}

.gc-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  padding: var(--soj-sp-8) var(--soj-sp-12);
  border: none;
  border-radius: 10px;
  background: var(--el-fill-color-lighter);
  cursor: pointer;
  text-align: left;
  transition: background 0.15s ease;
}

.gc-row:hover {
  background: var(--soj-blue-soft);
}

.gc-row-name {
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
}

.gc-row-meta {
  margin-left: auto;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.gc-row-go {
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-blue);
  opacity: 0;
  transform: translateX(-4px);
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.gc-row:hover .gc-row-go {
  opacity: 1;
  transform: translateX(0);
}

@media (prefers-reduced-motion: reduce) {
  .gcard,
  .gc-row,
  .gc-row-go {
    transition: none;
  }
  .gcard:hover {
    transform: none;
  }
}

/* ---- 首页分区（最近赛事 / 我的班级）：区块头 + 三列卡 ---- */
.home-sec {
  margin-top: var(--soj-sp-32);
}

.sec-head {
  display: flex;
  align-items: baseline;
  gap: var(--soj-sp-14);
  margin-bottom: var(--soj-sp-16);
}

.sec-head h2 {
  font-size: var(--soj-fs-22);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -0.5px;
  color: var(--soj-navy);
}

.sec-head a {
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-blue);
  margin-left: auto;
  cursor: pointer;
}

.sec-head a:hover {
  text-decoration: underline;
}

.cgrid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--soj-sp-16);
}

.ccard {
  padding: var(--soj-sp-20);
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-8);
  transition:
    transform 0.18s,
    box-shadow 0.18s;
}

.ccard:hover {
  transform: translateY(-3px);
  box-shadow: var(--soj-shadow-card-hover);
}

.ccard h3 {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
  letter-spacing: -0.2px;
}

.ccard .clname-row h3 {
  cursor: pointer;
  margin: 0;
}

.ccard .clname-row h3:hover {
  color: var(--soj-blue);
}

/* 名字行内的迷你点标随行居中（mini-tag 默认 flex-start 是给赛事标题行的，
   在班级名行会把「私密」徽章顶到行顶，与班名错位——2026-10-25 修复） */
.clname-row .mini-tag {
  align-self: center;
}

.ccard .clname-row h3:hover {
  color: var(--soj-blue);
}

.ccard .meta {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  line-height: 1.75;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  flex-wrap: wrap;
  margin-top: var(--soj-sp-2);
}

.clname-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  flex-wrap: wrap;
}

.clintro {
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-regular);
  line-height: 1.7;
  margin: 0;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* 迷你状态点标（赛事状态 / 班级私密、已加入）：圆点 + 文字浅胶囊 */
.mini-tag {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-6);
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  padding: var(--soj-sp-4) var(--soj-sp-12);
  border-radius: 999px;
  white-space: nowrap;
  align-self: flex-start;
}

.mini-tag::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  flex: none;
}

.mini-tag.running {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.mini-tag.scheduled {
  background: var(--el-color-warning-light-9);
  color: var(--el-color-warning);
}

.mini-tag.ended {
  background: var(--soj-mut-bg);
  color: var(--soj-mut);
}

/* v3 .joined / .lock：班级卡「已加入」绿胶囊、「私密」橙锁胶囊 */
.mini-tag.joined {
  background: var(--soj-ac-bg);
  color: var(--soj-ac);
}

.mini-tag.lock {
  background: var(--soj-tle-bg);
  color: var(--soj-tle);
}

@media (max-width: 900px) {
  .cgrid {
    grid-template-columns: 1fr;
  }

  .statline {
    flex-wrap: wrap;
    gap: var(--soj-sp-16);
  }

  .statline .cell + .cell::before {
    display: none;
  }

  .gcard {
    flex-direction: column;
    align-items: flex-start;
  }

  .gcard :deep(.el-button) {
    margin-left: 0 !important;
  }
}
</style>

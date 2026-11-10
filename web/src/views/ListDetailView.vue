<script setup lang="ts">
// 题单详情页（v0.18.3，plan §2.12；2026-10-26 参照题库/比赛界面重设计）：
// 名称卡（比赛详情同款：26px 大标题 + 隐藏标 + meta + 简介 + 进度条 + 难度堆叠柱）；
// Tab「题目」= 章节折叠（组头 = 自动编号 + 标题 + x/y 渐变小进度条）+ 组内 el-table
//（题库表格语言：状态/题号/题目/难度 四列；小节做跨列组行）；Tab「排名」= 完成排名
//（并列跳号、榜外 me 补算、每页 100 名）。副列按 web-design.md：管理卡 → 目录 → 说明。
// 折叠面（v0.70 重设计，拍板预览 = private_doc/preview/list-collapse-redesign-preview.html）：
// 默认静默（章头 surface-2 / 节条白行+发丝线+缩进），展开态才上蓝信号（蓝浅底+左蓝条+
// 三角变色+胶囊加深）；目录跳转目标行闪蓝。提案前旧案「蓝青渐变底+常驻蓝竖条」退役。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { onBeforeRouteLeave } from 'vue-router'

import MdView from '@/components/MdView.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import SubStatus from '@/components/SubStatus.vue'
import DiffChart from '@/components/DiffChart.vue'
import TierBadge from '@/components/TierBadge.vue'
import UserNameLevel from '@/components/UserNameLevel.vue'
import { listApi } from '@/api'
import { useListQuery } from '@/composables/useListQuery'
import { useUserStore } from '@/stores/user'
import type { ListChapter, ListDetail, ListProblemEntry, ListRankResult } from '@/types'
import { fmtDay } from '@/utils/format'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const detail = ref<ListDetail | null>(null)
const loading = ref(true)
const tab = ref<'problems' | 'rank'>('problems')
// URL 即状态（v0.39）：tab + 排名页码进 query（rp 键避开题库 page 语义）
const lq = useListQuery()
const expanded = ref<Record<string, boolean>>({})

async function load() {
  loading.value = true
  try {
    detail.value = await listApi.detail(Number(route.params.id))
    // v0.82 恢复展开快照（sessionStorage 会话级）：从题单点题离开后浏览器返回，
    // 章节/小节保持离开时的展开样子；新会话/新标签页无快照，回落默认静默
    const saved = sessionStorage.getItem(expandKey())
    if (saved) expanded.value = JSON.parse(saved) as Record<string, boolean>
    // v0.82.1 恢复滚动位置（与展开快照同款会话级）：nextTick 后内容仍在分帧长高
    //（el-table 行渲染+展开树插入），双 rAF 等布局稳定再滚；Chrome scroll anchoring
    // 会在内容长高时按锚点调整滚动（ego 实锤快照 2000 被冲成 31268），chapters 子树禁锚
    const savedY = Number(sessionStorage.getItem(scrollKey()) || 0)
    await nextTick()
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        if (savedY > 0) window.scrollTo(0, savedY)
      }),
    )
  } finally {
    loading.value = false
  }
}

// v0.82 展开快照按题单 id 隔离
function expandKey(): string {
  return `soj-list-expand:${route.params.id}`
}

// v0.82.1 滚动位置快照（同会话级同题单隔离）：点题离开时记录视口位置，返回恢复
function scrollKey(): string {
  return `soj-list-scroll:${route.params.id}`
}

onBeforeRouteLeave(() => {
  sessionStorage.setItem(scrollKey(), String(window.scrollY))
})

// 展开/收起/全部展开/目录直达任一变更即写快照（深度 watch，与渲染同源零漏记）
watch(expanded, (v) => sessionStorage.setItem(expandKey(), JSON.stringify(v)), { deep: true })

onMounted(() => {
  tab.value = lq.str('tab') === 'rank' ? 'rank' : 'problems'
  rankPage.value = lq.num('rp')
  // v0.82.1 接管滚动恢复：浏览器原生 history.scrollRestoration=auto 在 SPA 重挂载场景
  // 会拿陈旧历史位置异步覆盖（ego 实锤快照 2000 被冲成 31268），本页滚动全由快照管
  history.scrollRestoration = 'manual'
  load()
  if (tab.value === 'rank') loadRank()
})
onBeforeUnmount(() => {
  history.scrollRestoration = 'auto' // 离开本页还原浏览器默认行为，不影响其他页面
})
watch(() => route.params.id, load)

const pct = computed(() => {
  const d = detail.value
  return d && d.total ? Math.round((d.solved / d.total) * 100) : 0
})

// 每档已完成数（去重按题；DiffChart 的 solved 入参）
const solvedByDiff = computed(() => {
  const out = Array<number>(10).fill(0)
  const seen = new Set<number>()
  for (const ch of detail.value?.chapters ?? []) {
    for (const e of ch.problems) countEntry(e)
    for (const sec of ch.sections) for (const e of sec.problems) countEntry(e)
  }
  function countEntry(e: ListProblemEntry) {
    if (seen.has(e.id)) return
    seen.add(e.id)
    if (e.status === 'Accepted' && e.difficulty >= 1 && e.difficulty <= 10) out[e.difficulty - 1]++
  }
  return out
})

function chapterCount(ch: ListChapter): { solved: number; total: number } {
  let solved = 0
  let total = 0
  const walk = (es: ListProblemEntry[]) => {
    for (const e of es) {
      total++
      if (e.status === 'Accepted') solved++
    }
  }
  walk(ch.problems)
  for (const s of ch.sections) walk(s.problems)
  return { solved, total }
}

function sectionCount(sec: ListChapter['sections'][number]): { solved: number; total: number } {
  let solved = 0
  for (const e of sec.problems) {
    if (e.status === 'Accepted') solved++
  }
  return { solved, total: sec.problems.length }
}

function toggle(key: string) {
  expanded.value[key] = !expanded.value[key]
}
// 全部展开/收起：章节与小节一起
function expandAll(v: boolean) {
  const next: Record<string, boolean> = {}
  for (const ch of detail.value?.chapters ?? []) {
    next[`ch:${ch.no}`] = v
    for (const s of ch.sections) next[`sec:${ch.no}:${s.no}`] = v
  }
  expanded.value = next
}
const allExpanded = computed(() => {
  const chs = detail.value?.chapters ?? []
  return (
    chs.length > 0 &&
    chs.every(
      (c) => expanded.value[`ch:${c.no}`] && c.sections.every((s) => expanded.value[`sec:${c.no}:${s.no}`]),
    )
  )
})

// 目录卡：小节列表的展开态（默认展开）；章/节点击即跳
const tocOpen = ref<Record<string, boolean>>({})
// 目录卡默认收起（v0.71.2，用户拍板）：展开后超 420px 出内部滚动条（编辑页目录同款）
const tocCardOpen = ref(false)
const secTotal = computed(() => detail.value?.chapters.reduce((n, c) => n + c.sections.length, 0) ?? 0)

// 目录跳转目标闪蓝提示（v0.70）：rAF 时 v-if 懒挂载已完成，挂 .flash 类
// （::before 覆盖层 700ms 后摘类、0.9s 淡出），滚过去一眼能找到落点
function flashTarget(id: string) {
  requestAnimationFrame(() => {
    const el = document.getElementById(id)
    if (!el) return
    el.scrollIntoView({ behavior: 'smooth', block: 'start' })
    el.classList.add('flash')
    window.setTimeout(() => el.classList.remove('flash'), 700)
  })
}

function jumpTo(ch: ListChapter) {
  expanded.value[`ch:${ch.no}`] = true
  for (const s of ch.sections) expanded.value[`sec:${ch.no}:${s.no}`] = true
  tocOpen.value[ch.no] = true
  tab.value = 'problems'
  flashTarget(`chap-${ch.no}`)
}

function jumpToSec(ch: ListChapter, sec: ListChapter['sections'][number]) {
  expanded.value[`ch:${ch.no}`] = true
  expanded.value[`sec:${ch.no}:${sec.no}`] = true
  tocOpen.value[ch.no] = true
  tab.value = 'problems'
  flashTarget(`sec-${ch.no}-${sec.no}`)
}

// ---- 排名 Tab ----
const rank = ref<ListRankResult | null>(null)
const rankLoading = ref(false)
const rankPage = ref(1)
const rankTotal = ref(0)

async function loadRank() {
  rankLoading.value = true
  try {
    const r = await listApi.rank(Number(route.params.id), rankPage.value)
    rank.value = r
    rankTotal.value = r.total
  } finally {
    rankLoading.value = false
  }
}
watch(tab, (t) => {
  lq.sync({ tab: t !== 'problems' ? t : undefined, rp: t === 'rank' && rankPage.value > 1 ? rankPage.value : undefined })
  if (t === 'rank' && !rank.value) loadRank()
})
function onRankPage(p: number) {
  rankPage.value = p
  lq.sync({ tab: tab.value !== 'problems' ? tab.value : undefined, rp: p > 1 ? p : undefined })
  loadRank()
}

const pctOf = (n: number) => (detail.value?.total ? Math.round((n / detail.value.total) * 100) : 0)
</script>

<template>
  <div v-loading="loading">
    <template v-if="detail">
      <PageShell>
        <template #standalone>
          <!-- 名称卡（参照比赛详情）：大标题 + meta + 简介 + 进度/难度分布 -->
          <el-card class="name-card" :body-style="{ padding: '22px 26px' }">
            <div class="name-cols">
              <!-- 左列：名字 / 说明 / 总进度条 -->
              <div class="name-left">
                <div class="name-row">
                  <span class="big-title">{{ detail.title }}</span>
                  <el-tag v-if="detail.hidden" size="small" type="danger" effect="plain">隐藏</el-tag>
                </div>
                <div class="meta-line">
                  {{ detail.total }} 题 · {{ detail.chapters.length }} 章 · 创建于 {{ fmtDay(detail.created_at) }}
                </div>
                <div v-if="detail.intro" class="intro">
                  <MdView :html="detail.intro_html ?? ''" />
                </div>
                <div class="progress-block">
                  <div class="progress-txt">
                    <b class="mono">{{ detail.solved }}</b>
                    <span class="mono">/{{ detail.total }}</span>
                    <em>已完成</em>
                  </div>
                  <div class="soj-pbar"><i :style="{ width: `${pct}%` }" /></div>
                </div>
              </div>
              <!-- 右列：难度分布堆叠柱（淡竖线分隔） -->
              <div class="diff-block">
                <DiffChart :total="detail.difficulties" :solved="solvedByDiff" :height="116" />
              </div>
            </div>
          </el-card>
        </template>

        <el-tabs v-model="tab" class="ltabs">
          <el-tab-pane label="题目" name="problems" />
          <el-tab-pane label="排名" name="rank" />
        </el-tabs>

        <div v-show="tab === 'problems'">
          <div class="ptools">
            <span class="hint">点击章节/小节标题展开；目录可直达</span>
            <el-button link type="primary" size="small" @click="expandAll(!allExpanded)">
              {{ allExpanded ? '全部收起' : '全部展开' }}
            </el-button>
          </div>

          <el-empty
            v-if="!detail.chapters.length"
            description="这个题单还没有章节，等创建者来填充吧"
            :image-size="70"
          />
          <div v-else class="chapters">
            <div v-for="ch in detail.chapters" :id="`chap-${ch.no}`" :key="ch.no" class="chapter">
              <button class="chead" :class="{ open: expanded[`ch:${ch.no}`] }" @click="toggle(`ch:${ch.no}`)">
                <span class="tri" :class="{ open: expanded[`ch:${ch.no}`] }" />
                <span class="cno mono">{{ ch.no }}</span>
                <span class="ctitle">
                  {{ ch.title }}
                  <small v-if="ch.desc" class="cdesc">{{ ch.desc }}</small>
                </span>
                <span class="cbar">
                  <span class="cbar-in" :style="{ width: `${chapterCount(ch).total ? (chapterCount(ch).solved / chapterCount(ch).total) * 100 : 0}%` }" />
                </span>
                <span class="ccount mono">{{ chapterCount(ch).solved }}/{{ chapterCount(ch).total }}</span>
                <span class="tg">{{ expanded[`ch:${ch.no}`] ? '收起' : '展开' }}</span>
              </button>
              <!-- v-if 懒挂载（v0.62 挂载冻结根治批）：折叠章不建组件树（原 v-show 全量
                   挂载，大题单 300~600 题 = 同量 FitTags/表格一次建完） -->
              <div v-if="expanded[`ch:${ch.no}`]" class="cbody">
                <!-- 章直挂题 -->
                <el-table v-if="ch.problems.length" :data="ch.problems" class="ptable">
                  <el-table-column label="状态" width="140">
                    <template #default="{ row }">
                      <SubStatus v-if="row.status" :status="row.status" :score="row.my_score" />
                      <span v-else class="dim">未做</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="题号" width="96">
                    <template #default="{ row }"><span class="mono dim">P{{ row.id }}</span></template>
                  </el-table-column>
                  <el-table-column label="题目" min-width="320">
                    <template #default="{ row }">
                      <template v-if="row.locked">
                        <span class="ptitle locked"><b>{{ row.title }}</b></span>
                        <el-tag size="small" type="success" class="ml8">
                          {{ row.lock_kind === 'exam' ? '考试中' : '比赛中' }}
                        </el-tag>
                      </template>
                      <template v-else>
                        <router-link class="ptitle" :to="`/problems/${row.id}`"><b>{{ row.title }}</b></router-link>
                        <el-tag v-if="row.hidden" size="small" type="danger" effect="plain" class="ml8">隐藏</el-tag>
                        <el-tag v-if="row.duplicate" size="small" type="info" effect="plain" class="ml8">重复</el-tag>
                      </template>
                    </template>
                  </el-table-column>
                  <el-table-column label="难度" width="130">
                    <template #default="{ row }">
                      <TierBadge :tier="row.difficulty" kind="difficulty" size="sm" />
                    </template>
                  </el-table-column>
                  <el-table-column label="知识点" min-width="200">
                    <template #default="{ row }">
                      <FitTags v-if="row.tags?.length" :ids="row.tags" />
                      <span v-else class="dim">—</span>
                    </template>
                  </el-table-column>
                </el-table>

                <!-- 小节：独立折叠条 + 各自表格（表头在小节内） -->
                <div v-for="sec in ch.sections" :key="sec.no" class="sec-block">
                  <button
                    :id="`sec-${ch.no}-${sec.no}`"
                    class="sbar"
                    :class="{ open: expanded[`sec:${ch.no}:${sec.no}`] }"
                    @click="toggle(`sec:${ch.no}:${sec.no}`)"
                  >
                    <span class="tri sm" :class="{ open: expanded[`sec:${ch.no}:${sec.no}`] }" />
                    <span class="cno mono">{{ sec.no }}</span>
                    <span class="stitle">
                      {{ sec.title }}
                      <small v-if="sec.desc" class="cdesc">{{ sec.desc }}</small>
                    </span>
                    <span class="cbar">
                      <span
                        class="cbar-in"
                        :style="{ width: `${sectionCount(sec).total ? (sectionCount(sec).solved / sectionCount(sec).total) * 100 : 0}%` }"
                      />
                    </span>
                    <span class="ccount mono">{{ sectionCount(sec).solved }}/{{ sectionCount(sec).total }}</span>
                    <span class="tg">{{ expanded[`sec:${ch.no}:${sec.no}`] ? '收起' : '展开' }}</span>
                  </button>
                  <!-- v-if 懒挂载（v0.62）：同章节折叠体口径，折叠小节不建表格 -->
                  <div v-if="expanded[`sec:${ch.no}:${sec.no}`]" class="sec-table">
                    <el-table v-if="sec.problems.length" :data="sec.problems" class="ptable">
                      <el-table-column label="状态" width="140">
                        <template #default="{ row }">
                          <SubStatus v-if="row.status" :status="row.status" :score="row.my_score" />
                          <span v-else class="dim">未做</span>
                        </template>
                      </el-table-column>
                      <el-table-column label="题号" width="96">
                        <template #default="{ row }"><span class="mono dim">P{{ row.id }}</span></template>
                      </el-table-column>
                      <el-table-column label="题目" min-width="320">
                        <template #default="{ row }">
                          <template v-if="row.locked">
                            <span class="ptitle locked"><b>{{ row.title }}</b></span>
                            <el-tag size="small" type="success" class="ml8">
                              {{ row.lock_kind === 'exam' ? '考试中' : '比赛中' }}
                            </el-tag>
                          </template>
                          <template v-else>
                            <router-link class="ptitle" :to="`/problems/${row.id}`"><b>{{ row.title }}</b></router-link>
                            <el-tag v-if="row.hidden" size="small" type="danger" effect="plain" class="ml8">隐藏</el-tag>
                            <el-tag v-if="row.duplicate" size="small" type="info" effect="plain" class="ml8">重复</el-tag>
                          </template>
                        </template>
                      </el-table-column>
                      <el-table-column label="难度" width="130">
                        <template #default="{ row }">
                          <TierBadge :tier="row.difficulty" kind="difficulty" size="sm" />
                        </template>
                      </el-table-column>
                      <el-table-column label="知识点" min-width="200">
                        <template #default="{ row }">
                          <FitTags v-if="row.tags?.length" :ids="row.tags" />
                          <span v-else class="dim">—</span>
                        </template>
                      </el-table-column>
                    </el-table>
                    <p v-else class="hint sempty">本节暂无题目</p>
                  </div>
                </div>

                <p v-if="!ch.problems.length && !ch.sections.length" class="hint cempty">本章暂无内容</p>
              </div>
            </div>
          </div>
        </div>

        <div v-show="tab === 'rank'" v-loading="rankLoading" class="rank-wrap">
          <div class="rank-me">
            <template v-if="rank?.me">
              我的名次 <b class="mono">#{{ rank.me.rank }}</b>
              <span class="mono">{{ rank.me.solved }}/{{ detail.total }}</span>
            </template>
            <template v-else>你还没做过这个题单的题，去「题目」页开刷吧</template>
            <span class="mono dim">共 {{ rankTotal }} 人有进度</span>
          </div>
          <el-table :data="rank?.items ?? []" stripe>
            <el-table-column label="名次" width="88">
              <template #default="{ $index }">
                <b class="mono">{{ rank?.items[$index].rank }}</b>
              </template>
            </el-table-column>
            <el-table-column label="选手" min-width="220">
              <template #default="{ row }">
                <UserNameLevel :name="row.nickname || row.username" :level="row.level" :uid="row.uid" :me="row.uid === userStore.uid" />
              </template>
            </el-table-column>
            <el-table-column label="解题数" width="100">
              <template #default="{ row }">
                <span class="mono">{{ row.solved }}</span>
              </template>
            </el-table-column>
            <el-table-column label="完成度" min-width="180">
              <template #default="{ row }">
                <div class="rpct">
                  <div class="rpct-bar"><div class="rpct-in" :style="{ width: `${pctOf(row.solved)}%` }" /></div>
                  <span class="mono dim">{{ pctOf(row.solved) }}%</span>
                </div>
              </template>
            </el-table-column>
          </el-table>
          <div class="pager">
            <el-pagination
              background
              layout="prev, pager, next, total"
              :total="rankTotal"
              :page-size="rank?.page_size ?? 100"
              :current-page="rankPage"
              @current-change="onRankPage"
            />
          </div>
        </div>

        <template #aside>
          <!-- 副列原则（web-design.md）：管理卡第一——编辑等权限入口（删除入口在编辑器） -->
          <RailCard v-if="detail.manageable" ov="管理" title="题单管理" class="aside-card">
            <el-button type="primary" @click="router.push(`/lists/${detail.id}/edit`)">编辑题单</el-button>
            <p v-if="detail.hidden" class="hint">隐藏中：学生端看不到本题单，仅你与 root 可见。</p>
          </RailCard>

          <!-- 重要信息：章节目录（吸顶跟随；默认收起，展开超高出内部滚动条；
               章/节点击即跳并自动展开；小节随 v0.70 折叠口径默认收起） -->
          <RailCard ov="目录" title="章节目录" class="aside-card toc-card">
            <button class="toc-toggle" @click="tocCardOpen = !tocCardOpen">
              <span class="toc-chevron" :class="{ open: tocCardOpen }">▸</span>
              <span class="toc-title">{{ detail.chapters.length }} 章{{ secTotal ? ` · ${secTotal} 节` : '' }}</span>
            </button>
            <div v-show="tocCardOpen" class="toc">
              <div v-for="ch in detail.chapters" :key="ch.no" class="toc-group">
                <div class="toc-chap">
                  <button
                    v-if="ch.sections.length"
                    class="toc-chevron"
                    :class="{ open: tocOpen[ch.no] === true }"
                    :title="tocOpen[ch.no] === true ? '收起小节' : '展开小节'"
                    @click="tocOpen[ch.no] = tocOpen[ch.no] === true ? false : true"
                  >
▸
</button>
                  <button class="toc-row" @click="jumpTo(ch)">
                    <span class="cno mono">{{ ch.no }}</span>
                    <span class="toc-title">{{ ch.title }}</span>
                  </button>
                </div>
                <div v-show="tocOpen[ch.no] === true" class="toc-sub">
                  <button v-for="sec in ch.sections" :key="sec.no" class="toc-row sub" @click="jumpToSec(ch, sec)">
                    <span class="cno mono">{{ sec.no }}</span>
                    <span class="toc-title">{{ sec.title }}</span>
                  </button>
                </div>
              </div>
            </div>
          </RailCard>
        </template>
      </PageShell>
    </template>
    <el-empty v-else-if="!loading" description="题单不存在" :image-size="90">
      <el-button type="primary" @click="router.push('/lists')">回题单页</el-button>
    </el-empty>
  </div>
</template>

<style scoped>
/* 名称卡（比赛详情同款）：entity-detail 大标题 + meta + 简介 + 进度/难度分布 */
.name-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  flex-wrap: wrap;
}

.big-title {
  font-size: var(--soj-fs-entity-detail);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -0.5px;
  color: var(--soj-navy);
}

.meta-line {
  margin-top: var(--soj-sp-6);
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

.intro {
  margin-top: var(--soj-sp-12);
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
}

.intro :deep(p) {
  margin: 0;
}

/* 名称卡左右双列：左 = 名字/说明/进度，右 = 难度堆叠柱（淡竖线分隔） */
.name-cols {
  display: flex;
  gap: var(--soj-sp-24);
  align-items: center;
}

.name-left {
  flex: 1 1 auto;
  min-width: 0;
}

.progress-block {
  margin-top: var(--soj-sp-16);
}

.progress-txt {
  display: flex;
  align-items: baseline;
  gap: var(--soj-sp-6);
  margin-bottom: var(--soj-sp-8);
}

.progress-txt b {
  font-size: var(--soj-fs-22);
  color: var(--soj-navy);
}

.progress-txt span {
  font-size: var(--soj-fs-14);
  color: var(--soj-ink-3);
}

.progress-txt em {
  font-style: normal;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  margin-left: var(--soj-sp-4);
}

.diff-block {
  flex: none;
  display: flex;
  align-items: center;
  align-self: stretch;
  padding-left: var(--soj-sp-24);
  border-left: 1px solid var(--soj-line);
}

.ltabs {
  margin-top: var(--soj-sp-6);
}

/* 题目 Tab */
.ptools {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-12);
  margin-bottom: var(--soj-sp-10);
}

.ptools .hint {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.chapters {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-10);
  /* v0.82.1 子树禁滚动锚定：展开/表格分帧渲染长高时 Chrome 锚点调整会把快照恢复的
     滚动位置冲掉（实测 2000→31268），本页滚动只归滚动快照管 */
  overflow-anchor: none;
}

.chapter {
  border: 1px solid var(--soj-line);
  border-radius: 12px;
  background: var(--soj-white);
  overflow: hidden;
}

/* 组头（v0.70）：默认静默 surface-2，展开态蓝浅底 + 左蓝条出现；悬停各加深一档 */
.chead {
  position: relative;
  width: 100%;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: var(--soj-sp-20) var(--soj-sp-16);
  min-height: 70px;
  background: var(--soj-surface-2);
  border: none;
  border-left: 3px solid transparent;
  cursor: pointer;
  text-align: left;
  transition: background 0.15s;
}

.chead:hover {
  background: var(--soj-surface-3);
}

.chead.open {
  background: rgb(var(--soj-blue-rgb) / 0.045);
  border-left-color: var(--soj-blue);
}

.chead.open:hover {
  background: rgb(var(--soj-blue-rgb) / 0.08);
}

/* 章号软蓝序号章（v0.70）：层级锚点，展开态加深一档（小节序号仍为裸数字转灰） */
.chead .cno {
  min-width: 26px;
  height: 26px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border-radius: 7px;
  background: rgb(var(--soj-blue-rgb) / 0.09);
}

.chead.open .cno {
  background: rgb(var(--soj-blue-rgb) / 0.16);
}

.cno {
  flex: none;
  min-width: 30px;
  font-weight: var(--soj-fw-bold);
  color: var(--soj-blue);
  font-size: var(--soj-fs-14);
}

.ctitle {
  flex: 1 1 auto;
  min-width: 0;
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cdesc {
  font-weight: var(--soj-fw-regular);
  color: var(--soj-ink-3);
  margin-left: var(--soj-sp-8);
  font-size: var(--soj-fs-12);
}

.cbar {
  flex: none;
  width: 110px;
  height: 6px;
  border-radius: 999px;
  background: var(--soj-surface-2);
  overflow: hidden;
}

.cbar-in {
  display: block;
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--soj-blue-hi), var(--soj-teal));
}

/* 章头进度轨道随头底加深一档（surface-2 头上原轨道色不可见）；小节条白底仍用 surface-2 */
.chead .cbar {
  background: rgb(var(--soj-blue-rgb) / 0.1);
}

.ccount {
  flex: none;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  min-width: 44px;
  text-align: right;
}

/* 组头右侧两字开关（拍板 2：不要超小图标）升级软蓝胶囊（v0.70）：像按钮、点感明确，
   展开态加深一档；两字等宽不跳动 */
.tg {
  flex: none;
  padding: var(--soj-sp-4) var(--soj-sp-12);
  border-radius: var(--soj-radius-pill);
  background: rgb(var(--soj-blue-rgb) / 0.08);
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-blue);
  text-align: center;
}

.open .tg {
  background: rgb(var(--soj-blue-rgb) / 0.15);
}

.cbody {
  padding: 0;
}

/* 表格内题目链接与内联标签 */
.ptitle {
  color: var(--soj-navy);
  text-decoration: none;
}

a.ptitle:hover {
  color: var(--soj-blue);
}

.ptitle.locked {
  color: var(--soj-ink-3);
  cursor: not-allowed;
}

/* 可点展开三角形（章节/小节通用）：大号明显指示；收起灰、展开蓝（方向+颜色双信号） */
.tri {
  flex: none;
  width: 0;
  height: 0;
  border-left: 8px solid var(--soj-ink-3);
  border-top: 6px solid transparent;
  border-bottom: 6px solid transparent;
  transition:
    transform 0.2s,
    border-left-color 0.2s;
}

.tri.open {
  transform: rotate(90deg);
  border-left-color: var(--soj-blue);
}

/* 小节折叠条：行高对齐章节组头原高；白行 + 发丝线分隔 + 缩进（对齐章标题之下，
   层级靠结构表达），独立成条、各自带表；展开态更浅蓝底 + 左蓝条（条宽与章头一致） */
.sec-block {
  border-top: 1px solid var(--soj-line);
}

.sbar {
  position: relative;
  width: 100%;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: var(--soj-sp-12) var(--soj-sp-16);
  padding-left: var(--soj-sp-48);
  min-height: 48px;
  background: var(--soj-white);
  border: none;
  border-left: 3px solid transparent;
  cursor: pointer;
  text-align: left;
  transition: background 0.15s;
}

.sbar:hover {
  background: var(--soj-surface-2);
}

.sbar.open {
  background: rgb(var(--soj-blue-rgb) / 0.035);
  border-left-color: var(--soj-blue);
}

/* 小节序号转灰让层级，展开态回主蓝 */
.sbar .cno {
  color: var(--soj-ink-3);
}

.sbar.open .cno {
  color: var(--soj-blue);
}

.sbar .tri {
  border-left-width: 7px;
  border-top-width: 5px;
  border-bottom-width: 5px;
}

.stitle {
  flex: 1 1 auto;
  min-width: 0;
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sempty,
.cempty {
  margin: 0;
  padding: var(--soj-sp-12) var(--soj-sp-16);
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* 空节提示随小节条缩进对齐 */
.sempty {
  padding-left: var(--soj-sp-48);
}

/* 目录跳转目标闪蓝（v0.70）：::before 覆盖层淡出、不动布局；jumpTo 里挂/摘 .flash */
.chead::before,
.sbar::before {
  content: '';
  position: absolute;
  inset: 0;
  background: rgb(var(--soj-blue-hi-rgb) / 0.22);
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.9s ease-out;
}

.chead.flash::before,
.sbar.flash::before {
  opacity: 1;
  transition: none;
}

.cdesc {
  font-weight: var(--soj-fw-regular);
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-12);
}

.dim {
  color: var(--soj-ink-3);
}

/* 排名 Tab */
.rank-wrap {
  min-height: 200px;
}

.rank-me {
  display: flex;
  align-items: baseline;
  gap: var(--soj-sp-10);
  margin-bottom: var(--soj-sp-12);
  font-size: var(--soj-fs-14);
  color: var(--soj-navy);
}

.rank-me b {
  font-size: var(--soj-fs-18);
  color: var(--soj-blue);
}

.rank-me .dim {
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-12);
}

.rpct {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
}

.rpct-bar {
  flex: 1 1 auto;
  max-width: 180px;
  height: 6px;
  border-radius: 999px;
  background: var(--soj-surface-2);
  overflow: hidden;
}

.rpct-in {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--soj-blue-hi), var(--soj-teal));
}

.rpct .dim {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.pager {
  display: flex;
  justify-content: center;
  margin-top: var(--soj-sp-16);
}

/* TOC */
.toc {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-2);
  /* 展开超 420px 出内部滚动条（编辑页章节目录同款口径） */
  max-height: 420px;
  overflow-y: auto;
}

/* 目录卡收起/展开切换行（v0.70 折叠语言：三角灰→蓝 + 计数） */
.toc-toggle {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  width: 100%;
  border: none;
  background: none;
  padding: var(--soj-sp-4) var(--soj-sp-8);
  border-radius: 8px;
  cursor: pointer;
  text-align: left;
  font-size: var(--soj-fs-14);
  color: var(--soj-navy);
}

.toc-toggle:hover {
  background: var(--soj-surface-3);
}

.toc-toggle + .toc {
  margin-top: var(--soj-sp-4);
}

.toc-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  border: none;
  background: none;
  padding: var(--soj-sp-8) var(--soj-sp-8);
  border-radius: 8px;
  cursor: pointer;
  text-align: left;
  font-size: var(--soj-fs-14);
  color: var(--soj-navy);
}

.toc-row:hover {
  background: var(--soj-surface-3);
}

.toc-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.aside-card {
  margin-bottom: var(--soj-sp-16);
}

/* 目录卡吸顶：随滚动跟随。sticky 需要父列有行程——
   覆掉 PageShell .cols 的 align-items:flex-start，让右列拉伸到与主列等高 */
.page-shell :deep(.side-col) {
  align-self: stretch;
}

.toc-card {
  position: sticky;
  top: var(--soj-sticky-top);
}

.toc-group + .toc-group {
  margin-top: var(--soj-sp-2);
}

.toc-chap {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-4);
}

/* 组级小节列表收起/展开的旋转箭头 */
.toc-chevron {
  flex: none;
  width: 24px;
  height: 24px;
  border: none;
  background: none;
  color: var(--soj-blue);
  font-size: var(--soj-fs-16);
  cursor: pointer;
  transition: transform 0.2s;
  transform: rotate(90deg);
  padding: 0;
  line-height: 1;
}

.toc-chevron:not(.open) {
  transform: rotate(0deg);
}

.toc-sub {
  margin-left: var(--soj-sp-32);
}

.toc-row.sub {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  padding: var(--soj-sp-4) var(--soj-sp-8);
}

.toc-row.sub .cno {
  color: var(--soj-ink-3);
}

.aside-card .el-button {
  width: 100%;
}

.hint {
  margin: var(--soj-sp-10) 0 0;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}
</style>

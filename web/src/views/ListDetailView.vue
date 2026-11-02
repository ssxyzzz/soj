<script setup lang="ts">
// 题单详情页（v0.18.3，plan §2.12；2026-10-26 参照题库/比赛界面重设计）：
// 名称卡（比赛详情同款：26px 大标题 + 隐藏标 + meta + 简介 + 进度条 + 难度堆叠柱）；
// Tab「题目」= 章节折叠（组头 = 自动编号 + 标题 + x/y 渐变小进度条）+ 组内 el-table
//（题库表格语言：状态/题号/题目/难度 四列；小节做跨列组行）；Tab「排名」= 完成排名
//（并列跳号、榜外 me 补算、每页 100 名）。副列按 web-design.md：管理卡 → 目录 → 说明。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

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
  } finally {
    loading.value = false
  }
}
onMounted(() => {
  tab.value = lq.str('tab') === 'rank' ? 'rank' : 'problems'
  rankPage.value = lq.num('rp')
  load()
  if (tab.value === 'rank') loadRank()
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
    if (seen.has(e.pid)) return
    seen.add(e.pid)
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

function jumpTo(ch: ListChapter) {
  expanded.value[`ch:${ch.no}`] = true
  for (const s of ch.sections) expanded.value[`sec:${ch.no}:${s.no}`] = true
  tocOpen.value[ch.no] = true
  tab.value = 'problems'
  requestAnimationFrame(() => {
    document.getElementById(`chap-${ch.no}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  })
}

function jumpToSec(ch: ListChapter, sec: ListChapter['sections'][number]) {
  expanded.value[`ch:${ch.no}`] = true
  expanded.value[`sec:${ch.no}:${sec.no}`] = true
  tocOpen.value[ch.no] = true
  tab.value = 'problems'
  requestAnimationFrame(() => {
    document.getElementById(`sec-${ch.no}-${sec.no}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  })
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
              <button class="chead" @click="toggle(`ch:${ch.no}`)">
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
              <div v-show="expanded[`ch:${ch.no}`]" class="cbody">
                <!-- 章直挂题 -->
                <el-table v-if="ch.problems.length" :data="ch.problems" class="ptable">
                  <el-table-column label="状态" width="140">
                    <template #default="{ row }">
                      <SubStatus v-if="row.status" :status="row.status" :score="row.score" />
                      <span v-else class="dim">未做</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="题号" width="96">
                    <template #default="{ row }"><span class="mono dim">P{{ row.pid }}</span></template>
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
                        <router-link class="ptitle" :to="`/problems/${row.pid}`"><b>{{ row.title }}</b></router-link>
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
                  <button :id="`sec-${ch.no}-${sec.no}`" class="sbar" @click="toggle(`sec:${ch.no}:${sec.no}`)">
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
                  <div v-show="expanded[`sec:${ch.no}:${sec.no}`]" class="sec-table">
                    <el-table v-if="sec.problems.length" :data="sec.problems" class="ptable">
                      <el-table-column label="状态" width="140">
                        <template #default="{ row }">
                          <SubStatus v-if="row.status" :status="row.status" :score="row.score" />
                          <span v-else class="dim">未做</span>
                        </template>
                      </el-table-column>
                      <el-table-column label="题号" width="96">
                        <template #default="{ row }"><span class="mono dim">P{{ row.pid }}</span></template>
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
                            <router-link class="ptitle" :to="`/problems/${row.pid}`"><b>{{ row.title }}</b></router-link>
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

          <!-- 重要信息：章节目录（吸顶跟随；章/节点击即跳；组可收起展开） -->
          <RailCard ov="目录" title="章节目录" class="aside-card toc-card">
            <div class="toc">
              <div v-for="ch in detail.chapters" :key="ch.no" class="toc-group">
                <div class="toc-chap">
                  <button
                    v-if="ch.sections.length"
                    class="toc-chevron"
                    :class="{ open: tocOpen[ch.no] !== false }"
                    :title="tocOpen[ch.no] !== false ? '收起小节' : '展开小节'"
                    @click="tocOpen[ch.no] = tocOpen[ch.no] === false ? true : false"
                  >
▸
</button>
                  <button class="toc-row" @click="jumpTo(ch)">
                    <span class="cno mono">{{ ch.no }}</span>
                    <span class="toc-title">{{ ch.title }}</span>
                  </button>
                </div>
                <div v-show="tocOpen[ch.no] !== false" class="toc-sub">
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
}

.chapter {
  border: 1px solid var(--soj-line);
  border-radius: 12px;
  background: var(--soj-white);
  overflow: hidden;
}

/* 组头：蓝调渐变底 + 主蓝竖条强调章节名；悬停明显加深 */
.chead {
  width: 100%;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: var(--soj-sp-20) var(--soj-sp-16);
  min-height: 70px;
  background: linear-gradient(90deg, rgb(var(--soj-blue-hi-rgb) / 0.12), rgb(var(--soj-teal-rgb) / 0.06));
  border: none;
  border-left: 3px solid var(--soj-blue);
  cursor: pointer;
  text-align: left;
}

.chead:hover {
  background: linear-gradient(90deg, rgb(var(--soj-blue-hi-rgb) / 0.22), rgb(var(--soj-teal-rgb) / 0.12));
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

.ccount {
  flex: none;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  min-width: 44px;
  text-align: right;
}

/* 组头右侧两字开关（拍板 2：不要超小图标） */
.tg {
  flex: none;
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-blue);
  min-width: 26px;
  text-align: right;
}

.cbody {
  padding: 0 0 var(--soj-sp-2);
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

/* 可点展开三角形（章节/小节通用）：大号明显指示 */
.tri {
  flex: none;
  width: 0;
  height: 0;
  border-left: 8px solid var(--soj-blue);
  border-top: 6px solid transparent;
  border-bottom: 6px solid transparent;
  transition: transform 0.2s;
}

.tri.open {
  transform: rotate(90deg);
}

/* 小节折叠条：行高对齐章节组头原高；独立成条、各自带表 */
.sec-block + .sec-block,
.ptable + .sec-block,
.sec-block + .ptable {
  margin-top: var(--soj-sp-2);
}

.sbar {
  width: 100%;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: var(--soj-sp-12) var(--soj-sp-16);
  min-height: 48px;
  background: rgb(var(--soj-blue-hi-rgb) / 0.06);
  border: none;
  border-left: 3px solid var(--soj-blue);
  cursor: pointer;
  text-align: left;
}

.sbar:hover {
  background: rgb(var(--soj-blue-hi-rgb) / 0.13);
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

.sec-table {
  padding-bottom: var(--soj-sp-2);
}

.sempty,
.cempty {
  margin: 0;
  padding: var(--soj-sp-12) var(--soj-sp-16);
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
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

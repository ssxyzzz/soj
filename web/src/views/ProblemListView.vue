<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { ArrowDown, CircleClose, DArrowLeft, DArrowRight } from '@element-plus/icons-vue'

import { authApi, problemApi, rankApi } from '@/api'
import { downloadFile } from '@/api/download'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import FitTags from '@/components/problem/FitTags.vue'
import SubStatus from '@/components/SubStatus.vue'
import TagFilter from '@/components/problem/TagFilter.vue'
import TierBadge from '@/components/TierBadge.vue'
import { useLevelsStore } from '@/stores/levels'
import { useTagTreeStore } from '@/stores/tagtree'
import { useListQuery } from '@/composables/useListQuery'
import { useUserStore } from '@/stores/user'
import type { ProblemListItem } from '@/types'

const router = useRouter()

// 客观题小类徽章（v0.9）：带「题」字，与详情页题型标签口径一致
function kindBadge(kind?: string) {
  const map: Record<string, string> = { single: '单选题', multiple: '多选题', judge: '判断题', blank: '填空题' }
  return map[kind ?? ''] ?? '客观题'
}

// 题型徽章文案：客观题按小类细分；函数/交互题给固定名；传统题不显示
function typeBadge(row: { type?: string; kind?: string }): string {
  if (row.type === 'objective') return kindBadge(row.kind)
  if (row.type === 'function') return '函数题'
  if (row.type === 'interactive') return '交互题'
  return ''
}
const userStore = useUserStore()
const tagStore = useTagTreeStore()
const levels = useLevelsStore()
levels.ensure() // 难度档名（TierBadge 徽章与难度筛选胶囊展示）
const difficultyNotes = computed(() => {
  const notes = levels.cfg?.difficulty_notes ?? []
  return notes
    .map((n, i) => ({ tier: i + 1, name: levels.difficultyNameOf(i + 1), note: n.trim() }))
    .filter((x) => x.note)
}) // 逐档难度说明（后台难度等级页签编辑；空档不显示）

const list = ref<ProblemListItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 50
// 底部完整翻页的末页页码（EP pagination 无 first/last，首页/末页按钮自拼）
const lastPage = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const loading = ref(false)
const keyword = ref('')
const tagSel = ref<number[]>([])
const diffSel = ref<number[]>([])
// 筛选面板展开（v3-full chips 形态）：难度 / 知识点 / 标签（标签树）三个独立面板
// （2026-10-25：原「高级筛选」更名「标签」，与知识点（知识树）对称）
const advOpen = ref(false)
const tagOpen = ref(false)
const diffOpen = ref(false)
const metaSel = ref<number[]>([])
const activeAdvCount = computed(() => metaSel.value.length)
// 状态筛选（v0.47）：重复题/隐藏题精确过滤，仅「题库全量可见」视角可见（学生端这两类本就不可见）
const canStateFilter = computed(() => userStore.can('problem.view.all'))
const stateOpen = ref(false)
const stateSel = ref<'' | 'duplicate' | 'hidden'>('')
const stateOpts = [
  { v: 'duplicate', label: '重复题' },
  { v: 'hidden', label: '隐藏题' },
] as const

function onStateChange(s: 'duplicate' | 'hidden') {
  stateSel.value = stateSel.value === s ? '' : s // 点同值取消 = 全部
  page.value = 1
  qsync()
  loadDebounced()
}

// ---- 多题打包导出（v0.18.1）：problem.data.download 权限者可见 ----
// 勾选跨翻页/跨筛选保留（存 id 集合）；表格翻页后当前页勾选态由 selectableRows 同步回显
const canExport = computed(() => userStore.can('problem.data.download'))
const selected = ref<Set<number>>(new Set())
const exporting = ref(false)
// 选择模式：点「打包导出」进入（左侧勾选框出现），导出完成或点「完成选择」退出
const picking = ref(false)
// reserve-selection：el-table 跨页保留勾选，selection-change 回调给全量已选行
function onSelChange(rows: ProblemListItem[]) {
  selected.value = new Set(rows.map((r) => r.id))
}
// 导出（v0.64 直链化）：签发短签直链即返回，下载进度交浏览器原生下载器
async function exportSelected() {
  if (!selected.value.size || exporting.value) return
  exporting.value = true
  try {
    const ids = [...selected.value].sort((a, b) => a - b).join(',')
    await downloadFile(`/problems/export?ids=${ids}`, `soj-packages-${selected.value.size}.zip`)
    selected.value = new Set()
    picking.value = false
  } finally {
    exporting.value = false
  }
}

async function load() {
  loading.value = true
  try {
    const my = ++loadSeq
    const r = await problemApi.list(
      page.value,
      pageSize,
      keyword.value.trim() || undefined,
      tagSel.value,
      diffSel.value.length ? diffSel.value : undefined,
      metaSel.value.length ? metaSel.value : undefined,
      stateSel.value || undefined,
    )
    if (my !== loadSeq) return // 已有更新的筛选请求在途，丢弃旧响应（v0.37 竞态保护）
    list.value = r.list
    total.value = r.total
  } finally {
    loading.value = false
  }
}
let loadSeq = 0
// 筛选连点合并（v0.37）：快速勾一组标签/难度 = 每次即发请求；300ms 去抖只发末次，
// 高延迟链路（~100ms/请求）下省一串无效往返
let filterTimer: ReturnType<typeof setTimeout> | undefined
function loadDebounced() {
  if (filterTimer) clearTimeout(filterTimer)
  filterTimer = setTimeout(() => {
    filterTimer = undefined
    load()
  }, 300)
}

function search() {
  page.value = 1
  qsync()
  load()
}

function resetSearch() {
  keyword.value = ''
  tagSel.value = []
  diffSel.value = []
  metaSel.value = []
  stateSel.value = ''
  page.value = 1
  qsync()
  load()
}

function onTagChange(ids: number[]) {
  tagSel.value = ids
  page.value = 1
  qsync()
  loadDebounced()
}

function onDiffChange(ids: number[]) {
  diffSel.value = ids
  page.value = 1
  qsync()
  loadDebounced()
}

// 难度胶囊面板（2026-10-25：el-select 换十档胶囊一字排开）单点切换
function toggleDiff(d: number) {
  onDiffChange(diffSel.value.includes(d) ? diffSel.value.filter((x) => x !== d) : [...diffSel.value, d])
}

// 题表标签列（2026-10-25）：「知识点/标签」列头点击换向——知识点=知识树 tags，标签=标签树 meta_tags
const tagCol = ref<'knowledge' | 'meta'>('knowledge')
function toggleTagCol() {
  tagCol.value = tagCol.value === 'knowledge' ? 'meta' : 'knowledge'
}

function onMetaChange(ids: number[]) {
  metaSel.value = ids
  page.value = 1
  qsync()
  loadDebounced()
}

// 标签（标签树）一键清空（只清这一路，不动关键词/知识点/难度）
function clearAdv() {
  metaSel.value = []
  page.value = 1
  qsync()
  load()
}

// 常用标签 chips：一级节点前 9 个，点击 = 切换该标签筛选（v3 副列「常用标签」卡）
const hotTags = computed(() => tagStore.nodes.slice(0, 9))
function toggleHotTag(id: number) {
  onTagChange(tagSel.value.includes(id) ? tagSel.value.filter((t) => t !== id) : [...tagSel.value, id])
}

// ---- 我的做题统计（v3 副列 kv 四格 + 七天 spark）：/me/problem-stats + 排名榜定位 ----
const myStats = ref<Awaited<ReturnType<typeof authApi.myProblemStats>> | null>(null)
const myRank = ref<number | null>(null)

async function loadMyStats() {
  if (!userStore.token) return
  try {
    const [st, rank] = await Promise.all([authApi.myProblemStats(), rankApi.top()])
    myStats.value = st
    // 后端已按榜单同序算好观察者名次（含榜外），不必前端 find
    myRank.value = rank.me?.rank ?? null
  } catch {
    myStats.value = null
  }
}

// spark 柱高（%）：七天分布对最大值归一，全 0 时给最小可见高度
const sparkHeights = computed(() => {
  const arr = myStats.value?.recent7 ?? []
  const max = Math.max(...arr, 1)
  return arr.map((n) => Math.max(3, Math.round((n / max) * 100)))
})

// 目标地址（v0.39 链接化）：标题列 RouterLink 与整行点击共用同一口径
function rowHref(row: { id?: number; locked?: boolean; lock_kind?: string; contest_id?: number; exam_id?: number }) {
  if (row.locked) {
    return row.lock_kind === 'exam' && row.exam_id ? `/exams/${row.exam_id}` : `/contests/${row.contest_id}`
  }
  return `/problems/${row.id}`
}

function problemRoute(row: Parameters<typeof rowHref>[0]) {
  router.push(rowHref(row))
}

// 通过率 = AC 提交 / 总提交（百分比，整数省小数）
function acceptRate(row: { submit_count?: number; accepted_count?: number }): string {
  if (!row.submit_count) return '—'
  const pct = ((row.accepted_count ?? 0) / row.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
}

function onPageChange(p: number) {
  page.value = p
  qsync()
  load()
}

// ---- URL 即状态（v0.39）：挂载从 query 复原现场，变更 qsync 回写（不 watch 回读） ----
const lq = useListQuery()

function qsync() {
  lq.sync({
    page: page.value > 1 ? page.value : undefined,
    kw: keyword.value.trim() || undefined,
    tag: tagSel.value.length ? tagSel.value.join(',') : undefined,
    diff: diffSel.value.length ? diffSel.value.join(',') : undefined,
    meta: metaSel.value.length ? metaSel.value.join(',') : undefined,
    state: stateSel.value || undefined,
  })
}

onMounted(() => {
  page.value = lq.num('page')
  keyword.value = lq.str('kw') // 含 v0.19 首页 hero ?kw= 联动（同源收敛）
  tagSel.value = lq.ids('tag')
  diffSel.value = lq.ids('diff')
  metaSel.value = lq.ids('meta')
  const st = lq.str('state')
  stateSel.value = st === 'duplicate' || st === 'hidden' ? st : ''
  load()
  loadMyStats()
  tagStore.ensure() // 标签树异步就绪后筛选区自动出现（v-if 响应 store）
})
</script>

<template>
  <PageShell>
      <div class="page-head">
        <div class="kicker">PROBLEM SET</div>
        <h1 class="page-title">
          题库<small>共 {{ total }} 题<template v-if="myStats"> · 你已解决 {{ myStats.solved }} 题</template></small>
        </h1>
      </div>

      <!-- v3 toolbar：胶囊搜索（flex 自适应）+ 筛选 chips（难度/知识点/标签，激活蓝底白字）
           2026-10-25：去掉「高级筛选」名目，第三位改名「标签」（即标签树：地区/赛事/题库/年份/来源） -->
      <div class="toolbar">
        <label class="soj-input search-pill">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
          <input
            v-model="keyword"
            placeholder="搜索标题、题号、知识点或标签"
            @keyup.enter="search"
          />
        </label>
        <el-icon v-if="keyword" class="clear-btn" title="清空" @click="resetSearch"><CircleClose /></el-icon>
        <button
          class="soj-sel"
          :class="{ on: diffOpen || diffSel.length }"
          @click="diffOpen = !diffOpen"
        >
          难度<template v-if="diffSel.length"> · {{ diffSel.length }}</template>
          <el-icon class="sel-arrow" :class="{ open: diffOpen }"><ArrowDown /></el-icon>
        </button>
        <button
          class="soj-sel"
          :class="{ on: tagOpen || tagSel.length }"
          @click="tagOpen = !tagOpen"
        >
          知识点<template v-if="tagSel.length"> · {{ tagSel.length }}</template>
          <el-icon class="sel-arrow" :class="{ open: tagOpen }"><ArrowDown /></el-icon>
        </button>
        <button
          class="soj-sel"
          :class="{ on: advOpen || activeAdvCount }"
          @click="advOpen = !advOpen"
        >
          标签<template v-if="activeAdvCount"> · {{ activeAdvCount }}</template>
          <el-icon class="sel-arrow" :class="{ open: advOpen }"><ArrowDown /></el-icon>
        </button>
        <!-- 状态筛选（v0.47）：重复题/隐藏题精确过滤，仅题库全量可见视角 -->
        <button
          v-if="canStateFilter"
          class="soj-sel"
          :class="{ on: stateOpen || stateSel }"
          @click="stateOpen = !stateOpen"
        >
          状态<template v-if="stateSel"> · {{ stateSel === 'duplicate' ? '重复题' : '隐藏题' }}</template>
          <el-icon class="sel-arrow" :class="{ open: stateOpen }"><ArrowDown /></el-icon>
        </button>
      </div>

      <!-- 状态面板：重复题（灰）/ 隐藏题（红）胶囊单选，点同值取消 -->
      <div v-if="stateOpen && canStateFilter" class="panel-row state-panel">
        <el-tag
          v-for="s in stateOpts"
          :key="s.v"
          class="state-chip"
          :type="s.v === 'duplicate' ? 'info' : 'danger'"
          :effect="stateSel === s.v ? 'dark' : 'plain'"
          @click="onStateChange(s.v)"
        >
          {{ s.label }}
        </el-tag>
        <el-button v-if="stateSel" link type="primary" class="adv-clear" @click="onStateChange(stateSel)">清空</el-button>
      </div>

      <!-- 难度面板：十档胶囊一字排开（即题表「难度」列同款 TierBadge，色随档位），点击多选 -->
      <div v-if="diffOpen" class="panel-row diff-panel">
        <div class="diff-chips">
          <TierBadge
            v-for="d in 10"
            :key="d"
            :tier="d"
            kind="difficulty"
            size="md"
            class="diff-chip"
            :class="{ sel: diffSel.includes(d) }"
            @click="toggleDiff(d)"
          />
        </div>
        <el-button v-if="diffSel.length" link type="primary" class="adv-clear" @click="onDiffChange([])">
          清空
        </el-button>
      </div>
      <!-- 知识点面板（chip 展开） -->
      <div v-if="tagOpen && (tagStore.nodes.length || tagSel.length)" class="panel-row">
        <TagFilter :selected="tagSel" @change="onTagChange" />
      </div>
      <!-- 标签面板（标签树：地区/赛事/题库/年份/来源，2026-10-25 双树；原「高级筛选」） -->
      <div v-show="advOpen" class="panel-row">
        <div class="adv-inline">
          <TagFilter tree="meta" :selected="metaSel" @change="onMetaChange" />
          <el-button v-if="activeAdvCount" link type="primary" class="adv-clear" @click="clearAdv">
            清空
          </el-button>
        </div>
      </div>

      <el-card class="tbl-card">
        <el-table
          stripe
          v-loading="loading"
          :data="list"
          row-key="id"
          style="cursor: pointer"
          @row-click="(row: ProblemListItem) => problemRoute(row)"
          @selection-change="onSelChange"
        >
          <el-table-column v-if="canExport && picking" type="selection" width="48" reserve-selection :selectable="() => true" />
          <el-table-column label="状态" width="130">
            <template #default="{ row }">
              <SubStatus v-if="userStore.token && row.status" :status="row.status" :score="row.my_score" />
              <span v-else-if="userStore.token" class="dim">未做</span>
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="题号" width="76">
            <template #default="{ row }">
              <span class="mono dim">P{{ row.id }}</span>
            </template>
          </el-table-column>
          <el-table-column label="题目" min-width="320">
            <template #default="{ row }">
              <RouterLink :to="rowHref(row)" class="tlink" @click.stop>{{ row.title }}</RouterLink>
              <el-tag v-if="typeBadge(row)" size="small" class="ml8 ptype-tag">
                {{ typeBadge(row) }}
              </el-tag>
              <el-tag v-if="row.hidden" size="small" type="danger" class="ml8">隐藏题</el-tag>
              <el-tag v-if="row.duplicate" size="small" type="info" class="ml8">重复题</el-tag>
              <!-- 客观题没有测试数据是正常形态，不提示「无数据」 -->
              <el-tag v-if="row.has_cases === false && row.type !== 'objective'" size="small" type="warning" class="ml8">无数据</el-tag>
              <el-tag v-if="row.locked" size="small" type="success" class="ml8">
                {{ row.lock_kind === 'exam' ? '考试中' : '比赛中' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="难度" width="104">
            <template #default="{ row }">
              <!-- 赛时隐藏难度（hideMeta）时服务端不下发，渲染横杠而非按 D3 兜底 -->
              <TierBadge v-if="row.difficulty != null" :tier="row.difficulty" kind="difficulty" />
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column min-width="180">
            <template #header>
              <!-- 知识点/标签切换列头（2026-10-25）：蓝色可点击，当前维度高亮，点击整表换向 -->
              <button class="tag-col-toggle" title="点击切换知识点 / 标签" @click="toggleTagCol">
                <span :class="{ cur: tagCol === 'knowledge' }">知识点</span><span class="slash">/</span><span :class="{ cur: tagCol === 'meta' }">标签</span>
              </button>
            </template>
            <template #default="{ row }">
              <FitTags
                v-if="(tagCol === 'knowledge' ? row.tags : row.meta_tags)?.length"
                :ids="tagCol === 'knowledge' ? row.tags : row.meta_tags"
                :tree="tagCol"
              />
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="通过率" width="88">
            <template #default="{ row }">
              <!-- 原生 title（v0.62 挂载冻结根治批）：50 行 × el-tooltip 实例是题库刷新
                   卡的同构成分，一行文案用原生提示零成本承载 -->
              <span
                v-if="row.submit_count"
                class="mono"
                :title="`AC ${row.accepted_count ?? 0} / 提交 ${row.submit_count}`"
              >{{ acceptRate(row) }}</span>
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
        </el-table>
        <!-- 主列底部翻页（v3 pager）：居中、控件齐全——首页/末页按钮自拼
             （EP pagination 的 layout 不支持 first/last，写上会被静默忽略） -->
        <div class="table-pager">
          <el-button
            class="pg-edge"
            :disabled="page === 1"
            title="第一页"
            @click="onPageChange(1)"
          >
            <el-icon><DArrowLeft /></el-icon>
          </el-button>
          <el-pagination
            background
            layout="prev, pager, next"
            :total="total"
            :page-size="pageSize"
            :current-page="page"
            @current-change="onPageChange"
          />
          <el-button
            class="pg-edge"
            :disabled="page === lastPage"
            title="最后一页"
            @click="onPageChange(lastPage)"
          >
            <el-icon><DArrowRight /></el-icon>
          </el-button>
        </div>
        <div class="pager-meta dim">第 {{ page }} / {{ lastPage }} 页 · 共 {{ total }} 题</div>
      </el-card>

    <template #aside>
      <RailCard v-if="userStore.can('problem.create') || userStore.can('problem.import') || canExport" ov="管理" class="aside-card">
        <template #head>
          <div class="rail-ov">管理</div>
          <div class="rail-t">
            <TipLabel
              label="题目管理"
              tip="新增题在表单里写题面、zip 只收数据/配置；题包（含题面）批量导入走「导入题目」。打包导出：勾选跨翻页/筛选保留，导出目录 = 题号_标题，受每周限额约束"
            />
          </div>
        </template>
        <div class="entry-actions">
          <el-button v-if="userStore.can('problem.create')" type="primary" style="width: 100%" @click="router.push('/problems/create')">新增题目</el-button>
          <el-button v-if="userStore.can('problem.import')" style="width: 100%" @click="router.push('/import')">导入题目</el-button>
          <!-- 打包导出（v0.18.1）：点开进入选择模式（左侧勾选框出现），导出或完成退出 -->
          <template v-if="canExport">
            <el-button v-if="!picking" style="width: 100%" @click="picking = true">打包导出</el-button>
            <template v-else>
              <el-button
                type="primary"
                style="width: 100%"
                :loading="exporting"
                :disabled="!selected.size"
                @click="exportSelected"
              >
                {{ exporting ? '正在准备导出…' : `导出已选（${selected.size}）` }}
              </el-button>
              <el-button
                style="width: 100%"
                @click="picking = false; selected = new Set()"
              >
                完成选择
              </el-button>
            </template>
          </template>
        </div>
      </RailCard>

      <!-- v3「我的 · 数据概览」：kv 四格 + 七天 spark（未登录给登录入口） -->
      <RailCard v-if="userStore.token && myStats" ov="我的" title="数据概览" class="aside-card">
        <div class="soj-kv">
          <div class="k">
            <b>{{ myStats.solved }}<small>/{{ myStats.total }}</small></b>
            <span>已解决</span>
          </div>
          <div class="k">
            <b>{{ myRank ? `#${myRank}` : '—' }}</b>
            <span>全站排名</span>
          </div>
          <div class="k">
            <b>{{ myStats.submissions }}</b>
            <span>提交次数</span>
          </div>
          <div class="k">
            <b>{{ myStats.ac_rate.toFixed(0) }}%</b>
            <span>AC 率</span>
          </div>
        </div>
        <div class="spark" title="最近七天提交">
          <i
            v-for="(h, i) in sparkHeights"
            :key="i"
            :class="{ hi: i === sparkHeights.indexOf(Math.max(...sparkHeights)) }"
            :style="{ height: `${h}%` }"
          />
        </div>
        <p class="hint spark-hint">最近七天提交 · 峰值 {{ Math.max(...myStats.recent7, 0) }} 次</p>
      </RailCard>
      <RailCard v-else-if="!userStore.token" ov="我的" title="数据概览" class="aside-card">
        <p class="hint">登录后查看你的做题概览与七天提交分布。</p>
        <el-button type="primary" style="width: 100%" @click="router.push('/login')">去登录</el-button>
      </RailCard>

      <RailCard v-if="hotTags.length" ov="筛选" title="常用知识点" class="aside-card">
        <span
          v-for="t in hotTags"
          :key="t.id"
          class="soj-tagchip tagwrap"
          :class="[{ on: tagSel.includes(t.id) }, `lv-${tagStore.levelOf(t.id)}`]"
          @click="toggleHotTag(t.id)"
        >{{ t.name }}</span>
      </RailCard>

      <RailCard ov="说明" title="难度说明" class="aside-card">
        <p class="hint">难度共十档，锚点对齐 NOI 大纲与教学进度。</p>
        <!-- 逐档难度说明（2026-10-26）：徽章 + 文案一行一档；后台「难度等级」页签逐档编辑，随 levels 配置全局同步；空档不显示 -->
        <div v-for="x in difficultyNotes" :key="x.name" class="diff-note">
          <TierBadge :tier="x.tier" kind="difficulty" />
          <span class="hint">{{ x.note }}</span>
        </div>
        <!-- 关于题库（2026-10-26 挪到难度说明之后；卡内二级标题字体同 .rail-t） -->
        <div class="about-t">关于题库</div>
        <p class="hint">隐藏题目对普通选手不可见，不计入榜单与排名。</p>
      </RailCard>
    </template>
  </PageShell>
</template>

<style scoped>
/* 打包导出选择模式的勾选框放大（验收反馈：默认太小）——矢量绘制，scale 不糊；
   行勾选与表头全选一并放大 */
:deep(.el-table-column--selection .el-checkbox) {
  transform: scale(1.35);
}

/* toolbar 形态收编全局 .toolbar（flex/wrap/gap/mb），本地只留增量 */

.search-pill {
  flex: 1;
  min-width: 220px;
}

.clear-btn {
  cursor: pointer;
  color: var(--soj-ink-3);
  flex: none;
}

.clear-btn:hover {
  color: var(--soj-blue);
}

.sel-arrow {
  font-size: var(--soj-fs-12);
  transition: transform 0.2s;
}

.sel-arrow.open {
  transform: rotate(180deg);
}

/* chip 展开面板（难度胶囊行 / 知识点卡片流 / 标签卡片流） */
.panel-row {
  margin-bottom: var(--soj-sp-16);
  padding: var(--soj-sp-12) var(--soj-sp-14);
  background: var(--el-fill-color-light);
  border-radius: 14px;
}

/* 题表「知识点/标签」切换列头：蓝色可点击，当前维度不透明 + 下划线，另一维度淡化 */
.tag-col-toggle {
  display: inline-flex;
  align-items: center;
  padding: 0;
  border: 0;
  background: none;
  font: inherit;
  font-weight: var(--soj-fw-bold);
  color: var(--soj-blue);
  cursor: pointer;
  user-select: none;
}

.tag-col-toggle .slash {
  padding: 0 var(--soj-sp-2);
  opacity: 0.45;
}

.tag-col-toggle span:not(.slash) {
  opacity: 0.5;
  transition: opacity 0.15s;
}

.tag-col-toggle span.cur {
  opacity: 1;
  text-decoration: underline;
  text-underline-offset: 3px;
  text-decoration-thickness: 1.5px;
}

.tag-col-toggle:hover span:not(.slash) {
  opacity: 0.85;
}

.tag-col-toggle:hover span.cur {
  opacity: 1;
}

.diff-panel {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-4) var(--soj-sp-12);
  width: fit-content;
  max-width: 100%;
}

/* 难度胶囊一字排开（TierBadge 同款胶囊，色随档位）：选中 = 底色加深 + 档位色实线描边，
   未选中即默认浅胶囊；窄屏自然折行 */
.diff-chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--soj-sp-8);
}

.diff-chip {
  cursor: pointer;
  transition:
    transform 0.15s,
    box-shadow 0.15s,
    background-color 0.15s,
    border-color 0.15s;
}

.diff-chip:hover {
  transform: translateY(-1px);
}

.diff-chip.sel.diff-tag {
  background: color-mix(in srgb, var(--dc) 20%, var(--soj-white));
  border-color: var(--dc);
  box-shadow: inset 0 0 0 1px var(--dc);
}

/* 状态面板（v0.47）：重复题/隐藏题胶囊单选，同难度面板的点击手感 */
.state-panel {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
}

.state-chip {
  cursor: pointer;
}

/* 标签面板一字排开：标签等宽、控件统一宽；窄屏自然折行 */
.adv-inline {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8) var(--soj-sp-10);
  flex-wrap: wrap;
}

.adv-label {
  flex: none;
  font-size: var(--soj-fs-14);
  color: var(--soj-ink-2);
}

.adv-select {
  width: 200px;
}

.adv-select-year {
  width: 150px;
}

/* 一键清空钉在行尾 */
.adv-clear {
  margin-left: auto;
}

.table-pager {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-top: var(--soj-sp-16);
  padding-bottom: var(--soj-sp-2);
}

/* 自拼的首页/末页按钮：对齐 EP 分页按钮的盒子——32px 圆角方块（radius 10px 同
   main.css 分页口径）、无边框同底色；默认 el-button 的边框 + 4px 圆角 + 8px 内距
   会撑成椭圆，与中间页钮形状不一致（2026-10-26 拍板） */
.pg-edge {
  width: 32px;
  height: 32px;
  padding: 0;
  border: none;
  background: var(--el-fill-color-light);
  border-radius: 10px;
}

.pg-edge:hover {
  background: var(--el-fill-color);
  color: var(--soj-blue);
}

.pg-edge:disabled {
  background: var(--el-fill-color-light);
  color: var(--el-text-color-placeholder);
}

.pager-meta {
  text-align: center;
  margin-top: var(--soj-sp-6);
}

/* v3 .hint：副列卡说明文字 */
.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: var(--soj-sp-10) 0 0;
}

/* 难度说明逐档行：徽章 + 文案基线对齐（徽章随首行，文案折行不拽偏徽章） */
.diff-note {
  display: flex;
  align-items: baseline;
  gap: var(--soj-sp-8);
  margin-top: var(--soj-sp-10);
}

.diff-note .hint {
  margin: 0;
}

/* 卡内二级标题「关于题库」：字体同卡片标题 .rail-t（fs-body/bold 藏蓝） */
.about-t {
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  margin-top: var(--soj-sp-16);
}

/* v3 .spark：七天提交柱状（18% 主蓝柱，峰值格渐变高亮） */
.spark {
  display: flex;
  align-items: flex-end;
  gap: var(--soj-sp-6);
  height: 44px;
  margin-top: var(--soj-sp-14);
}

.spark i {
  flex: 1;
  border-radius: 4px 4px 2px 2px;
  background: rgb(var(--soj-blue-rgb) / 0.18);
  min-height: 3px;
  transition: height 0.3s ease;
}

.spark i.hi {
  background: linear-gradient(180deg, var(--soj-blue-hi), var(--soj-blue-lo));
}

.spark-hint {
  margin-top: var(--soj-sp-6);
}

/* 常用标签 chips 换行间距（v3 tagchip margin sp-2 sp-4 sp-2 0） */
/* 常用标签：五级层级色与题库列表统一（修复⑰，--lv 由全局 .lv-N 注入）；
   .on 选中态沿用筛选 chips 口径（主蓝底白字），优先级压过层级色 */
.tagwrap {
  margin: var(--soj-sp-2) var(--soj-sp-4) var(--soj-sp-2) 0;
  color: var(--lv);
  background: var(--lv-bg);
  border-color: var(--lv-border);
}

.tagwrap:hover {
  color: var(--lv);
  background: var(--lv-bg);
  border-color: var(--lv);
}

.tagwrap.on {
  color: var(--soj-white);
  background: var(--soj-blue);
  border-color: var(--soj-blue);
}
</style>

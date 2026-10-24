<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowDown, CircleClose, DArrowLeft, DArrowRight } from '@element-plus/icons-vue'

import { authApi, problemApi, rankApi, sourcesApi } from '@/api'
import { downloadFile } from '@/api/download'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import FitTags from '@/components/problem/FitTags.vue'
import SubStatus from '@/components/SubStatus.vue'
import TagFilter from '@/components/problem/TagFilter.vue'
import TierBadge from '@/components/TierBadge.vue'
import { useLevelsStore } from '@/stores/levels'
import { useTagTreeStore } from '@/stores/tagtree'
import { useUserStore } from '@/stores/user'
import { SOURCE_TYPE_LABELS } from '@/utils/source'
import type { ProblemListItem } from '@/types'

const router = useRouter()
const route = useRoute()

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
levels.ensure() // 难度档名（筛选下拉与徽章展示）

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
// 筛选面板展开（v3-full chips 形态）：标签 / 难度 两个独立 chip 面板 + 高级筛选（来源/年份）
const advOpen = ref(false)
const tagOpen = ref(false)
const diffOpen = ref(false)
const srcTypeSel = ref<string[]>([])
const yearSel = ref<number[]>([])
const sourceYears = ref<number[]>([])
const activeAdvCount = computed(() => srcTypeSel.value.length + yearSel.value.length)

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
    const r = await problemApi.list(
      page.value,
      pageSize,
      keyword.value.trim() || undefined,
      tagSel.value,
      diffSel.value.length ? diffSel.value : undefined,
      srcTypeSel.value.length ? srcTypeSel.value : undefined,
      yearSel.value.length ? yearSel.value : undefined,
    )
    list.value = r.list
    total.value = r.total
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

function resetSearch() {
  keyword.value = ''
  tagSel.value = []
  diffSel.value = []
  srcTypeSel.value = []
  yearSel.value = []
  page.value = 1
  load()
}

function onTagChange(ids: number[]) {
  tagSel.value = ids
  page.value = 1
  load()
}

function onDiffChange(ids: number[]) {
  diffSel.value = ids
  page.value = 1
  load()
}

function onSrcTypeChange(ts: string[]) {
  srcTypeSel.value = ts
  page.value = 1
  load()
}

function onYearChange(ys: (number | string)[]) {
  // allow-create 手输的是字符串：归一为整数年份，非法输入（NaN/超界）丢弃、去重
  const norm = [...new Set(ys.map((v) => Number(v)).filter((v) => Number.isInteger(v) && v >= 1900 && v <= 2100))]
  yearSel.value = norm
  page.value = 1
  load()
}

// 高级筛选一键清空（只清来源/年份两路，不动关键词/标签/难度）
function clearAdv() {
  srcTypeSel.value = []
  yearSel.value = []
  page.value = 1
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
    const me = await authApi.me()
    myRank.value = rank.find((r) => r.user_id === me.id)?.rank ?? null
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

function problemRoute(row: { id?: number; locked?: boolean; lock_kind?: string; contest_id?: number; exam_id?: number }) {
  if (row.locked) {
    router.push(row.lock_kind === 'exam' && row.exam_id ? `/exams/${row.exam_id}` : `/contests/${row.contest_id}`)
    return
  }
  router.push(`/problems/${row.id}`)
}

// 通过率 = AC 提交 / 总提交（百分比，整数省小数）
function acceptRate(row: { submit_count?: number; accepted_count?: number }): string {
  if (!row.submit_count) return '—'
  const pct = ((row.accepted_count ?? 0) / row.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
}

function onPageChange(p: number) {
  page.value = p
  load()
}

onMounted(() => {
  // v0.19 首页 hero 搜索联动：?kw= 直达关键词搜索结果
  const kw = String(route.query.kw ?? '').trim()
  if (kw) {
    keyword.value = kw
    search()
  }
  load()
  loadMyStats()
  tagStore.ensure() // 标签树异步就绪后筛选区自动出现（v-if 响应 store）
  // 来源年份选项（v0.12 高级筛选）：加载失败静默（筛选仍可用难度/类型）
  sourcesApi
    .meta()
    .then((m) => (sourceYears.value = m.years))
    .catch(() => {})
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

      <!-- v3 toolbar：胶囊搜索（flex 自适应）+ 筛选 chips（标签/难度/高级筛选，激活蓝底白字） -->
      <div class="toolbar">
        <label class="soj-input search-pill">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
          <input
            v-model="keyword"
            placeholder="搜索标题关键词或题号"
            @keyup.enter="search"
          />
        </label>
        <el-icon v-if="keyword" class="clear-btn" title="清空" @click="resetSearch"><CircleClose /></el-icon>
        <button
          class="soj-sel"
          :class="{ on: tagOpen || tagSel.length }"
          @click="tagOpen = !tagOpen"
        >
          标签<template v-if="tagSel.length"> · {{ tagSel.length }}</template>
          <el-icon class="sel-arrow" :class="{ open: tagOpen }"><ArrowDown /></el-icon>
        </button>
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
          :class="{ on: advOpen || activeAdvCount }"
          @click="advOpen = !advOpen"
        >
          高级筛选<template v-if="activeAdvCount"> · {{ activeAdvCount }}</template>
          <el-icon class="sel-arrow" :class="{ open: advOpen }"><ArrowDown /></el-icon>
        </button>
      </div>

      <!-- 标签面板（chip 展开） -->
      <div v-if="tagOpen && (tagStore.nodes.length || tagSel.length)" class="panel-row">
        <TagFilter :selected="tagSel" @change="onTagChange" />
      </div>
      <!-- 难度面板（chip 展开；十档多选） -->
      <div v-if="diffOpen" class="panel-row diff-panel">
        <el-select
          v-model="diffSel"
          multiple
          collapse-tags
          placeholder="全部档位"
          clearable
          style="width: 320px"
          @change="onDiffChange"
        >
          <el-option v-for="d in 10" :key="d" :value="d" :label="levels.difficultyNameOf(d)" />
        </el-select>
      </div>
      <!-- 高级筛选面板（来源类型/年份） -->
      <div v-show="advOpen" class="panel-row">
        <div class="adv-inline">
          <span class="adv-label">来源类型</span>
          <el-select
            v-model="srcTypeSel"
            multiple
            collapse-tags
            placeholder="全部"
            clearable
            class="adv-select"
            @change="onSrcTypeChange"
          >
            <el-option
              v-for="(label, value) in SOURCE_TYPE_LABELS"
              :key="value"
              :value="value"
              :label="label"
            />
          </el-select>
          <span class="adv-label">年份</span>
          <el-select
            v-model="yearSel"
            multiple
            collapse-tags
            placeholder="全部"
            clearable
            filterable
            allow-create
            default-first-option
            class="adv-select adv-select-year"
            @change="onYearChange"
          >
            <el-option v-for="y in sourceYears" :key="y" :value="y" :label="String(y)" />
          </el-select>
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
          <el-table-column label="题号" width="84">
            <template #default="{ row }">
              <span class="mono dim">P{{ row.id }}</span>
            </template>
          </el-table-column>
          <el-table-column label="题目" min-width="320">
            <template #default="{ row }">
              <span class="tlink">{{ row.title }}</span>
              <el-tag v-if="typeBadge(row)" size="small" class="ml8 ptype-tag">
                {{ typeBadge(row) }}
              </el-tag>
              <el-tag v-if="row.hidden" size="small" type="danger" class="ml8">隐藏题</el-tag>
              <!-- 客观题没有测试数据是正常形态，不提示「无数据」 -->
              <el-tag v-if="row.has_cases === false && row.type !== 'objective'" size="small" type="warning" class="ml8">无数据</el-tag>
              <el-tag v-if="row.locked" size="small" type="success" class="ml8">
                {{ row.lock_kind === 'exam' ? '考试中' : '比赛中' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="难度" width="90">
            <template #default="{ row }">
              <TierBadge :tier="row.difficulty ?? 3" kind="difficulty" />
            </template>
          </el-table-column>
          <el-table-column label="标签" min-width="180">
            <template #default="{ row }">
              <FitTags v-if="row.tags?.length" :ids="row.tags" />
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="通过率" width="96">
            <template #default="{ row }">
              <el-tooltip
                v-if="row.submit_count"
                :content="`AC ${row.accepted_count ?? 0} / 提交 ${row.submit_count}`"
                placement="top"
              >
                <span class="mono">{{ acceptRate(row) }}</span>
              </el-tooltip>
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column v-if="userStore.token" label="我的状态" width="110">
            <template #default="{ row }">
              <SubStatus v-if="row.status" :status="row.status" :score="row.my_score" />
              <span v-else class="dim">未做</span>
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
      <RailCard v-if="userStore.can('problem.create') || canExport" ov="管理" title="题目管理" class="aside-card">
        <div class="entry-actions">
          <el-button v-if="userStore.can('problem.create')" type="primary" style="width: 100%" @click="router.push('/problems/create')">新增题目</el-button>
          <el-button v-if="userStore.can('problem.create')" style="width: 100%" @click="router.push('/import')">导入题目</el-button>
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
                导出已选（{{ selected.size }}）
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
        <p class="hint">
          <template v-if="picking">勾选跨翻页/筛选保留；导出目录 = 题号_标题，受每日限额约束</template>
          <template v-else>新增题在表单里写题面、zip 只收数据/配置；题包（含题面）批量导入走「导入题目」。</template>
        </p>
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

      <RailCard v-if="hotTags.length" ov="筛选" title="常用标签" class="aside-card">
        <span
          v-for="t in hotTags"
          :key="t.id"
          class="soj-tagchip tagwrap"
          :class="{ on: tagSel.includes(t.id) }"
          @click="toggleHotTag(t.id)"
        >{{ t.name }}</span>
      </RailCard>

      <RailCard ov="说明" title="关于题库" class="aside-card">
        <p class="hint">难度共十档，锚点对齐 NOI 大纲与教学进度；隐藏题目对普通选手不可见，不计入榜单与排名。</p>
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

/* v3 toolbar：搜索胶囊撑满剩余宽 + chips 一排 */
.toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

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

/* chip 展开面板（标签卡片流 / 难度下拉 / 高级筛选行） */
.panel-row {
  margin-bottom: 16px;
  padding: 12px 14px;
  background: var(--el-fill-color-light);
  border-radius: 14px;
}

.diff-panel {
  width: fit-content;
}

/* 高级筛选一字排开：标签等宽、控件统一宽；窄屏自然折行 */
.adv-inline {
  display: flex;
  align-items: center;
  gap: 8px 10px;
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
  gap: 8px;
  margin-top: 16px;
  padding-bottom: 2px;
}

/* 自拼的首页/末页按钮：观感对齐 EP 分页的 background 按钮 */
.pg-edge {
  padding: 8px;
}

.pager-meta {
  text-align: center;
  margin-top: 6px;
}

/* v3 .hint：副列卡说明文字 */
.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: 10px 0 0;
}

/* v3 .spark：七天提交柱状（18% 主蓝柱，峰值格渐变高亮） */
.spark {
  display: flex;
  align-items: flex-end;
  gap: 6px;
  height: 44px;
  margin-top: 14px;
}

.spark i {
  flex: 1;
  border-radius: 4px 4px 2px 2px;
  background: rgba(36, 85, 218, 0.18);
  min-height: 3px;
  transition: height 0.3s ease;
}

.spark i.hi {
  background: linear-gradient(180deg, var(--soj-blue-hi), var(--soj-blue-lo));
}

.spark-hint {
  margin-top: 6px;
}

/* 常用标签 chips 换行间距（v3 tagchip margin 2px 4px 2px 0） */
.tagwrap {
  margin: 2px 4px 2px 0;
}
</style>

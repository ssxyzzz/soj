<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowDown, DArrowLeft, DArrowRight } from '@element-plus/icons-vue'

import { authApi, problemApi, rankApi, sourcesApi } from '@/api'
import { downloadFile } from '@/api/download'
import PageShell from '@/components/PageShell.vue'
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
// 筛选折叠（v0.12 高级筛选；标签 2026-10-23 同款化）：入口一行、点击展开对应面板
const advOpen = ref(false)
const tagOpen = ref(false)
const srcTypeSel = ref<string[]>([])
const yearSel = ref<number[]>([])
const sourceYears = ref<number[]>([])
const activeAdvCount = computed(
  () => diffSel.value.length + srcTypeSel.value.length + yearSel.value.length,
)

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

const solvedCount = ref<number | null>(null)
const solvedRank = ref<number | null>(null)

async function loadOverview() {
  if (!userStore.token) return
  try {
    const [me, rank] = await Promise.all([authApi.me(), rankApi.top()])
    const row = rank.find((r) => r.user_id === me.id)
    solvedCount.value = row?.solved ?? 0
    solvedRank.value = row?.rank ?? null
  } catch {
    solvedCount.value = null
  }
}

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
  load()
  loadOverview()
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
      <el-card>
        <div class="search-row">
          <el-input
            v-model="keyword"
            placeholder="搜索题目（标题关键词或题号）"
            clearable
            style="width: 320px"
            @keyup.enter="search"
            @clear="resetSearch"
          />
          <el-button type="primary" @click="search">搜索</el-button>
          <el-button v-if="keyword" @click="resetSearch">重置</el-button>
          <el-pagination
            class="search-pager"
            background
            layout="slot, prev, pager, next"
            :total="total"
            :page-size="pageSize"
            :current-page="page"
            @current-change="onPageChange"
          >
            <!-- 计数单位用「题」（EP locale 的「共 N 条」是全局的，波及其他列表，这里走插槽局部改） -->
            <span class="dim">共 {{ total }} 题</span>
          </el-pagination>
        </div>
        <!-- 筛选入口行：标签 / 高级筛选两个折叠开关同行，点击展开对应面板、再点收起 -->
        <div class="filter-row">
          <div
            v-if="tagStore.nodes.length || tagSel.length"
            class="adv-toggle"
            @click="tagOpen = !tagOpen"
          >
            <el-icon class="adv-arrow" :class="{ open: tagOpen }"><ArrowDown /></el-icon>
            标签
            <span v-if="!tagOpen && tagSel.length" class="adv-count">{{ tagSel.length }}</span>
          </div>
          <div class="adv-toggle" @click="advOpen = !advOpen">
            <el-icon class="adv-arrow" :class="{ open: advOpen }"><ArrowDown /></el-icon>
            高级筛选
            <span v-if="!advOpen && activeAdvCount" class="adv-count">{{ activeAdvCount }}</span>
          </div>
        </div>
        <div v-if="tagOpen && (tagStore.nodes.length || tagSel.length)" class="tag-row">
          <TagFilter :selected="tagSel" @change="onTagChange" />
        </div>
        <div v-show="advOpen" class="adv-panel">
          <!-- 三路筛选一字排开（2026-10-23 验收调整）；年份可输入（候选外年份允许手输） -->
          <div class="adv-inline">
            <span class="dim diff-label">难度</span>
            <el-select
              v-model="diffSel"
              multiple
              collapse-tags
              placeholder="全部"
              clearable
              style="width: 200px"
              @change="onDiffChange"
            >
              <el-option v-for="d in 10" :key="d" :value="d" :label="levels.difficultyNameOf(d)" />
            </el-select>
            <span class="dim diff-label">来源类型</span>
            <el-select
              v-model="srcTypeSel"
              multiple
              collapse-tags
              placeholder="全部"
              clearable
              style="width: 200px"
              @change="onSrcTypeChange"
            >
              <el-option
                v-for="(label, value) in SOURCE_TYPE_LABELS"
                :key="value"
                :value="value"
                :label="label"
              />
            </el-select>
            <span class="dim diff-label">年份</span>
            <el-select
              v-model="yearSel"
              multiple
              collapse-tags
              placeholder="全部"
              clearable
              filterable
              allow-create
              default-first-option
              style="width: 160px"
              @change="onYearChange"
            >
              <el-option v-for="y in sourceYears" :key="y" :value="y" :label="String(y)" />
            </el-select>
          </div>
        </div>
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
          <el-table-column prop="id" label="ID" width="100" />
          <el-table-column v-if="userStore.token" label="状态" width="150">
            <template #default="{ row }">
              <SubStatus v-if="row.status" :status="row.status" :score="row.my_score" />
              <span v-else class="dim">未做</span>
            </template>
          </el-table-column>
          <el-table-column label="题目" min-width="320">
            <template #default="{ row }">
              <span class="title">{{ row.title }}</span>
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
          <el-table-column label="难度" width="76">
            <template #default="{ row }">
              <TierBadge :tier="row.difficulty ?? 3" kind="difficulty" />
            </template>
          </el-table-column>
          <el-table-column label="标签" min-width="170">
            <template #default="{ row }">
              <FitTags v-if="row.tags?.length" :ids="row.tags" />
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="通过率" width="110">
            <template #default="{ row }">
              <el-tooltip
                v-if="row.submit_count"
                :content="`AC ${row.accepted_count ?? 0} / 提交 ${row.submit_count}`"
                placement="top"
              >
                <span>{{ acceptRate(row) }}</span>
              </el-tooltip>
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
        </el-table>
        <!-- 主列底部额外翻页（2026-10-23 验收）：居中、控件齐全——首页/末页按钮自拼
             （EP pagination 的 layout 不支持 first/last，写上会被静默忽略）；搜索行分页保留 -->
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
        <!-- 计数行（2026-10-23 验收）：翻页条下方居中「当前在 x 页 / 共 xx 页 / 共 xx 题」 -->
        <div class="pager-meta dim">当前在 {{ page }} 页 / 共 {{ lastPage }} 页 / 共 {{ total }} 题</div>
      </el-card>

    <template #aside>
      <el-card v-if="userStore.can('problem.create') || canExport" class="aside-card">
        <template #header><span>题目管理</span></template>
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
        <div class="dim tip">
          <template v-if="picking">勾选跨翻页/筛选保留；导出目录 = 题号_标题，与单题下载同格式，受每日限额约束</template>
          <template v-else>新增题在表单里写题面、zip 只收数据/配置；题包（含题面）批量导入走「导入题目」</template>
        </div>
      </el-card>

      <el-card class="aside-card">
        <template #header><span>我的概览</span></template>
        <template v-if="userStore.token">
          <div class="stat-row">
            <div class="stat">
              <div class="stat-big">{{ solvedCount ?? '—' }}</div>
              <div class="dim">已解决题数</div>
            </div>
            <div class="stat">
              <div class="stat-big">{{ solvedRank ?? '—' }}</div>
              <div class="dim">当前排名</div>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="dim">登录后查看你的做题概览</div>
          <el-button type="primary" plain style="width: 100%; margin-top: 8px" @click="router.push('/login')">
            去登录
          </el-button>
        </template>
      </el-card>
    </template>
  </PageShell>
</template>

<style scoped>
/* 打包导出选择模式的勾选框放大（验收反馈：默认太小）——矢量绘制，scale 不糊；
   行勾选与表头全选一并放大 */
:deep(.el-table-column--selection .el-checkbox) {
  transform: scale(1.35);
}

.search-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.search-pager {
  margin-left: auto;
}

.table-pager {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 8px;
  margin-top: 16px;
}

/* 自拼的首页/末页按钮：观感对齐 EP 分页的 background 按钮 */
.pg-edge {
  padding: 8px;
}

.pager-meta {
  text-align: center;
  margin-top: 6px;
}

.tag-row {
  display: flex;
  align-items: flex-start;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: var(--el-border-radius-base);
  margin-bottom: 12px;
}

/* 筛选入口行（2026-10-23）：标签 / 高级筛选两个折叠开关并排 */
.filter-row {
  display: flex;
  align-items: center;
  gap: 20px;
  margin-bottom: 12px;
}

/* 折叠开关（v0.12 高级筛选同款）：文字开关 + 展开面板；难度行样式沿用 */
.adv-toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--el-color-primary);
  cursor: pointer;
  font-size: var(--soj-fs-body);
  user-select: none;
}

.adv-toggle:hover {
  text-decoration: underline;
}

.adv-arrow {
  transition: transform 0.2s;
}

.adv-arrow.open {
  transform: rotate(180deg);
}

/* 收起时的已选条件数角标 */
.adv-count {
  min-width: 18px;
  height: 18px;
  line-height: 18px;
  padding: 0 5px;
  border-radius: 9px;
  background: var(--el-color-primary);
  color: var(--el-color-white);
  font-size: var(--soj-fs-small);
  text-align: center;
}

.adv-panel {
  padding: 10px 12px;
  background: var(--el-fill-color-light);
  border-radius: var(--el-border-radius-base);
  margin-bottom: 12px;
}

/* 难度筛选行（v0.11）：与标签行同节奏 */
.diff-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.adv-panel .diff-row:last-child {
  margin-bottom: 2px;
}

/* 高级筛选一字排开（2026-10-23）：难度/来源/年份同行，窄屏自然折行 */
.adv-inline {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.diff-label {
  font-size: var(--soj-fs-body);
}

/* 实体名（题名）主色，整行可点击 */
.title {
  color: var(--el-color-primary);
}

.tip {
  margin-top: 8px;
}

/* 统计大数字（32/600，强调档） */
/* 两个统计（已解决题数/当前排名）并排一行，数字同规格 */
.stat-row {
  display: flex;
  align-items: flex-end;
  gap: 40px;
}

.stat-big {
  font-size: var(--soj-fs-stat);
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-color-primary);
  line-height: 1.2;
}
</style>

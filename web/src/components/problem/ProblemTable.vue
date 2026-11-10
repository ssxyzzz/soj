<script setup lang="ts">
// 统一题目表格（v0.89 统一批②）：题库表为超集蓝本，题单/作业/比赛/考试做减法配置，
// 消灭四处各写各的 el-table（列宽三种口径、难度空值双口径、通过率 tooltip/原生 title 两套、
// acceptRate 三份逐字重复）。场景差异走 props/slot 收敛：
//   - 题号形态 numbering：P1000（题库/题单）| T1 序号（比赛/作业）
//   - 锁题 lockedFrozen：题单锁题行冻结纯文本（题库锁题行照常可点进赛事）
//   - 赛时遮罩：行级 row.result_hidden（比赛/作业数据自带）——状态列「已提交」/—、
//     标签与通过率列 —，口径比原作业多遮了比赛知识点列（该字段 result_hidden 时本就不下发，行为不变）
//   - 打包导出 selection / 收藏星 favorite：仅题库
// 口径统一（以题库为准）：难度空值横杠（作业 || 3 兜底退役）、通过率原生 title（v0.62
// 挂载冻结口径，el-tooltip 退役）、TierBadge 默认尺寸（题单 size=sm 退役）、状态列游客「—」。
import { RouterLink } from 'vue-router'
import { Star } from 'lucide-vue-next'

import FitTags from '@/components/problem/FitTags.vue'
import SubStatus from '@/components/SubStatus.vue'
import TierBadge from '@/components/TierBadge.vue'
import { useUserStore } from '@/stores/user'

// 统一行类型：四场景条目（ProblemListItem / ListProblemEntry / ContestProblem /
// AssignmentProblem）的结构超集，字段全可缺省——各接口只下发自己有的
export interface ProblemTableRow {
  id: number
  title: string
  contest_id?: number
  exam_id?: number
  status?: string
  my_score?: number
  submitted?: boolean
  result_hidden?: boolean
  difficulty?: number | null
  tags?: number[]
  meta_tags?: number[]
  favorite?: boolean
  hidden?: boolean
  duplicate?: boolean
  locked?: boolean
  lock_kind?: string
  type?: string
  kind?: string
  has_cases?: boolean
  submit_count?: number
  accepted_count?: number
}

const props = withDefaults(
  defineProps<{
    rows: ProblemTableRow[]
    /** 题号形态：P = P1000（含收藏星位）；T = T1/T2 场内序号 */
    numbering?: 'P' | 'T'
    /** 状态列整列开关（比赛/作业游客隐藏）；游客见状态列时显示「—」 */
    showStatus?: boolean
    /** 通过率列开关（题单无此列） */
    showRate?: boolean
    /** 打包导出勾选列（仅题库） */
    selection?: boolean
    /** 收藏星开关（仅题库；渲染仍要求登录） */
    favorite?: boolean
    /** 知识点/标签双树切换列头（仅题库） */
    tagSwitch?: boolean
    /** 标签树维度：题库双树切换由页面传当前值并挂 toggle-tag-tree */
    tagTree?: 'knowledge' | 'meta'
    /** 行 hover 手型 + 整行点击（emit row-click） */
    clickable?: boolean
    /** 表格 loading 遮罩 */
    loading?: boolean
    /** 锁题行冻结纯文本不可点（题单语义；题库锁题行照常引导进赛事） */
    lockedFrozen?: boolean
    /** 题目链接目标（不传 = 纯文本标题，点击语义走 title-click / row-click） */
    titleTo?: (row: ProblemTableRow) => string
  }>(),
  {
    numbering: 'P',
    showStatus: true,
    showRate: true,
    selection: false,
    favorite: false,
    tagSwitch: false,
    tagTree: 'knowledge',
    clickable: false,
    loading: false,
    lockedFrozen: false,
    titleTo: undefined,
  },
)

const emit = defineEmits<{
  (e: 'row-click', row: ProblemTableRow): void
  (e: 'title-click', row: ProblemTableRow): void
  (e: 'selection-change', rows: ProblemTableRow[]): void
  (e: 'toggle-favorite', row: ProblemTableRow): void
  (e: 'toggle-tag-tree'): void
}>()

const userStore = useUserStore()

// 客观题小类徽章：带「题」字，与详情页题型标签口径一致
function kindBadge(kind?: string) {
  const map: Record<string, string> = { single: '单选题', multiple: '多选题', judge: '判断题', blank: '填空题' }
  return map[kind ?? ''] ?? '客观题'
}

// 题型徽章文案：客观题按小类细分；函数/交互题给固定名；传统题不显示
function typeBadge(row: Pick<ProblemTableRow, 'type' | 'kind'>): string {
  if (row.type === 'objective') return kindBadge(row.kind)
  if (row.type === 'function') return '函数题'
  if (row.type === 'interactive') return '交互题'
  return ''
}

function acceptRate(row: { submit_count?: number; accepted_count?: number }): string {
  if (!row.submit_count) return '—'
  const pct = ((row.accepted_count ?? 0) / row.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
}

function onRowClick(row: ProblemTableRow) {
  // 题单锁题行冻结（lockedFrozen）：整行不可点（与标题冻结同语义）
  if (props.clickable && !(props.lockedFrozen && row.locked)) emit('row-click', row)
}

// 锁定行光标复原（lockedFrozen 场景整表 pointer，冻结行退回 default）
function rowClassName({ row }: { row: ProblemTableRow }) {
  return props.lockedFrozen && row.locked ? 'locked-row' : ''
}
</script>

<template>
  <el-table
    stripe
    v-loading="loading"
    :data="rows"
    row-key="id"
    :style="clickable ? 'cursor: pointer' : undefined"
    :row-class-name="rowClassName"
    @row-click="(row: ProblemTableRow) => onRowClick(row)"
    @selection-change="(rows: ProblemTableRow[]) => emit('selection-change', rows)"
  >
    <el-table-column v-if="selection" type="selection" width="48" reserve-selection :selectable="() => true" />
    <el-table-column v-if="showStatus" label="状态" width="140">
      <template #default="{ row }">
        <template v-if="row.result_hidden">
          <el-tag v-if="row.submitted" size="small" type="info">已提交</el-tag>
          <span v-else class="dim">—</span>
        </template>
        <SubStatus v-else-if="userStore.token && row.status" :status="row.status" :score="row.my_score" />
        <span v-else-if="userStore.token" class="dim">未做</span>
        <span v-else class="dim">—</span>
      </template>
    </el-table-column>
    <el-table-column label="题号" width="96">
      <template #default="{ row, $index }">
        <!-- 收藏星标（v0.86；v0.89 铺开题单/作业）：仅登录视角渲染（游客列内纯题号），
             P/T 两种题号形态都在星后；点击拦截冒泡防触发整行跳转 -->
        <button
          v-if="favorite && userStore.token"
          class="fav-btn"
          :class="{ on: row.favorite }"
          :title="row.favorite ? '取消收藏' : '收藏'"
          @click.stop="emit('toggle-favorite', row as ProblemTableRow)"
        >
          <Star :size="14" :stroke-width="1.8" />
        </button>
        <span v-if="numbering === 'P'" class="mono dim">P{{ row.id }}</span>
        <span v-else class="mono dim">T{{ $index + 1 }}</span>
      </template>
    </el-table-column>
    <el-table-column label="题目" min-width="320">
      <template #default="{ row }">
        <!-- 题单锁题冻结：纯文本 + 赛事 tag，不可点；题库锁题行照常链接（跳转目标页面定） -->
        <template v-if="lockedFrozen && row.locked">
          <span class="locked-title"><b>{{ row.title }}</b></span>
        </template>
        <template v-else-if="titleTo">
          <RouterLink :to="titleTo(row as ProblemTableRow)" class="tlink" @click.stop>{{ row.title }}</RouterLink>
        </template>
        <template v-else>
          <span class="tlink" @click="emit('title-click', row as ProblemTableRow)">{{ row.title }}</span>
        </template>
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
        <!-- 难度空值横杠（赛时 hideMeta 服务端不下发 / 作业缺省），不按 D3 兜底 -->
        <TierBadge v-if="row.difficulty != null" :tier="row.difficulty" kind="difficulty" />
        <span v-else class="dim">—</span>
      </template>
    </el-table-column>
    <el-table-column min-width="180">
      <template #header>
        <!-- 知识点/标签切换列头（题库双树）：默认静态「知识点」，题库传 tagSwitch 得到切换按钮 -->
        <button v-if="tagSwitch" class="tag-col-toggle" title="点击切换知识点 / 标签" @click="emit('toggle-tag-tree')">
          <span :class="{ cur: tagTree === 'knowledge' }">知识点</span><span class="slash">/</span><span :class="{ cur: tagTree === 'meta' }">标签</span>
        </button>
        <span v-else>知识点</span>
      </template>
      <template #default="{ row }">
        <!-- 赛时遮罩（result_hidden）与字段缺席（hideMeta 不下发）统一落横杠 -->
        <FitTags v-if="!row.result_hidden && tagTree === 'knowledge' && row.tags?.length" :ids="row.tags" tree="knowledge" />
        <FitTags v-else-if="!row.result_hidden && tagTree === 'meta' && row.meta_tags?.length" :ids="row.meta_tags" tree="meta" />
        <span v-else class="dim">—</span>
      </template>
    </el-table-column>
    <el-table-column v-if="showRate" label="通过率" width="88">
      <template #default="{ row }">
        <!-- 原生 title（v0.62 挂载冻结口径）：50 行 × el-tooltip 实例是列表刷新卡的同构成分 -->
        <span
          v-if="!row.result_hidden && row.submit_count"
          class="mono"
          :title="`AC ${row.accepted_count ?? 0} / 提交 ${row.submit_count}`"
        >{{ acceptRate(row) }}</span>
        <span v-else class="dim">—</span>
      </template>
    </el-table-column>
  </el-table>
</template>

<style scoped>
/* 收藏星标（v0.86 自题库页收编）：未收藏墨灰轮廓，悬停琥珀微亮，已收藏琥珀实心
   （--soj-warning 语义档即 TLE 同源琥珀，半透明派生走 color-mix 纪律） */
.fav-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  margin-right: 2px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--soj-ink-3);
  cursor: pointer;
  vertical-align: middle;
  transition:
    color 0.15s,
    background-color 0.15s;
}

.fav-btn:hover {
  color: var(--soj-warning);
  background: color-mix(in srgb, var(--soj-warning) 12%, transparent);
}

.fav-btn.on {
  color: var(--soj-warning);
}

.fav-btn.on svg {
  fill: currentColor;
}

/* 题单锁题冻结行标题（原 ListDetailView .ptitle.locked 语义：墨灰 + 不可点光标） */
.locked-title {
  color: var(--soj-ink-3);
  cursor: not-allowed;
}

/* 冻结行整行光标复原（整表 pointer 时的例外行；:deep 穿透 EP 行元素） */
:deep(.el-table__row.locked-row) {
  cursor: default;
}

/* 知识点/标签切换列头（自题库页收编原版三态）：蓝色可点击，当前维度不透明 + 下划线，
   另一维度淡化 */
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
</style>

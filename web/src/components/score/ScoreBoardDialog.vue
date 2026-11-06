<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import UserNameLevel from '@/components/UserNameLevel.vue'

import ScoreCell from './ScoreCell.vue'
import type { ScoreBoardView, ScoreCellView, ScoreProblemView, ScoreRowView } from './types'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    board: ScoreBoardView | null
    title?: string
    loading?: boolean
    canExport?: boolean
    exporting?: boolean
    myUid?: number | null
  }>(),
  {
    title: '成绩表',
    loading: false,
    canExport: false,
    exporting: false,
    myUid: null,
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'refresh'): void
  (e: 'export'): void
  (e: 'inspect', payload: { uid: number; name: string; pid: number; title: string }): void
}>()

const router = useRouter()

// 客户端分页（v0.63 挂载冻结扫尾批）：全参赛者 × 全题矩阵一次挂载在百人场次 =
// 行×题个 ScoreCell 组件实例 + 每题列表头 tooltip，弹窗打开即冻结——50 行/页封顶，
// 榜单数据整包照拿（导出 CSV/定位我仍是全量口径），只把 DOM 挂载量分页化。
const PAGE = 50
const page = ref(1)
const pagedRows = computed(() => {
  const rows = props.board?.rows ?? []
  return rows.slice((page.value - 1) * PAGE, page.value * PAGE)
})
// 新榜单/刷新：页码复位，并自动跳到「我」所在页（看榜首先是找自己）
watch(
  () => props.board,
  (b) => {
    const idx = b ? b.rows.findIndex((r) => r.uid === props.myUid) : -1
    page.value = idx >= 0 ? Math.floor(idx / PAGE) + 1 : 1
  },
)

function cellOf(row: ScoreRowView, p: ScoreProblemView): ScoreCellView {
  return (
    row.cells.find((c) => c.pid === p.pid) ?? {
      pid: p.pid,
      score: 0,
      errors: 0,
      fullScore: p.fullScore,
      tries: 0,
      state: 'untried',
    }
  )
}

function goProblem(p: ScoreProblemView) {
  if (p.route) router.push(p.route)
}

function onInspect(row: ScoreRowView, p: ScoreProblemView) {
  emit('inspect', { uid: row.uid, name: row.name, pid: p.pid, title: p.title })
}

// 我的行整行淡蓝底（main.css .me-row 全局样式，与排名页/题单排名同款）——
// 替代旧「我的行每格 2px 描边」的格级圈框（26 列即 26 个框，视觉噪音，2026-10-27 用户拍板去掉）
function rowClass({ row }: { row: ScoreRowView }): { 'me-row': boolean } {
  return { 'me-row': row.uid === props.myUid }
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    width="1200"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <template #header>
      <TipLabel :label="title" tip="点击分数格可查看该题对应提交；绿色满分 · 橙色部分分 · 红色零分" />
    </template>
    <div class="toolbar">
      <span class="dim">共 {{ board?.rows.length ?? 0 }} 人 · {{ board?.problems.length ?? 0 }} 题</span>
      <span class="actions">
        <el-button size="small" :loading="loading" @click="emit('refresh')">刷新</el-button>
        <el-button
          v-if="canExport"
          size="small"
          type="primary"
          :loading="exporting"
          @click="emit('export')"
        >
          导出 CSV
        </el-button>
      </span>
    </div>

    <div v-loading="loading">
      <el-table
        v-if="board?.rows.length"
        stripe
        :data="pagedRows"
        row-key="uid"
        :row-class-name="rowClass as never"
        class="score-table"
      >
        <el-table-column label="排名" width="60" align="center" fixed="left">
          <template #default="{ row }">{{ row.rank ?? '—' }}</template>
        </el-table-column>
        <el-table-column label="昵称" min-width="190" fixed="left">
          <template #default="{ row }">
            <UserNameLevel :name="row.name" :level="row.level" :me="row.uid === myUid" :uid="row.uid" />
          </template>
        </el-table-column>
        <el-table-column label="总分" width="64" align="center" fixed="left">
          <template #default="{ row }"><span class="total">{{ row.total }}</span></template>
        </el-table-column>
        <el-table-column
          v-for="p in board.problems"
          :key="p.pid"
          :label="p.label"
          min-width="64"
          align="center"
          class-name="prob-col"
        >
          <template #header>
            <el-tooltip :content="p.title" placement="top" :show-after="200">
              <el-link v-if="p.route" type="primary" @click="goProblem(p)">
                {{ p.label }}
              </el-link>
              <span v-else>{{ p.label }}</span>
            </el-tooltip>
          </template>
          <template #default="{ row }">
            <ScoreCell
              :cell="cellOf(row as ScoreRowView, p)"
              :can-inspect="board.canInspect"
              @inspect="onInspect(row as ScoreRowView, p)"
            />
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else description="暂无成绩" :image-size="60" />
      <!-- 分页（v0.63）：50 行/页封顶挂载；已自动定位到「我」所在页 -->
      <div v-if="(board?.rows.length ?? 0) > PAGE" class="pager-row">
        <el-pagination
          background
          layout="prev, pager, next, total"
          :total="board?.rows.length ?? 0"
          :page-size="PAGE"
          :current-page="page"
          @current-change="(p: number) => (page = p)"
        />
      </div>
    </div>
  </el-dialog>
</template>

<style scoped>
/* 全局 .toolbar 之上的增量：两端对齐（不换行工具条） */
.toolbar {
  justify-content: space-between;
}

.actions {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-8);
}

/* T1/T2 默认不铺下划线，悬停再提示可点（密集表头降低视觉噪音） */
.score-table :deep(.el-link) {
  text-decoration: none;
}

.score-table :deep(.el-link:hover) {
  text-decoration: underline;
}

/* 分数格竖向空间（2026-10-27）：.cell 默认行高 23px + overflow hidden 会把 26px 的
   分数药丸（及我的行 2px 描边）上下沿裁平——放开行高并给上下内距，行高随内容走 */
.score-table :deep(.el-table .cell) {
  line-height: normal;
  padding-top: var(--soj-sp-4); /* 5px→sp-4 就近归并 */
  padding-bottom: var(--soj-sp-4);
}

/* 题目列横向空间（2026-10-27）：.cell 默认左右内距 12px 把 52px 列宽吃到只剩 28px——
   「T11」表头挤出省略点、36px 分数药丸两侧被裁、右下角错误次数角标整块落在裁切区外。
   题目列内距收到 sp-6（64px 列宽 = 52px 内容，表头/药丸/角标全部放得下），行内看 26+ 列
   依旧走表格横向滚动 */
.score-table :deep(.prob-col .cell) {
  padding-left: var(--soj-sp-6);
  padding-right: var(--soj-sp-6);
}

.total {
  color: var(--soj-blue);
  font-weight: var(--soj-fw-bold);
  font-variant-numeric: tabular-nums;
}

.pager-row {
  display: flex;
  justify-content: center;
  margin-top: var(--soj-sp-10);
}
</style>

<script setup lang="ts">
// 题库整理（v0.43 两段式；v0.49 拆双模式）：AI 整理 = AI/规则只产建议，root 预览勾选
// 确认后才批量写库；手动整理 = 分页题目表（每页 100 题）直接手改，见 ManualTidyPanel。
// 三类任务共用单任务骨架（run → 2s 轮询 → 差异表 → apply），照 BackupPanel
// 轮询范式（失败计数超限复位按钮，v0.37）。冲突题置顶 + 默认不勾 + 可改判是本面板灵魂：
// AI 档与通过率档并列展示由 root 拍板（plan 2026-10-31 三拍板）。
// v0.49 双模式 + 建议表服务端分页（验收期调整）：页面只持有当前页 ≤100 条建议——
// 全量快照（2000 题级）整包进 2s 轮询是卡顿根源；勾选为当前页独立语义，翻页即清，
// 不做跨页保留。suggestion_total/conflict_total 为全量口径（翻页器与冲突计数）。
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import { usePolling } from '@/composables/usePolling'
import LazySelectCell from '@/components/LazySelectCell.vue'
import ManualTidyPanel from './ManualTidyPanel.vue'
import type { TidyKind, TidySuggestion, TidyStatus } from '@/types'

// 模式卡（v0.49）：AI整理（建议流）/ 手动整理（直改流）
const MODE_META = {
  ai: {
    label: 'AI整理',
    desc: 'AI 读题面评估难度、从知识树候选集选知识点、标题规则重挂来源标签——只产建议不写库，预览勾选确认后才应用。',
  },
  manual: {
    label: '手动整理',
    desc: '分页题目表（每页 100 题）直接手改难度 / 知识点 / 来源标签，保存即写库并计入已整理水位，不经 AI。',
  },
} as const

const mode = ref<keyof typeof MODE_META>('ai')
const PAGE_SIZE = 100
// 难度改判十档选项（LazySelectCell 单选 D1..D10）
const diffOptions = Array.from({ length: 10 }, (_, i) => ({ id: i + 1, label: `D${i + 1}` }))

const KIND_META: Record<TidyKind, { label: string; desc: string }> = {
  difficulty: {
    label: '难度',
    desc: 'AI 读题面对照难度锚点表给主建议；通过率档位并列参考。两者档差 ≥2 判冲突置顶默认不勾，由你改判。AI 失败的题按通过率兜底。',
  },
  knowledge: {
    label: '知识点',
    desc: 'AI 从知识树候选集中选题（逐字一致），幻觉/歧义/树外名一律丢弃不自动建节点；现有标签认为无关的会出移除建议。',
  },
  meta: {
    label: '来源标签',
    desc: '纯规则零 AI：重跑标题方括号解析（年份/地区/来源名）对齐来源树重挂，树外孤儿 ID 清除；标题无线索的题只列出不动。',
  },
}

const kind = ref<TidyKind>('difficulty')
const redo = ref(false) // 重新整理已整理过的题（默认只整理未整理的，断点续整）
const rangeFrom = ref<number | undefined>() // 题号范围下界（含；空 = 全库）
const rangeTo = ref<number | undefined>() // 题号范围上界（含；空 = 全库）
const starting = ref(false)
const status = ref<TidyStatus | null>(null)
const applying = ref(false)
const appliedPids = ref(new Set<number>())
const onlyConflict = ref(false) // 难度表「只看冲突」筛选

// 勾选（pid → true）与难度改判（pid → 最终值，默认建议值）
const selected = ref(new Set<number>())
const finalDiff = ref(new Map<number, number>())
const tableRef = ref()

const running = computed(() => status.value?.state === 'running')
const done = computed(() => ['done', 'cancelled', 'failed'].includes(status.value?.state ?? ''))
// 当前页切片（服务端分页后 status.suggestions 只含本页 ≤100 条）
const suggestions = computed<TidySuggestion[]>(() => status.value?.suggestions ?? [])
const suggestionTotal = computed(() => status.value?.suggestion_total ?? suggestions.value.length)
const conflictCount = computed(
  () => status.value?.conflict_total ?? suggestions.value.filter((s) => s.conflict).length,
)
const jobKind = computed<TidyKind | null>(() => status.value?.kind ?? null)

// 任务题号范围（快照带出）：单边留空显示开口；全空 = 全库不显示
const rangeLabel = computed(() => {
  const f = status.value?.from ?? 0
  const t = status.value?.to ?? 0
  if (!f && !t) return ''
  return `题号 ${f || '起'}~${t || '止'}`
})

// 预览表数据 = 服务端当前页切片（only_conflict 过滤也在服务端做，总数口径一致）
const tableRows = computed<TidySuggestion[]>(() => suggestions.value)

const page = ref(1)
const pageLabel = computed(() => {
  const n = suggestionTotal.value
  if (n <= PAGE_SIZE) return ''
  const lo = (page.value - 1) * PAGE_SIZE + 1
  const hi = Math.min(page.value * PAGE_SIZE, n)
  return ` · 当前第 ${lo}–${hi} 条`
})

// 勾选默认值（plan 拍板）：难度=未设且非冲突才勾；知识点=有增/移建议才勾；来源=有建议且与现值不同才勾
function defaultChecked(s: TidySuggestion): boolean {
  if (appliedPids.value.has(s.pid)) return false
  switch (s.kind) {
    case 'difficulty':
      return !s.has_difficulty && !s.conflict
    case 'knowledge':
      return !!(s.add_tag_ids?.length || s.remove_tag_ids?.length)
    case 'meta':
      return !s.no_clue && !!s.suggest_meta_ids && !sameSet(s.cur_meta_ids, s.suggest_meta_ids)
  }
}

function sameSet(a?: number[], b?: number[]): boolean {
  const x = [...(a ?? [])].sort((m, n) => m - n)
  const y = [...(b ?? [])].sort((m, n) => m - n)
  return x.length === y.length && x.every((v, i) => v === y[i])
}

function syncSelection(rows: TidySuggestion[]) {
  selected.value = new Set(rows.filter(defaultChecked).map((s) => s.pid))
  // 难度改判初始值 = 建议值
  finalDiff.value = new Map(
    rows.filter((s) => s.kind === 'difficulty').map((s) => [s.pid, s.suggest_difficulty ?? 3]),
  )
  // toggle 会逐个触发 selection-change → onSelectionChange 用表格部分实况覆盖 selected，
  // 故循环前快照目标集、toggle 只吃快照常量（活读会让第 2 个起全部失真——实测实锤）
  const want = new Set(selected.value)
  void nextTick(() => {
    for (const row of rows) {
      tableRef.value?.toggleRowSelection(row, want.has(row.pid))
    }
  })
}

function clearSelection() {
  selected.value = new Set()
  void nextTick(() => {
    for (const row of tableRows.value) {
      tableRef.value?.toggleRowSelection(row, false)
    }
  })
}

// 按当前 page/onlyConflict 拉取状态快照（服务端切片）。runningMerge=true 时（2s 轮询拍）
// 只就地更新进度字段不整对象替换——轮询期建议表未挂载（v-if done），整替换只会让全部
// computed 白跑一遍；保持引用稳定，进度条独享重渲染（v0.63 批次2）。
async function fetchStatus(runningMerge = false): Promise<boolean> {
  try {
    const r = await adminApi.tidyStatus(page.value, PAGE_SIZE, onlyConflict.value)
    if (runningMerge && status.value && status.value.state === 'running' && r.state === 'running') {
      status.value.total = r.total
      status.value.done = r.done
      if (r.excluded !== undefined) status.value.excluded = r.excluded
    } else {
      status.value = r
    }
    return true
  } catch {
    return false
  }
}

function onSelectionChange(rows: TidySuggestion[]) {
  // 当前页语义：勾选集 = 本页表格实况（翻页即清，无跨页状态）
  selected.value = new Set(rows.map((r) => r.pid))
}

function selectAllVisible() {
  const target = new Set<number>()
  for (const row of tableRows.value) target.add(row.pid)
  selected.value = target
  void nextTick(() => {
    for (const row of tableRows.value) tableRef.value?.toggleRowSelection(row, true)
  })
}

function invertVisible() {
  // 同 syncSelection 的快照纪律：先算目标集整体赋值，再按 target 常量回放
  const target = new Set<number>()
  for (const row of tableRows.value) {
    if (!selected.value.has(row.pid)) target.add(row.pid)
  }
  selected.value = target
  void nextTick(() => {
    for (const row of tableRows.value) tableRef.value?.toggleRowSelection(row, target.has(row.pid))
  })
}

const selectedCount = computed(() => selected.value.size)

// ---- 任务生命周期 ----

const pollFails = ref(0)
// v0.65 轮询归一（usePolling 全站唯一轮询基建）：2s 拍 + 失败 5 连停 + 终态停，
// 后台标签自动暂停、作用域卸载自清（原裸 setInterval 后台空转）
const poll = usePolling(async () => {
  const ok = await fetchStatus(true)
  if (!ok) {
    if (++pollFails.value >= 5) {
      poll.stop()
      ElMessage.error('整理状态查询持续失败——请刷新页面查看；任务仍在服务器后台执行')
    }
    return
  }
  pollFails.value = 0
  if (status.value?.state && status.value.state !== 'running') {
    poll.stop()
    if (status.value.state === 'done') {
      // 全被水位排除是断点续整的正常终态（v0.45 验收期用户反馈「像跳过了扫描」）：
      // 有建议才请核对差异表；零建议带排除数时明说原因，别让 root 误判成异常
      if (suggestionTotal.value > 0) {
        ElMessage.success('整理完成，请核对差异表后勾选应用')
      } else if (status.value.excluded) {
        ElMessage.info(`整理完成：${status.value.excluded} 题均已整理过，默认排除、无新建议`)
      } else {
        ElMessage.success('整理完成，本次没有产出建议')
      }
    } else if (status.value.state === 'cancelled') {
      ElMessage.info('整理已取消（已完成部分照常可勾选应用）')
    } else {
      ElMessage.error(`整理失败：${status.value.error || '未知错误'}`)
    }
    syncSelection(suggestions.value)
  }
}, 2000)

function startPoll() {
  pollFails.value = 0
  poll.restart()
}

// 翻页 / 「只看冲突」切换：终态下按新条件拉取对应切片（轮询进行中由轮询自然带新页），
// 勾选清空重置（当前页独立语义）
watch([page, onlyConflict], async () => {
  if (running.value) return
  const ok = await fetchStatus()
  if (ok && done.value) clearSelection()
})

async function runTidy() {
  const f = rangeFrom.value
  const t = rangeTo.value
  if ((f != null && f < 1) || (t != null && t < 1) || (f != null && t != null && f > t)) {
    ElMessage.warning('题号范围不合法：需 起始 ≤ 结束（≥1），留空 = 全库')
    return
  }
  starting.value = true
  appliedPids.value = new Set()
  page.value = 1
  onlyConflict.value = false
  try {
    await adminApi.tidyRun(kind.value, redo.value, f, t)
    status.value = { state: 'running', kind: kind.value, total: 0, done: 0, suggestions: [] }
    startPoll()
    ElMessage.info(redo.value ? '整理任务已启动（全量重跑）' : '整理任务已启动（只整理未整理过的题）')
  } catch {
    // 拦截器已提示（409 已有任务 / AI 未配置）
  } finally {
    // 成功也要复位（v0.45 验收期实弹抓出）：否则弃置结果回到发起卡后按钮永久
    // 停在 loading/disabled，二次发起只能靠刷新页面——starting 只表达本次点击在途
    starting.value = false
  }
}

async function cancelTidy() {
  await adminApi.tidyCancel()
  ElMessage.info('已请求取消')
}

async function applySelected() {
  if (!jobKind.value || !selected.value.size) return
  const items = suggestions.value
    .filter((s) => selected.value.has(s.pid))
    .map((s) => {
      switch (s.kind) {
        case 'difficulty':
          return { pid: s.pid, difficulty: finalDiff.value.get(s.pid) ?? s.suggest_difficulty ?? 3 }
        case 'knowledge': {
          // 最终集 = (现有 ∪ 新增) − 移除
          const rm = new Set(s.remove_tag_ids ?? [])
          const set = new Set<number>([...(s.cur_tag_ids ?? []), ...(s.add_tag_ids ?? [])])
          for (const id of rm) set.delete(id)
          return { pid: s.pid, tags: [...set] }
        }
        case 'meta':
          return { pid: s.pid, meta_tags: [...(s.suggest_meta_ids ?? [])] }
      }
    })
  applying.value = true
  try {
    const r = await adminApi.tidyApply(jobKind.value, items)
    for (const it of items) appliedPids.value.add(it.pid)
    selected.value = new Set()
    ElMessage.success(r.message || `已应用 ${r.applied} 题`)
    void nextTick(() => {
      for (const row of tableRows.value) {
        tableRef.value?.toggleRowSelection(row, false)
      }
    })
  } finally {
    applying.value = false
  }
}

// 回到发起卡（2026-10-31 验收期修复：终态结果此前霸屏、发起入口被吞——
// 结果属于「当次任务当次处置」，root 主动弃置后才回发起区；未应用的条数给确认）
async function discardResults() {
  const pending = selected.value.size
  if (pending > 0) {
    try {
      await ElMessageBox.confirm(
        `还有 ${pending} 条勾选的建议未应用，弃置后需要重新整理才能找回。确定回到发起页？`,
        '弃置本次结果',
        { type: 'warning', confirmButtonText: '弃置并回发起页', cancelButtonText: '留在此页' },
      )
    } catch {
      return
    }
  }
  status.value = null
  selected.value = new Set()
  appliedPids.value = new Set()
  finalDiff.value = new Map()
  onlyConflict.value = false
}

onMounted(async () => {
  // 进页恢复任务态（刷新后仍在跑/刚完成的任务照常可续；建议切片按当前页拉取）
  const ok = await fetchStatus()
  if (ok && status.value?.state === 'running') startPoll()
  else if (ok && done.value) syncSelection(suggestions.value)
})
// 卸载清理由 usePolling 作用域 dispose 接管（v0.65）
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>题库整理</span>
        <el-button v-if="running" type="warning" plain @click="cancelTidy">取消整理</el-button>
        <el-button v-else-if="done" type="primary" plain @click="discardResults">发起新整理</el-button>
      </div>
    </template>

    <!-- 发起区：模式卡（AI整理 / 手动整理）+ AI 三类任务卡。任务进行中或已有结果待
         处置时收起（结果属于「当次任务当次处置」，弃置结果后回到这里） -->
    <template v-if="!running && (!status || status.state === 'idle')">
      <div class="mode-cards">
        <div
          v-for="(m, k) in MODE_META"
          :key="k"
          class="kind-card mode-card"
          :class="{ on: mode === k }"
          role="button"
          tabindex="0"
          @click="mode = k"
          @keydown.enter="mode = k"
        >
          <div class="kc-title">{{ m.label }}</div>
          <div class="kc-desc">{{ m.desc }}</div>
        </div>
      </div>
      <template v-if="mode === 'ai'">
        <div class="kind-cards">
          <div
            v-for="(m, k) in KIND_META"
            :key="k"
            class="kind-card"
            :class="{ on: kind === k }"
            role="button"
            tabindex="0"
            @click="kind = k"
            @keydown.enter="kind = k"
          >
            <div class="kc-title">{{ m.label }}</div>
            <div class="kc-desc">{{ m.desc }}</div>
          </div>
        </div>
        <div class="run-row">
          <el-button type="primary" :loading="starting" @click="runTidy">
            开始整理（{{ KIND_META[kind].label }}）
          </el-button>
          <el-checkbox v-model="redo">重新整理已整理过的题</el-checkbox>
          <span class="range-box">
            <span class="muted">题号</span>
            <el-input-number v-model="rangeFrom" :min="1" :controls="false" placeholder="起始" class="range-input" />
            <span class="muted">~</span>
            <el-input-number v-model="rangeTo" :min="1" :controls="false" placeholder="结束" class="range-input" />
            <span class="muted">留空 = 全库</span>
          </span>
          <span class="muted">默认只整理未整理过的（已应用的自动排除，从后继续）；可指定题号范围只扫部分题；只产建议不写库，勾选确认后才应用。</span>
        </div>
      </template>
      <ManualTidyPanel v-else />
    </template>

    <!-- 进行中：进度 + 取消 -->
    <div v-if="running" class="progress-row">
      <el-progress
        :percentage="status?.total ? Math.round(((status.done ?? 0) / status.total) * 100) : 0"
        :stroke-width="14"
        striped
        striped-flow
        class="prog"
      />
      <span class="muted">
        {{ status?.done ?? 0 }} / {{ status?.total ?? '…' }} 题<template v-if="status?.excluded">（已排除 {{ status.excluded }} 题整理过的）</template><template v-if="rangeLabel"> · {{ rangeLabel }}</template>
      </span>
    </div>

    <!-- 终态提示 -->
    <el-alert
      v-if="status?.state === 'failed'"
      type="error"
      :closable="false"
      show-icon
      :title="`整理失败：${status.error}`"
      class="state-alert"
    />
    <el-alert
      v-else-if="status?.state === 'cancelled'"
      type="info"
      :closable="false"
      show-icon
      title="任务已取消——已完成的建议仍在下方，可核对后应用"
      class="state-alert"
    />

    <!-- 结果差异表 -->
    <template v-if="done && suggestions.length">
      <div class="result-bar">
        <template v-if="jobKind === 'difficulty'">
          <el-checkbox v-model="onlyConflict">只看冲突（{{ conflictCount }}，默认不勾待改判）</el-checkbox>
        </template>
        <span class="muted">
          已勾选 <b>{{ selectedCount }}</b> / {{ suggestionTotal }} 题{{ pageLabel }}
          <template v-if="status?.excluded"> · 已排除 {{ status.excluded }} 题整理过的</template>
          <template v-if="appliedPids.size"> · 已应用 {{ appliedPids.size }} 题</template>
          <template v-if="rangeLabel"> · {{ rangeLabel }}</template>
        </span>
        <span class="spacer" />
        <el-button size="small" @click="selectAllVisible">全选可见</el-button>
        <el-button size="small" @click="invertVisible">反选</el-button>
        <el-button type="primary" :loading="applying" :disabled="!selectedCount" @click="applySelected">
          应用勾选（{{ selectedCount }}）
        </el-button>
      </div>

      <!-- 难度：现值 / AI 建议（可改判）/ 通过率参考 / 冲突 -->
      <el-table
        v-if="jobKind === 'difficulty'"
        ref="tableRef"
        :data="tableRows"
        size="small"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="42" />
        <el-table-column prop="pid" label="题号" width="76" />
        <el-table-column prop="title" label="标题" min-width="220" show-overflow-tooltip />
        <el-table-column label="现难度" width="86">
          <template #default="{ row }">
            <span v-if="row.has_difficulty" class="mono">D{{ row.cur_difficulty }}</span>
            <el-tag v-else size="small" type="info">未设置</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="建议（可改判）" width="150">
          <template #default="{ row }">
            <!-- v0.63 惰性单元格编辑（LazySelectCell）：点格才挂 select，同屏 ≤1 个 -->
            <LazySelectCell
              :model-value="finalDiff.get(row.pid) ?? row.suggest_difficulty ?? 3"
              :options="diffOptions"
              :readonly="appliedPids.has(row.pid)"
              empty="D3"
              @update:model-value="(v: number | number[]) => finalDiff.set((row as TidySuggestion).pid, v as number)"
            />
          </template>
        </el-table-column>
        <el-table-column label="通过率参考" width="150">
          <template #default="{ row }">
            <span class="mono">D{{ row.rate_difficulty }}</span>
            <span class="muted" style="margin-left: var(--soj-sp-6)">
              {{ row.attempted ? `${Math.round((row.pass_rate ?? 0) * 100)}% · ${row.attempted}人` : '无人做过' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="冲突" width="76">
          <template #default="{ row }">
            <el-tag v-if="row.conflict" type="danger" size="small">冲突</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="ai_error" label="备注" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="row.ai_error" class="warn-text">{{ row.ai_error }}</span>
          </template>
        </el-table-column>
      </el-table>

      <!-- 知识点：现有 / 新增 / 移除 / 丢弃 -->
      <el-table
        v-else-if="jobKind === 'knowledge'"
        ref="tableRef"
        :data="tableRows"
        size="small"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="42" />
        <el-table-column prop="pid" label="题号" width="76" />
        <el-table-column prop="title" label="标题" min-width="180" show-overflow-tooltip />
        <el-table-column label="现有" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">{{ (row.cur_tag_names ?? []).join('、') || '—' }}</template>
        </el-table-column>
        <el-table-column label="新增" min-width="140">
          <template #default="{ row }">
            <template v-if="row.add_tag_names?.length">
              <el-tag v-for="n in row.add_tag_names" :key="n" size="small" type="success" class="tag-gap">{{ n }}</el-tag>
            </template>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="移除" min-width="140">
          <template #default="{ row }">
            <template v-if="row.remove_tag_names?.length">
              <el-tag v-for="n in row.remove_tag_names" :key="n" size="small" type="danger" class="tag-gap">{{ n }}</el-tag>
            </template>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="丢弃（AI 越界名）" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">{{ (row.dropped ?? []).join('；') || '—' }}</template>
        </el-table-column>
        <el-table-column prop="note" label="备注" min-width="140" show-overflow-tooltip />
      </el-table>

      <!-- 来源标签：现值 / 建议 / 状态 -->
      <el-table
        v-else-if="jobKind === 'meta'"
        ref="tableRef"
        :data="tableRows"
        size="small"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="42" />
        <el-table-column prop="pid" label="题号" width="76" />
        <el-table-column prop="title" label="标题" min-width="220" show-overflow-tooltip />
        <el-table-column label="现来源" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">{{ (row.cur_meta_names ?? []).join('、') || '—' }}</template>
        </el-table-column>
        <el-table-column label="建议来源" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">
            <template v-if="row.no_clue"><el-tag size="small" type="info">标题无线索</el-tag></template>
            <template v-else>{{ (row.suggest_meta_names ?? []).join('、') || '（清空）' }}</template>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.no_clue && !(row.cur_meta_ids?.length)" size="small" type="info">不动</el-tag>
            <el-tag v-else-if="sameSet(row.cur_meta_ids, row.suggest_meta_ids)" size="small" type="success">已对齐</el-tag>
            <el-tag v-else size="small" type="warning">待变更</el-tag>
          </template>
        </el-table-column>
      </el-table>

      <!-- 100/页分页器（v0.49，服务端分页）：只拉当前页切片，翻页勾选清空（当前页独立语义） -->
      <div v-if="suggestionTotal > PAGE_SIZE" class="pager-row">
        <el-pagination
          background
          layout="prev, pager, next, total"
          :total="suggestionTotal"
          :page-size="PAGE_SIZE"
          :current-page="page"
          @current-change="(p: number) => (page = p)"
        />
      </div>
    </template>

    <!-- 完成但零结果：全被水位排除（断点续整的正常终态，v0.45 验收期用户反馈曾误读为跳过扫描） -->
    <el-alert
      v-else-if="done && !suggestionTotal && status?.excluded"
      type="info"
      :closable="false"
      show-icon
      :title="`${status.excluded} 题全部已整理过，默认排除——本次没有新建议`"
      class="state-alert"
    >
      <span class="muted">
        {{ jobKind === 'difficulty' ? '这些题难度已设置或已应用过整理' : '这些题已应用过整理' }}，默认不再进任务。
        如需全量重跑：点右上角「发起新整理」→ 勾选「重新整理已整理过的题」→ 重新开始。
      </span>
    </el-alert>
    <div v-else-if="done && !suggestionTotal" class="muted" style="padding: var(--soj-sp-12) 0">
      本次任务没有产出任何建议（{{ status?.error || (rangeLabel ? `${rangeLabel} 内没有待整理的题` : '题库为空，或在产出第一条建议前被取消') }}）。
    </div>
  </el-card>
</template>

<style scoped>
.head {
  gap: var(--soj-sp-12); /* 全局 .head 之上的增量：行距 sp-12（全局默认 sp-8） */
}

.kind-cards {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--soj-sp-12);
}

/* 模式卡（v0.49）：AI整理 / 手动整理 两列，嵌在任务卡上方 */
.mode-cards {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--soj-sp-12);
  margin-bottom: var(--soj-sp-12);
}

.pager-row {
  display: flex;
  justify-content: center;
  margin-top: var(--soj-sp-12);
}

.kind-card {
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  padding: var(--soj-sp-12) var(--soj-sp-14);
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s;
}

.kind-card.on {
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 1px var(--el-color-primary) inset;
}

.kc-title {
  font-weight: var(--soj-fw-emphasis);
  margin-bottom: var(--soj-sp-6);
}

.kc-desc {
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-13); /* --soj-fs-secondary 未定义，原兜底 13px 即 fs-13 */
  line-height: 1.6;
}

.run-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-14);
  margin-top: var(--soj-sp-14);
  flex-wrap: wrap;
}

.range-box {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-6);
}

.range-input {
  width: 88px;
}

.progress-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-14);
}

.prog {
  flex: 1;
}

.state-alert {
  margin: var(--soj-sp-12) 0;
}

.result-bar {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  flex-wrap: wrap;
  margin: var(--soj-sp-14) 0 var(--soj-sp-10);
}

.spacer {
  flex: 1;
}

.tag-gap {
  margin: 0 var(--soj-sp-4) var(--soj-sp-4) 0;
}

.warn-text {
  color: var(--el-color-warning);
}

.mono {
  font-family: var(--font-mono);
}

.muted {
  color: var(--el-text-color-secondary);
}

/* 窄屏：任务卡纵排（面板内非整页，≤992px 收为一列；模式卡两列够放不动） */
@media (max-width: 992px) {
  .kind-cards {
    grid-template-columns: 1fr;
  }
}
</style>

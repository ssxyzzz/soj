<script setup lang="ts">
// 题库一键整理（v0.43，plan §2.45）：两段式——AI/规则只产建议，root 预览勾选确认后
// 才批量写库。三类任务共用单任务骨架（run → 2s 轮询 → 差异表 → apply），照 BackupPanel
// 轮询范式（失败计数超限复位按钮，v0.37）。冲突题置顶 + 默认不勾 + 可改判是本面板灵魂：
// AI 档与通过率档并列展示由 root 拍板（plan 2026-10-31 三拍板）。
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import type { TidyKind, TidySuggestion, TidyStatus } from '@/types'

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
const suggestions = computed<TidySuggestion[]>(() => status.value?.suggestions ?? [])
const conflictCount = computed(() => suggestions.value.filter((s) => s.conflict).length)
const jobKind = computed<TidyKind | null>(() => status.value?.kind ?? null)

// 预览表数据（难度表按「只看冲突」过滤；其余全量）
const tableRows = computed<TidySuggestion[]>(() => {
  if (jobKind.value === 'difficulty' && onlyConflict.value) {
    return suggestions.value.filter((s) => s.conflict)
  }
  return suggestions.value
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
  void nextTick(() => {
    for (const row of rows) {
      tableRef.value?.toggleRowSelection(row, selected.value.has(row.pid))
    }
  })
}

function onSelectionChange(rows: TidySuggestion[]) {
  selected.value = new Set(rows.map((r) => r.pid))
}

function selectAllVisible() {
  for (const row of tableRows.value) selected.value.add(row.pid)
  void nextTick(() => {
    for (const row of tableRows.value) tableRef.value?.toggleRowSelection(row, true)
  })
}

function invertVisible() {
  for (const row of tableRows.value) {
    if (selected.value.has(row.pid)) selected.value.delete(row.pid)
    else selected.value.add(row.pid)
    tableRef.value?.toggleRowSelection(row, selected.value.has(row.pid))
  }
}

const selectedCount = computed(() => selected.value.size)

// ---- 任务生命周期 ----

const pollFails = ref(0)
let pollTimer: ReturnType<typeof setInterval> | undefined

function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = undefined
  }
}

function startPoll() {
  stopPoll()
  pollFails.value = 0
  pollTimer = setInterval(async () => {
    try {
      status.value = await adminApi.tidyStatus()
      pollFails.value = 0
    } catch {
      if (++pollFails.value >= 5) {
        stopPoll()
        ElMessage.error('整理状态查询持续失败——请刷新页面查看；任务仍在服务器后台执行')
      }
      return
    }
    if (status.value?.state && status.value.state !== 'running') {
      stopPoll()
      if (status.value.state === 'done') {
        ElMessage.success('整理完成，请核对差异表后勾选应用')
      } else if (status.value.state === 'cancelled') {
        ElMessage.info('整理已取消（已完成部分照常可勾选应用）')
      } else {
        ElMessage.error(`整理失败：${status.value.error || '未知错误'}`)
      }
      syncSelection(suggestions.value)
    }
  }, 2000)
}

async function runTidy() {
  starting.value = true
  appliedPids.value = new Set()
  onlyConflict.value = false
  try {
    await adminApi.tidyRun(kind.value, redo.value)
    status.value = { state: 'running', kind: kind.value, total: 0, done: 0, suggestions: [] }
    startPoll()
    ElMessage.info(redo.value ? '整理任务已启动（全量重跑）' : '整理任务已启动（只整理未整理过的题）')
  } catch {
    starting.value = false // 拦截器已提示（409 已有任务 / AI 未配置）
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
  // 进页恢复任务态（刷新后仍在跑/刚完成的任务照常可续）
  try {
    status.value = await adminApi.tidyStatus()
    if (status.value.state === 'running') startPoll()
    else if (done.value) syncSelection(suggestions.value)
  } catch {
    /* 未登录等由拦截器处理 */
  }
})
onUnmounted(stopPoll)
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>题库一键整理</span>
        <el-button v-if="running" type="warning" plain @click="cancelTidy">取消整理</el-button>
        <el-button v-else-if="done" type="primary" plain @click="discardResults">发起新整理</el-button>
      </div>
    </template>

    <!-- 发起区：三类任务卡（任务进行中或已有结果待处置时收起，弃置结果后回到这里） -->
    <template v-if="!running && (!status || status.state === 'idle')">
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
        <span class="muted">默认只整理未整理过的（已应用的自动排除，从后继续）；只产建议不写库，勾选确认后才应用。</span>
      </div>
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
        {{ status?.done ?? 0 }} / {{ status?.total ?? '…' }} 题<template v-if="status?.excluded">（已排除 {{ status.excluded }} 题整理过的）</template>
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
          已勾选 <b>{{ selectedCount }}</b> / {{ tableRows.length }} 题
          <template v-if="status?.excluded"> · 已排除 {{ status.excluded }} 题整理过的</template>
          <template v-if="appliedPids.size"> · 已应用 {{ appliedPids.size }} 题</template>
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
            <el-select
              :model-value="finalDiff.get(row.pid) ?? row.suggest_difficulty"
              size="small"
              :disabled="appliedPids.has(row.pid)"
              @update:model-value="(v: number) => finalDiff.set(row.pid, v)"
            >
              <el-option v-for="d in 10" :key="d" :value="d" :label="`D${d}`" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="通过率参考" width="150">
          <template #default="{ row }">
            <span class="mono">D{{ row.rate_difficulty }}</span>
            <span class="muted" style="margin-left: 6px">
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
    </template>

    <!-- 完成但零结果（如全部取消于第一题前） -->
    <div v-else-if="done && !suggestions.length" class="muted" style="padding: 12px 0">
      本次任务没有产出任何建议（{{ status?.error || '可能在第一题完成前被取消' }}）。
    </div>
  </el-card>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.kind-cards {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
}

.kind-card {
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  padding: 12px 14px;
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s;
}

.kind-card.on {
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 1px var(--el-color-primary) inset;
}

.kc-title {
  font-weight: var(--soj-fw-emphasis);
  margin-bottom: 6px;
}

.kc-desc {
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-secondary, 13px);
  line-height: 1.6;
}

.run-row {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-top: 14px;
  flex-wrap: wrap;
}

.progress-row {
  display: flex;
  align-items: center;
  gap: 14px;
}

.prog {
  flex: 1;
}

.state-alert {
  margin: 12px 0;
}

.result-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  margin: 14px 0 10px;
}

.spacer {
  flex: 1;
}

.tag-gap {
  margin: 0 4px 4px 0;
}

.warn-text {
  color: var(--el-color-warning);
}

.mono {
  font-family: var(--soj-font-mono, monospace);
}

.muted {
  color: var(--el-text-color-secondary);
}

/* 窄屏：任务卡纵排（面板内非整页，≤992px 收为一列） */
@media (max-width: 992px) {
  .kind-cards {
    grid-template-columns: 1fr;
  }
}
</style>

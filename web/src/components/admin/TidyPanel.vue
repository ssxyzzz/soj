<script setup lang="ts">
// 题库整理（v0.43 两段式；v0.49 拆双模式；v0.92 二期）：AI 整理 = AI/规则只产建议，
// root 预览勾选确认后才批量写库；手动整理 = 分页题目表（每页 100 题）直接手改，
// 见 ManualTidyPanel；整理历史 = apply 两路批次留痕回看（逐题 old→new 可审计）。
// 三类任务共用单任务骨架（run → 2s 轮询 → 差异表 → apply），照 BackupPanel
// 轮询范式（失败计数超限复位按钮，v0.37）。
// v0.92 难度五源判读：题面+知识点+题解+AC代码+网上答案（GLM web_search）综合，
// 通过率链路退役——参考列/冲突置顶/只看冲突/兜底全数移除，AI 失败行无建议默认不勾。
// v0.68 知识点勾选细分：知识点表退役整行勾选，新增/移除两维独立勾选采纳（默认两维全勾 =
// 拆分前「有建议即整行勾」语义）——只勾新增时现有标签原样保留，不勾移除就不移除。
// v0.92.1 逐知识点勾选：两维勾选从「每题一个框」细化为「每个知识点一个框」
//（selAdd/selRm 改 pid:tagId 复合键），可只采纳某题建议中的部分标签；全选可见
// 随之拆成「全选新增 / 全选移除」两枚（反选 = 可见行逐标签取反）。
// v0.49 建议表服务端分页：页面只持有当前页 ≤100 条建议——全量快照（2000 题级）
// 整包进 2s 轮询是卡顿根源；勾选为当前页独立语义，翻页即清，不做跨页保留。
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import { usePolling } from '@/composables/usePolling'
import LazySelectCell from '@/components/LazySelectCell.vue'
import ManualTidyPanel from './ManualTidyPanel.vue'
import type { TidyApplyItem, TidyHistoryBatch, TidyHistoryDetail, TidyKind, TidySuggestion, TidyStatus } from '@/types'

// 模式卡（v0.49 双模式 + v0.92 历史第三卡）：AI整理（建议流）/ 手动整理（直改流）/ 整理历史（回看）
const MODE_META = {
  ai: {
    label: 'AI整理',
    desc: '综合 = 难度+知识点一次判读（五源共享一次外呼，全库整理外呼减半）；来源 = 标题规则重挂。只产建议不写库，预览勾选确认后才应用。',
  },
  manual: {
    label: '手动整理',
    desc: '分页题目表（每页 100 题）直接手改难度 / 知识点 / 来源标签，保存即写库并计入已整理水位，不经 AI。',
  },
  history: {
    label: '整理历史',
    desc: 'AI 确认应用与手动整理保存的每一批次都留痕：谁、何时、逐题从什么改成什么，可追溯可审计。',
  },
} as const

const mode = ref<keyof typeof MODE_META>('ai')
// 预览表每页条数可调（v0.94.1 用户拍板：50/100/200/500/1000，服务端切片上限已放宽）
const pageSize = ref(100)
const pageSizeOptions = [50, 100, 200, 500, 1000]
// 难度改判十档选项（LazySelectCell 单选 D1..D10）
const diffOptions = Array.from({ length: 10 }, (_, i) => ({ id: i + 1, label: `D${i + 1}` }))

// 可发起的整理维度（v0.94.1：单维难度/知识点 AI 整理退役，综合全覆盖）；
// TidyKind 仍保留四值供历史批次 kinds 兼容渲染
const KIND_META = {
  combo: {
    label: '综合',
    desc: '难度+知识点一次判读（五源共享一次外呼，搜索一次，全库整理外呼减半）：难度对照锚点表、知识点对照知识树层级锚点+难度锚点档位校准（逐字一致、父级自动补链、低级可行只标低级、一题多解同档才都标）。输出整体解析失败则两维都不出建议。',
  },
  meta: {
    label: '来源标签',
    desc: '纯规则零 AI：重跑标题方括号解析（年份/地区/来源名）对齐来源树重挂，树外孤儿 ID 清除；标题无线索的题只列出不动。',
  },
} as const

const kind = ref<keyof typeof KIND_META>('combo')
const redo = ref(false) // 重新整理已整理过的题（默认只整理未整理的，断点续整）
const rangeFrom = ref<number | undefined>() // 题号范围下界（含；空 = 全库）
const rangeTo = ref<number | undefined>() // 题号范围上界（含；空 = 全库）
const starting = ref(false)
const status = ref<TidyStatus | null>(null)
const applying = ref(false)
const appliedPids = ref(new Set<number>())

// 勾选四件套：selected = 待应用行集（难度/来源 = 表格整行勾；知识点 = 两维有勾的题并集；
// 综合 = 难度行勾 ∪ 两维有勾题并集，应用按钮与计数口径），selAdd/selRm = 知识点新增/移除
// 两维逐标签采纳集（v0.92.1 细化，复合键 `${pid}:${tagId}`，同题可只勾部分标签）；
// selDiff = 综合模式难度维行勾（v0.94，自持集合不走 el-table selection——避免与逐标签
// 勾的并集语义打架）；finalDiff = 难度改判（pid → 最终值）
const selected = ref(new Set<number>())
const selAdd = ref(new Set<string>())
const selRm = ref(new Set<string>())
const selDiff = ref(new Set<number>())
const finalDiff = ref(new Map<number, number>())

// 知识点复合键 → 题号集（selected 维护用；应用/计数按题口径不变）
function pidsOfKeys(keys: Iterable<string>): Set<number> {
  const pids = new Set<number>()
  for (const k of keys) pids.add(Number(k.slice(0, k.indexOf(':'))))
  return pids
}

// 综合模式 selected 不变式：难度行勾 ∪ 两维有勾题并集（v0.94）
function syncComboSelected() {
  selected.value = new Set([...pidsOfKeys([...selAdd.value, ...selRm.value]), ...selDiff.value])
}
const tableRef = ref()

const running = computed(() => status.value?.state === 'running')
const done = computed(() => ['done', 'cancelled', 'failed'].includes(status.value?.state ?? ''))
// 当前页切片（服务端分页后 status.suggestions 只含本页 ≤100 条）
const suggestions = computed<TidySuggestion[]>(() => status.value?.suggestions ?? [])
const suggestionTotal = computed(() => status.value?.suggestion_total ?? suggestions.value.length)
const jobKind = computed<TidyKind | null>(() => status.value?.kind ?? null)

// 任务题号范围（快照带出）：单边留空显示开口；全空 = 全库不显示。
// v0.92.1 去「题号」前缀精简（result-bar 一行放不下按钮曾换行）
const rangeLabel = computed(() => {
  const f = status.value?.from ?? 0
  const t = status.value?.to ?? 0
  if (!f && !t) return ''
  return `${f || '起'}~${t || '止'}`
})

// 预览表数据 = 服务端当前页切片（only_conflict 过滤也在服务端做，总数口径一致）
const tableRows = computed<TidySuggestion[]>(() => suggestions.value)

const page = ref(1)
const pageLabel = computed(() => {
  const n = suggestionTotal.value
  if (n <= pageSize.value) return ''
  const lo = (page.value - 1) * pageSize.value + 1
  const hi = Math.min(page.value * pageSize.value, n)
  return ` · 当前第 ${lo}–${hi} 条`
})

// 整行勾选默认值（来源标签口径）：有建议且与现值不同才勾。
// 综合不走整行勾选（难度维 selDiff + 知识点逐标签两维，默认值见 syncSelection combo 分支）
function defaultChecked(s: TidySuggestion): boolean {
  if (appliedPids.value.has(s.pid)) return false
  if (s.kind === 'meta') {
    return !s.no_clue && !!s.suggest_meta_ids && !sameSet(s.cur_meta_ids, s.suggest_meta_ids)
  }
  return false
}

function sameSet(a?: number[], b?: number[]): boolean {
  const x = [...(a ?? [])].sort((m, n) => m - n)
  const y = [...(b ?? [])].sort((m, n) => m - n)
  return x.length === y.length && x.every((v, i) => v === y[i])
}

function syncSelection(rows: TidySuggestion[]) {
  if (jobKind.value === 'combo') {
    // 拆分默认值：新增/移除两维逐标签默认全勾（含父级自动补链行，均可取消）；
    // 难度维：未设难度且有建议才勾（同原单维难度默认口径），改判初值 = 建议值；
    // 无内置 selection 列，无需 toggleRowSelection 回放
    const add = new Set<string>()
    const rm = new Set<string>()
    const diff = new Set<number>()
    for (const s of rows) {
      if (appliedPids.value.has(s.pid)) continue
      for (const id of s.add_tag_ids ?? []) add.add(`${s.pid}:${id}`)
      for (const id of s.remove_tag_ids ?? []) rm.add(`${s.pid}:${id}`)
      if (!s.has_difficulty && !s.ai_error && (s.suggest_difficulty ?? 0) >= 1) {
        diff.add(s.pid)
      }
    }
    selAdd.value = add
    selRm.value = rm
    selDiff.value = diff
    finalDiff.value = new Map(
      rows.filter((s) => s.kind === 'combo' && !s.ai_error).map((s) => [s.pid, s.suggest_difficulty ?? 3]),
    )
    selected.value = new Set([...pidsOfKeys([...add, ...rm]), ...diff])
    return
  }
  selected.value = new Set(rows.filter(defaultChecked).map((s) => s.pid))
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
  selAdd.value = new Set()
  selRm.value = new Set()
  selDiff.value = new Set()
  void nextTick(() => {
    for (const row of tableRows.value) {
      tableRef.value?.toggleRowSelection(row, false)
    }
  })
}

// 按当前 page 拉取状态快照（服务端切片）。runningMerge=true 时（2s 轮询拍）
// 只就地更新进度字段不整对象替换——轮询期建议表未挂载（v-if done），整替换只会让全部
// computed 白跑一遍；保持引用稳定，进度条独享重渲染（v0.63 批次2）。
async function fetchStatus(runningMerge = false): Promise<boolean> {
  try {
    const r = await adminApi.tidyStatus(page.value, pageSize.value)
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

// 综合全选拆三枚（v0.94.1 用户拍板）：全选难度 / 全选新增 / 全选移除各自独立，
// 可见行逐项补勾；跳过已应用行（其勾选框已禁用，程序化再勾与禁用态自相矛盾）
function selectAllCombo(part: 'diff' | 'add' | 'rm') {
  if (part === 'diff') {
    const diff = new Set(selDiff.value)
    for (const row of tableRows.value) {
      if (appliedPids.value.has(row.pid)) continue
      diff.add(row.pid)
    }
    selDiff.value = diff
  } else {
    const next = new Set(part === 'add' ? selAdd.value : selRm.value)
    for (const row of tableRows.value) {
      if (appliedPids.value.has(row.pid)) continue
      for (const id of (part === 'add' ? row.add_tag_ids : row.remove_tag_ids) ?? []) {
        next.add(`${row.pid}:${id}`)
      }
    }
    if (part === 'add') selAdd.value = next
    else selRm.value = next
  }
  syncComboSelected()
}

function invertVisible() {
  if (jobKind.value === 'combo') {
    // 可见行三维取反（逐标签取反 + 难度维整行取反；无建议的行不凭空造勾；跳过已应用行）
    const add = new Set(selAdd.value)
    const rm = new Set(selRm.value)
    const diff = new Set(selDiff.value)
    for (const row of tableRows.value) {
      if (appliedPids.value.has(row.pid)) continue
      for (const id of row.add_tag_ids ?? []) {
        const k = `${row.pid}:${id}`
        if (add.has(k)) add.delete(k)
        else add.add(k)
      }
      for (const id of row.remove_tag_ids ?? []) {
        const k = `${row.pid}:${id}`
        if (rm.has(k)) rm.delete(k)
        else rm.add(k)
      }
      if (diff.has(row.pid)) diff.delete(row.pid)
      else diff.add(row.pid)
    }
    selAdd.value = add
    selRm.value = rm
    selDiff.value = diff
    syncComboSelected()
    return
  }
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

// 综合单标签勾选切换（v0.92.1 粒度）：同题各标签互相独立；
// selected 恒 = 难度行勾 ∪ 两维有勾题号并集（不变式，应用/计数口径）
function toggleKnowledge(pid: number, part: 'add' | 'rm', tagId: number, on: boolean) {
  const k = `${pid}:${tagId}`
  const next = new Set(part === 'add' ? selAdd.value : selRm.value)
  if (on) next.add(k)
  else next.delete(k)
  if (part === 'add') selAdd.value = next
  else selRm.value = next
  syncComboSelected()
}

// 综合难度维整行勾切：勾了才随应用写难度（finalDiff 改判值），不勾 = 难度不动
function toggleComboDiff(pid: number, on: boolean) {
  const next = new Set(selDiff.value)
  if (on) next.add(pid)
  else next.delete(pid)
  selDiff.value = next
  syncComboSelected()
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
  // background: true——worker 定时器驱动，切走标签页不受后台节流钳速（v0.71 生产实锤：
  // 后台标签 setTimeout 被钳到约 1 次/分钟，进度条看似卡死而服务端批次 0-10s/批）
}, 2000, { background: true })

function startPoll() {
  pollFails.value = 0
  poll.restart()
}

// 翻页/换每页条数：终态下按新条件拉取对应切片（轮询进行中由轮询自然带新页），
// 勾选清空重置（当前页独立语义）；换条数回第一页
watch(page, async () => {
  if (running.value) return
  const ok = await fetchStatus()
  if (ok && done.value) clearSelection()
})
watch(pageSize, async () => {
  if (running.value) return
  page.value = 1
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
  const items: TidyApplyItem[] = []
  for (const s of suggestions.value) {
    if (!selected.value.has(s.pid)) continue
    switch (s.kind) {
      case 'combo': {
        // 两维独立勾选：难度行勾才写难度（finalDiff 改判值），标签按逐标签勾选拼终值；
        // 两维全无勾 = 该行不应用（selected 并集不变式下不出现，守卫兜底）
        const item: TidyApplyItem = { pid: s.pid }
        if (selDiff.value.has(s.pid)) {
          item.difficulty = finalDiff.value.get(s.pid) ?? s.suggest_difficulty ?? 3
        }
        const add = new Set<number>()
        for (const id of s.add_tag_ids ?? []) {
          if (selAdd.value.has(`${s.pid}:${id}`)) add.add(id)
        }
        const rm = new Set<number>()
        for (const id of s.remove_tag_ids ?? []) {
          if (selRm.value.has(`${s.pid}:${id}`)) rm.add(id)
        }
        if (add.size || rm.size) {
          const set = new Set<number>(s.cur_tag_ids ?? [])
          for (const id of add) set.add(id)
          for (const id of rm) set.delete(id)
          item.tags = [...set]
        }
        if (item.difficulty === undefined && !item.tags) break
        items.push(item)
        break
      }
      case 'meta':
        items.push({ pid: s.pid, meta_tags: [...(s.suggest_meta_ids ?? [])] })
        break
    }
  }
  if (!items.length) return
  applying.value = true
  try {
    const r = await adminApi.tidyApply(jobKind.value, items)
    for (const it of items) appliedPids.value.add(it.pid)
    selected.value = new Set()
    selAdd.value = new Set()
    selRm.value = new Set()
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
// 结果属于「当次任务当次处置」，root 主动弃置后才回发起区；未应用的条数给确认）。
// v0.95.1 弃置同步清服务器任务槽（tidyDiscard）：否则进页恢复一拉终态快照，
// 已弃置的结果刷新即复活。
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
  try {
    await adminApi.tidyDiscard()
  } catch {
    // 拦截器已提示（running 态 409 等）；前端照常回发起卡
  }
  status.value = null
  selected.value = new Set()
  selAdd.value = new Set()
  selRm.value = new Set()
  selDiff.value = new Set()
  appliedPids.value = new Set()
  finalDiff.value = new Map()
}

// ---- 整理历史（v0.92）：批次列表 + 单批明细 ----

const HIST_PAGE_SIZE = 20
const histPage = ref(1)
const histList = ref<TidyHistoryBatch[]>([])
const histTotal = ref(0)
const histLoading = ref(false)
const histDetail = ref<TidyHistoryDetail | null>(null)
const detailLoading = ref(false)
const detailShow = ref(false)

const KIND_LABEL: Record<TidyKind, string> = { combo: '综合', difficulty: '难度', knowledge: '知识点', meta: '来源标签' }

async function loadHistory() {
  histLoading.value = true
  try {
    const r = await adminApi.tidyHistory(histPage.value, HIST_PAGE_SIZE)
    histList.value = r.list
    histTotal.value = r.total
  } catch {
    // 拦截器已提示
  } finally {
    histLoading.value = false
  }
}

async function openHistoryDetail(id: number) {
  detailShow.value = true
  detailLoading.value = true
  histDetail.value = null
  try {
    histDetail.value = await adminApi.tidyHistoryDetail(id)
  } catch {
    // 拦截器已提示
  } finally {
    detailLoading.value = false
  }
}

// 历史明细「变更」单元格：按批内 kinds 渲染 old→new（难度 D?、知识点/来源名字快照）；
// combo 批按 item 真带了哪个维的终值渲染对应段（只勾一维时另一维不渲染，旧值无变更义）
function histChange(row: { old_difficulty?: number; new_difficulty?: number; old_tag_names?: string[]; new_tag_names?: string[]; old_meta_names?: string[]; new_meta_names?: string[] }, kinds: TidyKind[]): string {
  const parts: string[] = []
  if (kinds.includes('combo')) {
    if (row.new_difficulty !== undefined) {
      parts.push(`难度 ${row.old_difficulty ? `D${row.old_difficulty}` : '未设置'} → ${row.new_difficulty ? `D${row.new_difficulty}` : '未设置'}`)
    }
    if (row.new_tag_names !== undefined) {
      parts.push(`知识点 ${(row.old_tag_names ?? []).join('、') || '无'} → ${(row.new_tag_names ?? []).join('、') || '清空'}`)
    }
    return parts.join('；')
  }
  if (kinds.includes('difficulty')) {
    parts.push(`难度 ${row.old_difficulty ? `D${row.old_difficulty}` : '未设置'} → ${row.new_difficulty ? `D${row.new_difficulty}` : '未设置'}`)
  }
  if (kinds.includes('knowledge')) {
    parts.push(`知识点 ${(row.old_tag_names ?? []).join('、') || '无'} → ${(row.new_tag_names ?? []).join('、') || '清空'}`)
  }
  if (kinds.includes('meta')) {
    parts.push(`来源 ${(row.old_meta_names ?? []).join('、') || '无'} → ${(row.new_meta_names ?? []).join('、') || '清空'}`)
  }
  return parts.join('；')
}

// 切到历史模式即拉列表（apply 后再进来自动看到新批次）
watch(mode, (m) => {
  if (m === 'history') {
    histPage.value = 1
    void loadHistory()
  }
})

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

    <!-- 发起区：模式卡（AI整理 / 手动整理 / 整理历史）+ AI 三类任务卡。任务进行中或已有
         结果待处置时收起（结果属于「当次任务当次处置」，弃置结果后回到这里） -->
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
      <!-- 整理历史（v0.92）：批次列表 + 点开逐题明细 -->
      <template v-if="mode === 'history'">
        <el-table :data="histList" size="small" v-loading="histLoading">
          <el-table-column prop="id" label="批次" width="70" />
          <el-table-column label="时间" width="170">
            <template #default="{ row }">{{ new Date(row.at).toLocaleString() }}</template>
          </el-table-column>
          <el-table-column prop="by_name" label="操作者" width="100" />
          <el-table-column label="模式" width="96">
            <template #default="{ row }">
              <el-tag size="small" :type="row.mode === 'ai' ? 'primary' : 'warning'">{{ row.mode === 'ai' ? 'AI确认' : '手动' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="类型" min-width="120">
            <template #default="{ row }">{{ row.kinds.map((k: TidyKind) => KIND_LABEL[k]).join(' + ') }}</template>
          </el-table-column>
          <el-table-column label="范围" width="130">
            <template #default="{ row }">
              <span v-if="row.from || row.to" class="mono">{{ row.from || '起' }}~{{ row.to || '止' }}</span>
              <span v-else class="muted">全库</span>
            </template>
          </el-table-column>
          <el-table-column label="应用 / 失败" width="110">
            <template #default="{ row }">
              <span class="mono">{{ row.applied }}</span>
              <span v-if="row.failed" class="fail-n"> / {{ row.failed }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="80">
            <template #default="{ row }">
              <el-button size="small" link type="primary" @click="openHistoryDetail(row.id)">明细</el-button>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="histTotal > HIST_PAGE_SIZE" class="pager-row">
          <el-pagination
            background
            layout="prev, pager, next, total"
            :total="histTotal"
            :page-size="HIST_PAGE_SIZE"
            :current-page="histPage"
            @current-change="(p: number) => { histPage = p; void loadHistory() }"
          />
        </div>
        <el-empty v-if="!histLoading && !histTotal" description="还没有整理记录——AI 确认应用或手动整理保存后自动留痕" :image-size="72" />

        <!-- 单批明细：逐题 old→new（名字快照，树改版不影响回看） -->
        <el-dialog v-model="detailShow" :title="histDetail ? `批次 #${histDetail.id} · ${histDetail.by_name} · ${new Date(histDetail.at).toLocaleString()}` : '批次明细'" width="760px">
          <el-table v-if="histDetail" v-loading="detailLoading" :data="histDetail.items" size="small" max-height="60vh">
            <el-table-column prop="pid" label="题号" width="76" />
            <el-table-column label="标题" min-width="160" show-overflow-tooltip>
              <template #default="{ row }">
                <router-link :to="`/problems/${row.pid}`" target="_blank" class="tlink">{{ row.title }}</router-link>
              </template>
            </el-table-column>
            <el-table-column label="变更" min-width="240" show-overflow-tooltip>
              <template #default="{ row }">{{ histChange(row, histDetail!.kinds) }}</template>
            </el-table-column>
            <el-table-column label="结果" width="150" show-overflow-tooltip>
              <template #default="{ row }">
                <el-tag v-if="row.ok" size="small" type="success">成功</el-tag>
                <el-tag v-else size="small" type="danger">失败：{{ row.err }}</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-dialog>
      </template>
      <template v-else-if="mode === 'ai'">
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
          <TipLabel tip="默认只整理未整理过的（已应用的自动排除，从后继续）；可指定题号范围只扫部分题；只产建议不写库，勾选确认后才应用" />
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
        <span class="muted">
          已勾选 <b>{{ selectedCount }}</b>/{{ suggestionTotal }} 题{{ pageLabel }}
          <template v-if="jobKind === 'combo' && (selAdd.size || selRm.size)"> · 新增 {{ selAdd.size }} · 移除 {{ selRm.size }}</template>
          <template v-if="status?.excluded"> · 已排除 {{ status.excluded }}</template>
          <template v-if="appliedPids.size"> · 已应用 {{ appliedPids.size }}</template>
          <template v-if="rangeLabel"> · {{ rangeLabel }}</template>
        </span>
        <span class="spacer" />
        <!-- 综合全选拆三枚（v0.94.1）：难度/新增/移除三维各自独立，逐项补勾可见行 -->
        <template v-if="jobKind === 'combo'">
          <el-button size="small" @click="selectAllCombo('diff')">全选难度</el-button>
          <el-button size="small" @click="selectAllCombo('add')">全选新增</el-button>
          <el-button size="small" @click="selectAllCombo('rm')">全选移除</el-button>
        </template>
        <el-button v-else size="small" @click="selectAllVisible">全选可见</el-button>
        <el-button size="small" @click="invertVisible">反选</el-button>
        <el-button type="primary" :loading="applying" :disabled="!selectedCount" @click="applySelected">
          应用勾选（{{ selectedCount }}）
        </el-button>
      </div>

      <!-- 综合（v0.94）：一行两维——难度行勾（未设难度默认勾）+「现难度 → 建议难度（可
           改判 LazySelectCell）」合列 + 知识点「现 / ＋新增勾选 / －移除勾选」三行合列
          （v0.94.1 合列去横向滚动；联动为后台机制，前端不再显示「联动」字样）；
           无 el-table selection 列（难度维自持 selDiff 集合，与逐标签勾的并集语义不打架） -->
      <el-table
        v-if="jobKind === 'combo'"
        ref="tableRef"
        :data="tableRows"
        size="small"
      >
        <el-table-column width="42" label="">
          <template #header>
            <el-tooltip content="勾选 = 应用时写难度（建议值或改判值）" placement="top">
              <span class="muted">难度</span>
            </el-tooltip>
          </template>
          <template #default="{ row }">
            <el-checkbox
              :model-value="selDiff.has((row as TidySuggestion).pid)"
              :disabled="appliedPids.has(row.pid) || !!row.ai_error"
              @update:model-value="(v: boolean | string | number) => toggleComboDiff((row as TidySuggestion).pid, v === true)"
            />
          </template>
        </el-table-column>
        <el-table-column prop="pid" label="题号" width="70" />
        <el-table-column label="标题" min-width="130" show-overflow-tooltip>
          <template #default="{ row }">
            <router-link :to="`/problems/${row.pid}`" target="_blank" class="tlink">{{ row.title }}</router-link>
          </template>
        </el-table-column>
        <el-table-column label="难度（现 → 建议）" min-width="168">
          <template #default="{ row }">
            <span class="diff-cell">
              <span v-if="row.has_difficulty" class="mono">D{{ row.cur_difficulty }}</span>
              <el-tag v-else size="small" type="info">未设置</el-tag>
              <span class="muted">→</span>
              <!-- AI 失败行无建议：置灰提示，勾选后仍可改判（v0.63 惰性单元格编辑） -->
              <LazySelectCell
                v-if="!row.ai_error"
                :model-value="finalDiff.get(row.pid) ?? row.suggest_difficulty ?? 3"
                :options="diffOptions"
                :readonly="appliedPids.has(row.pid)"
                empty="D3"
                @update:model-value="(v: number | number[]) => finalDiff.set((row as TidySuggestion).pid, v as number)"
              />
              <span v-else class="muted">—</span>
            </span>
          </template>
        </el-table-column>
        <el-table-column label="知识点（现 / ＋新增 / －移除）" min-width="320">
          <template #default="{ row }">
            <div class="kp-cell">
              <div class="kp-line">
                <span class="kp-label">现</span>
                <span class="kp-plain">{{ (row.cur_tag_names ?? []).join('、') || '—' }}</span>
              </div>
              <div v-if="row.add_tag_names?.length" class="kp-line">
                <span class="kp-label kp-add">＋</span>
                <span class="part-cell">
                  <span v-for="(n, i) in row.add_tag_names" :key="n" class="tag-pick">
                    <el-checkbox
                      class="part-check"
                      :model-value="selAdd.has(`${(row as TidySuggestion).pid}:${row.add_tag_ids?.[i]}`)"
                      :disabled="appliedPids.has(row.pid)"
                      @update:model-value="(v: boolean | string | number) => toggleKnowledge((row as TidySuggestion).pid, 'add', row.add_tag_ids?.[i] as number, v === true)"
                    />
                    <el-tag size="small" type="success">{{ n }}</el-tag>
                  </span>
                </span>
              </div>
              <div v-if="row.remove_tag_names?.length" class="kp-line">
                <span class="kp-label kp-rm">－</span>
                <span class="part-cell">
                  <span v-for="(n, i) in row.remove_tag_names" :key="n" class="tag-pick">
                    <el-checkbox
                      class="part-check"
                      :model-value="selRm.has(`${(row as TidySuggestion).pid}:${row.remove_tag_ids?.[i]}`)"
                      :disabled="appliedPids.has(row.pid)"
                      @update:model-value="(v: boolean | string | number) => toggleKnowledge((row as TidySuggestion).pid, 'rm', row.remove_tag_ids?.[i] as number, v === true)"
                    />
                    <el-tag size="small" type="danger">{{ n }}</el-tag>
                  </span>
                </span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="丢弃 / 备注" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="row.ai_error" class="warn-text">{{ row.ai_error }}</span>
            <span v-else-if="row.dropped?.length">丢弃：{{ row.dropped.join('；') }}</span>
            <span v-else-if="row.note" class="muted">{{ row.note }}</span>
          </template>
        </el-table-column>
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
        <el-table-column label="标题" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <router-link :to="`/problems/${row.pid}`" target="_blank" class="tlink">{{ row.title }}</router-link>
          </template>
        </el-table-column>
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

      <!-- 分页器（v0.49 服务端分页；v0.94.1 每页条数可调）：只拉当前页切片，翻页/换条数勾选清空（当前页独立语义） -->
      <div v-if="suggestionTotal > pageSizeOptions[0]" class="pager-row">
        <el-pagination
          background
          layout="sizes, prev, pager, next, total"
          :total="suggestionTotal"
          :page-sizes="pageSizeOptions"
          :page-size="pageSize"
          :current-page="page"
          @current-change="(p: number) => (page = p)"
          @size-change="(s: number) => (pageSize = s)"
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
        {{ jobKind === 'combo' ? '这些题难度与知识点都已应用过整理' : '这些题已应用过整理' }}，默认不再进任务。
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
  grid-template-columns: repeat(2, 1fr);
  gap: var(--soj-sp-12);
}

/* 综合表合列（v0.94.1 去横向滚动）：难度「现→建议」同行排布；知识点「现/＋/－」三行堆叠 */
.diff-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-8);
}

.kp-cell {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-4);
  min-width: 0;
}

.kp-line {
  display: flex;
  align-items: flex-start;
  gap: var(--soj-sp-4);
}

.kp-label {
  flex: none;
  width: 16px;
  font-weight: var(--soj-fw-bold);
  text-align: center;
}

.kp-add {
  color: var(--el-color-success);
}

.kp-rm {
  color: var(--el-color-danger);
}

.kp-plain {
  min-width: 0;
}

/* 模式卡（v0.49 双模式；v0.92 三卡）：AI整理 / 手动整理 / 整理历史，嵌在任务卡上方 */
.mode-cards {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
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

/* 逐标签勾选格（v0.92.1 细化 v0.68）：勾选框+标签成对内联、对间换行；
   checkbox 压到 small tag 高（20px，checkbox 默认 32px 偏高） */
.part-cell {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--soj-sp-4) var(--soj-sp-10);
}

.tag-pick {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-4);
}

.part-check {
  height: 20px;
}

.warn-text {
  color: var(--el-color-warning);
}

/* 历史列表失败计数（应用 / 失败 的失败段） */
.fail-n {
  color: var(--el-color-danger);
  font-family: var(--font-mono);
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

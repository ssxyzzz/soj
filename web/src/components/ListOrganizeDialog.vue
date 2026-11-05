<script setup lang="ts">
// 题单 AI 一键整理弹窗（v0.71）：三段式——配置（范围筛选 + 已挂题处理 radio）→
// 进行中（usePolling 2s 轻量 status 轮询 + 取消）→ 预览（叶分组勾选 + 未匹配清单 +
// 结果条 → 应用写编辑草稿）。两段式铁律同 TidyPanel：任务只产建议，勾选采纳、
// 应用进 chs 草稿，保存仍由大纲页整体 PATCH——本弹窗不直接写库。
// 应用语义（用户拍板「已挂题可选」）：
//   保持原挂载（默认）= 各叶已有不动，勾选新题按难度升序追加；
//   纳入 AI 重排 = 叶内全部按难度升序重排（未设难度殿后、同难按题号），未勾选的
//   已挂题保持原挂载殿后——勾选 = 采纳建议，不勾不落。
// 叶口径与后端一致：章无小节 = 章叶（ch:i），否则小节叶（sec:i-j）。
// 弹窗重开时 status 探测：running 续看进度；done/cancelled 提示可查看上次结果。
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import TagFilter from '@/components/problem/TagFilter.vue'
import TierBadge from '@/components/TierBadge.vue'
import { listApi } from '@/api'
import { usePolling } from '@/composables/usePolling'
import { draftToChapters } from '@/utils/listDraft'
import type { ListEntryDraft, ListOrganizeSuggest, ListOrganizeStatus } from '@/types'

const props = defineProps<{
  modelValue: boolean
  listId: number
  chs: ListEntryDraft[] // 编辑草稿引用（应用时直接改）
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
}>()

type Step = 'config' | 'running' | 'preview' | 'failed'
const step = ref<Step>('config')
const includeMounted = ref(false)
const diffSel = ref<number[]>([])
const tagSel = ref<number[]>([])
const metaSel = ref<number[]>([])
const rangeFrom = ref<number | null>(null) // 题号范围（含边界，留空 = 不限；同题库整理口径）
const rangeTo = ref<number | null>(null)
const status = ref<ListOrganizeStatus | null>(null)
const suggest = ref<ListOrganizeSuggest | null>(null)
const hasLast = ref(false) // 打开时探测到上次终态任务（提示可查看）
const sel = ref(new Set<number>())
const folded = ref(new Set<string>())
const unmatchedOpen = ref(false)

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
})

const percent = computed(() => {
  const s = status.value
  if (!s || !s.total) return 0
  return Math.min(100, Math.round(((s.done ?? 0) / s.total) * 100))
})

// 任务边界标签（进行中提示用；服务端快照 from/to，重开弹窗也能记得范围）
const rangeLabel = computed(() => {
  const f = status.value?.from ?? 0
  const t = status.value?.to ?? 0
  if (f > 0 && t > 0) return `题号 ${f}–${t}`
  if (f > 0) return `题号 ${f}+`
  if (t > 0) return `题号 ≤${t}`
  return ''
})

const allPids = computed(() => {
  const ids: number[] = []
  for (const g of suggest.value?.groups ?? []) for (const it of g.items ?? []) ids.push(it.pid)
  return ids
})

const selCount = computed(() => sel.value.size)

function toggleDiff(d: number) {
  diffSel.value = diffSel.value.includes(d) ? diffSel.value.filter((x) => x !== d) : [...diffSel.value, d]
}

// ---- 叶子定位与排序（叶 key 与后端 assemble 口径一致） ----

function leafOf(key: string): ListEntryDraft | null {
  const m = /^ch:(\d+)$/.exec(key)
  if (m) {
    const ch = props.chs[Number(m[1])]
    return ch && !ch.children.length ? ch : null
  }
  const s = /^sec:(\d+)-(\d+)$/.exec(key)
  if (s) return props.chs[Number(s[1])]?.children[Number(s[2])] ?? null
  return null
}

// 排序键：难度升序、未设难度（0/越界）殿后、同难按题号（与后端 assemble 同口径）
function diffKey(d: number): number {
  return d >= 1 && d <= 10 ? d : 99
}

function byDiffAsc(a: { pid: number; difficulty: number }, b: { pid: number; difficulty: number }) {
  return diffKey(a.difficulty) - diffKey(b.difficulty) || a.pid - b.pid
}

// ---- 打开/关闭 ----

watch(
  () => props.modelValue,
  (v) => {
    if (!v) {
      poll.stop()
      return
    }
    reset()
    // 打开即探测：本列表任务进行中 → 续看进度；上次终态 → 提示可查看
    void listApi
      .organizeStatus(props.listId)
      .then((st) => {
        status.value = st
        if (st.state === 'running') {
          step.value = 'running'
          poll.restart()
        } else if (st.state === 'done' || st.state === 'cancelled') {
          hasLast.value = true
        }
      })
      .catch(() => {})
  },
)

function reset() {
  step.value = 'config'
  status.value = null
  suggest.value = null
  hasLast.value = false
  sel.value = new Set()
  folded.value = new Set()
  unmatchedOpen.value = false
  failMsg.value = ''
}

function close() {
  emit('update:modelValue', false)
}

// ---- 轮询（轻量 status；建议不进轮询响应，终态后一次性拉） ----

const failMsg = ref('')

const poll = usePolling(
  async () => {
    const st = await listApi.organizeStatus(props.listId)
    status.value = st
    if (st.state === 'running') return true
    // 终态必停轮：预览装载失败也要落到 failed 面板可见报错，不得抛给轮询吞掉续转
    try {
      if (st.state === 'done' || st.state === 'cancelled') {
        await loadPreview()
      } else {
        failMsg.value = st.error || '任务异常终止'
        step.value = 'failed'
      }
    } catch (e) {
      console.error('[list-organize] 预览装载失败', e)
      failMsg.value = '读取整理结果失败：' + (e instanceof Error ? e.message : '')
      step.value = 'failed'
    }
    return false
  },
  2000,
  {
    immediate: true,
    background: true, // worker 定时器驱动：切走/最小化不受后台节流钳速（v0.71 生产实锤）
    maxDurationMs: 30 * 60_000,
    onExpire: () => {
      failMsg.value = '等待超时，请稍后重试'
      step.value = 'failed'
    },
  },
)

// ---- 发起 ----

async function run() {
  if (!props.chs.length) return
  await listApi.organizeRun(props.listId, {
    chapters: draftToChapters(props.chs),
    include_mounted: includeMounted.value,
    from: rangeFrom.value ?? undefined,
    to: rangeTo.value ?? undefined,
    difficulty: diffSel.value.length ? diffSel.value : undefined,
    tags: tagSel.value.length ? tagSel.value : undefined,
    meta: metaSel.value.length ? metaSel.value : undefined,
  })
  hasLast.value = false
  step.value = 'running'
  poll.restart()
}

async function cancelTask() {
  await listApi.organizeCancel(props.listId)
}

// ---- 预览 ----

async function loadPreview() {
  const s = await listApi.organizeSuggestions(props.listId)
  suggest.value = s
  sel.value = new Set(allPids.value) // 默认全勾（应用进草稿可再手动修剪）
  folded.value = new Set((s.groups ?? []).filter((g) => !(g.items ?? []).length).map((g) => g.key))
  unmatchedOpen.value = false
  step.value = 'preview'
}

function toggleGroup(key: string) {
  const next = new Set(folded.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  folded.value = next
}

function toggleSel(pid: number) {
  const next = new Set(sel.value)
  if (next.has(pid)) next.delete(pid)
  else next.add(pid)
  sel.value = next
}

function selectAll() {
  sel.value = new Set(allPids.value)
}

function invertSel() {
  const next = new Set<number>()
  for (const pid of allPids.value) if (!sel.value.has(pid)) next.add(pid)
  sel.value = next
}

// ---- 应用到大纲草稿（不写库；保存走大纲页整体 PATCH） ----

// 全草稿挂载索引（应用前防重）：任何叶已挂的题不再追加——同一批建议被二次应用
//（如「查看上次结果」重放、先后两批建议落点不同）不再产生同节/跨节双挂
//（v0.71.2 生产实锤：建议集未消费 + applyAppend 纯 push，整单每题两遍）
function mountIndexOf(): Set<number> {
  const s = new Set<number>()
  for (const ch of props.chs) {
    for (const p of ch.problems) s.add(p.id)
    for (const sec of ch.children) for (const p of sec.problems) s.add(p.id)
  }
  return s
}

function apply() {
  const s = suggest.value
  if (!s || !sel.value.size) return
  let skipped = 0
  if (includeMounted.value) applyRearrange(s)
  else skipped = applyAppend(s)
  // 建议集消费：应用后不再提供「查看上次结果」重放同一批建议（防重复应用）
  const applied = sel.value.size
  suggest.value = null
  hasLast.value = false
  sel.value = new Set()
  close()
  ElMessage.success(
    `已按整理结果更新大纲草稿（${applied} 题${skipped ? `，跳过已挂载 ${skipped} 题` : ''}），核对后点「保存」生效`,
  )
}

// 保持原挂载：各叶已有不动，勾选的新题按难度升序追加在末尾；已在草稿任何叶挂载的
// 建议跳过（返回跳过数）
function applyAppend(s: ListOrganizeSuggest): number {
  const mounted = mountIndexOf()
  let skipped = 0
  for (const g of s.groups) {
    const node = leafOf(g.key)
    if (!node) continue
    for (const it of (g.items ?? []).filter((it) => sel.value.has(it.pid)).sort(byDiffAsc)) {
      if (mounted.has(it.pid)) {
        skipped++
        continue
      }
      node.problems.push({ id: it.pid, title: it.title, hidden: it.hidden || undefined })
      mounted.add(it.pid) // 防御：建议集内异常重复也只落一次
    }
  }
  return skipped
}

// 纳入 AI 重排：叶内 = [该叶勾选建议（含从别叶挪来的已挂题），难度升序] +
// [未勾选的已挂题，保持原序殿后]——勾选 = 采纳建议，不勾不落（原地保留）。
// 遍历全部叶而不只搬入叶：被挪走原题的源叶（本叶无任何搬入）也要清掉 dest 题，
// 否则同题跨节双挂（v0.71 走查实锤 #1003 双挂）；叶 key 与后端 assemble 口径一致。
function applyRearrange(s: ListOrganizeSuggest) {
  const diffOf = new Map<number, number>()
  const dest = new Map<number, { key: string; title: string; hidden?: boolean }>()
  for (const g of s.groups) {
    for (const it of g.items ?? []) {
      diffOf.set(it.pid, it.difficulty)
      if (sel.value.has(it.pid)) dest.set(it.pid, { key: g.key, title: it.title, hidden: it.hidden || undefined })
    }
  }
  // 组装即对齐 PickedProblem 形状（{id,title,hidden}）——重排条目直接进草稿，形状错了保存就写废挂载
  const byLeaf = new Map<string, { id: number; title: string; hidden?: boolean }[]>()
  for (const [pid, d] of dest) {
    const arr = byLeaf.get(d.key) ?? []
    arr.push({ id: pid, title: d.title, hidden: d.hidden })
    byLeaf.set(d.key, arr)
  }
  // 全部叶统一重写：搬入序在前（难度升序），未勾选已挂题殿后，源叶残留的 dest 题一并清除
  props.chs.forEach((ch, ci) => {
    if (ch.children.length) {
      ch.children.forEach((sec, si) => rewriteLeaf(`sec:${ci}-${si}`, sec, byLeaf, dest, diffOf))
    } else {
      rewriteLeaf(`ch:${ci}`, ch, byLeaf, dest, diffOf)
    }
  })
}

function rewriteLeaf(
  key: string,
  node: ListEntryDraft,
  byLeaf: Map<string, { id: number; title: string; hidden?: boolean }[]>,
  dest: Map<number, unknown>,
  diffOf: Map<number, number>,
) {
  const moved = byLeaf.get(key) ?? []
  moved.sort((a, b) => diffKey(diffOf.get(a.id) ?? 0) - diffKey(diffOf.get(b.id) ?? 0) || a.id - b.id)
  const keep = node.problems.filter((p) => !dest.has(p.id))
  node.problems = [...moved, ...keep]
}
</script>

<template>
  <el-dialog
    v-model="visible"
    title="AI 整理到题单"
    width="1080px"
    top="4vh"
    @update:model-value="(v: boolean) => !v && close()"
  >
    <!-- ① 配置 -->
    <div v-if="step === 'config'" class="lo-config">
      <p class="lo-lead dim">
        AI 会结合题面与章节标题/描述的语义，把题库题目归入下方的章/小节（难度、知识点、来源标签作参考），
        节内按难度升序排布。整理只产建议，预览勾选确认后才写入大纲草稿，点「保存」前可随时反悔。
      </p>

      <div v-if="hasLast" class="lo-last">
        <span>上一次整理已完成</span>
        <el-button link type="primary" @click="loadPreview">查看上次结果</el-button>
      </div>

      <div class="lo-block">
        <div class="fl">范围（不选 = 全库未隐藏未重复题）</div>
        <div class="lo-chips">
          <TierBadge
            v-for="d in 10"
            :key="d"
            :tier="d"
            kind="difficulty"
            size="md"
            class="lo-chip"
            :class="{ sel: diffSel.includes(d) }"
            @click="toggleDiff(d)"
          />
          <el-button v-if="diffSel.length" link type="primary" @click="diffSel = []">清空</el-button>
        </div>
        <div class="lo-range">
          <span class="muted">题号</span>
          <el-input-number v-model="rangeFrom" :min="1" :controls="false" placeholder="起始" class="lo-range-input" />
          <span class="muted">~</span>
          <el-input-number v-model="rangeTo" :min="1" :controls="false" placeholder="结束" class="lo-range-input" />
          <span class="muted">留空 = 不限（含边界）</span>
        </div>
        <div class="lo-filters">
          <TagFilter :selected="tagSel" @change="(ids: number[]) => (tagSel = ids)" />
          <TagFilter tree="meta" :selected="metaSel" @change="(ids: number[]) => (metaSel = ids)" />
        </div>
      </div>

      <div class="lo-block">
        <div class="fl">已挂题目</div>
        <el-radio-group v-model="includeMounted">
          <el-radio :value="false">保持原挂载，只加新题</el-radio>
          <el-radio :value="true">纳入 AI 重排（全单按难度重排）</el-radio>
        </el-radio-group>
        <p class="lo-hint dim">
          {{ includeMounted ? '已挂题（含隐藏/重复）全部进入 AI 重排；不勾选的建议题保持原挂载不动。' : '已挂的题一律不动，只把题库里未挂的新题归入章/节。' }}
        </p>
      </div>
    </div>

    <!-- ② 进行中 -->
    <div v-else-if="step === 'running'" class="lo-running">
      <el-progress :percentage="percent" :stroke-width="14" striped striped-flow class="lo-progress" />
      <p class="dim">
        正在归类 {{ status?.done ?? 0 }} / {{ status?.total ?? 0 }} 题<template v-if="rangeLabel"> · {{ rangeLabel }}</template>（AI 批量处理，大题单约需数分钟，可切走稍后回来看）
      </p>
      <p v-if="status?.failed_batches" class="lo-warn">
        已有 {{ status.failed_batches }} 批调用失败，相关题将计入未匹配
      </p>
    </div>

    <!-- ③ 预览 -->
    <div v-else-if="step === 'preview' && suggest" class="lo-preview">
      <div class="lo-result-bar">
        <span class="dim">已勾选 {{ selCount }} / {{ allPids.length }} 题</span>
        <span class="lo-bar-btns">
          <el-button size="small" @click="selectAll">全选</el-button>
          <el-button size="small" @click="invertSel">反选</el-button>
        </span>
      </div>
      <p v-if="suggest.state === 'cancelled'" class="lo-warn">任务已取消，以下为已完成部分，可直接采纳或重新发起</p>
      <p v-if="suggest.failed_batches" class="lo-warn">
        有 {{ suggest.failed_batches }} 批 AI 调用失败，相关题已计入未匹配（可重新发起整理）
      </p>

      <div class="lo-groups">
        <div v-for="g in suggest.groups" :key="g.key" class="lo-group">
          <button type="button" class="lo-group-head" @click="toggleGroup(g.key)">
            <span class="tri">{{ folded.has(g.key) ? '▸' : '▾' }}</span>
            <span class="g-label">{{ g.label }}</span>
            <span class="g-count dim">{{ (g.items ?? []).length }} 题匹配</span>
          </button>
          <div v-if="!folded.has(g.key)" class="lo-rows">
            <div
              v-for="it in g.items ?? []"
              :key="it.pid"
              class="lo-row"
              :class="{ sel: sel.has(it.pid) }"
              @click="toggleSel(it.pid)"
            >
              <el-checkbox :model-value="sel.has(it.pid)" @click.stop @change="toggleSel(it.pid)" />
              <span class="lo-title">#{{ it.pid }} {{ it.title }}</span>
              <span v-if="it.mounted" class="mounted-tag">已挂</span>
              <el-tag v-if="it.hidden" size="small" type="danger">隐藏题</el-tag>
              <TierBadge v-if="it.difficulty" :tier="it.difficulty" kind="difficulty" />
            </div>
          </div>
        </div>

        <!-- 未匹配：置底默认折叠 -->
        <div class="lo-group unmatched">
          <button type="button" class="lo-group-head" @click="unmatchedOpen = !unmatchedOpen">
            <span class="tri">{{ unmatchedOpen ? '▾' : '▸' }}</span>
            <span class="g-label">未匹配</span>
            <span class="g-count dim">{{ suggest.unmatched.length }} 题（不自动放入）</span>
          </button>
          <div v-if="unmatchedOpen" class="lo-rows">
            <div v-for="it in suggest.unmatched" :key="it.pid" class="lo-row plain">
              <span class="lo-title">#{{ it.pid }} {{ it.title }}</span>
              <span v-if="it.mounted" class="mounted-tag">已挂</span>
              <TierBadge v-if="it.difficulty" :tier="it.difficulty" kind="difficulty" />
            </div>
            <p v-if="!suggest.unmatched.length" class="dim lo-none">全部题目都有归属</p>
          </div>
        </div>
      </div>
    </div>

    <!-- ④ 失败 -->
    <div v-else-if="step === 'failed'" class="lo-failed">
      <p class="lo-warn">{{ failMsg || '整理任务失败' }}</p>
    </div>

    <template #footer>
      <template v-if="step === 'config'">
        <el-button @click="close">取消</el-button>
        <el-button type="primary" :disabled="!chs.length" @click="run">开始整理</el-button>
      </template>
      <template v-else-if="step === 'running'">
        <el-button type="warning" plain @click="cancelTask">取消任务</el-button>
      </template>
      <template v-else-if="step === 'preview'">
        <el-button @click="step = 'config'">重新配置</el-button>
        <el-button type="primary" :disabled="!selCount" @click="apply">
          应用到大纲（{{ selCount }}）
        </el-button>
      </template>
      <template v-else>
        <el-button type="primary" @click="step = 'config'">返回配置</el-button>
      </template>
    </template>
  </el-dialog>
</template>

<style scoped>
.lo-lead {
  margin: 0 0 var(--soj-sp-12);
  line-height: 1.7;
}

.lo-last {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: var(--soj-sp-8) var(--soj-sp-12);
  margin-bottom: var(--soj-sp-12);
  background: var(--el-color-primary-light-9);
  border-radius: var(--el-border-radius-base);
}

.lo-block {
  margin-bottom: var(--soj-sp-16);
}

.fl {
  font-weight: var(--soj-fw-medium);
  margin-bottom: var(--soj-sp-6);
}

.lo-chips {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--soj-sp-6);
  margin-bottom: var(--soj-sp-8);
}

.lo-chip {
  cursor: pointer;
  opacity: 0.45;
}

.lo-chip.sel {
  opacity: 1;
  outline: 2px solid var(--el-color-primary);
}

.lo-range {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-bottom: var(--soj-sp-10);
}

.lo-range-input {
  width: 120px;
}

.lo-filters {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-8);
}

.lo-hint {
  margin: var(--soj-sp-6) 0 0;
  font-size: var(--soj-fs-13);
}

.lo-running {
  padding: var(--soj-sp-24) var(--soj-sp-8);
  text-align: center;
}

.lo-progress {
  max-width: 560px;
  margin: 0 auto var(--soj-sp-12);
}

.lo-warn {
  color: var(--el-color-warning);
  font-size: var(--soj-fs-13);
  margin: var(--soj-sp-6) 0;
}

.lo-result-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--soj-sp-10);
}

.lo-groups {
  max-height: 52vh;
  overflow-y: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
}

.lo-group {
  border-bottom: 1px solid var(--el-fill-color);
}

.lo-group:last-child {
  border-bottom: none;
}

.lo-group-head {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  width: 100%;
  padding: var(--soj-sp-8) var(--soj-sp-12);
  border: none;
  background: var(--el-fill-color-lighter);
  cursor: pointer;
  text-align: left;
}

.lo-group-head:hover {
  background: var(--el-fill-color-light);
}

.tri {
  color: var(--el-text-color-secondary);
  width: 12px;
  flex: none;
}

.g-label {
  font-weight: var(--soj-fw-medium);
  color: var(--soj-ink);
}

.g-count {
  margin-left: auto;
  font-size: var(--soj-fs-13);
}

.lo-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  padding: var(--soj-sp-8) var(--soj-sp-12);
  border-bottom: 1px solid var(--el-fill-color);
  cursor: pointer;
}

.lo-row:hover {
  background: var(--el-fill-color-light);
}

.lo-row.sel {
  background: var(--el-color-primary-light-9);
}

.lo-row.plain {
  cursor: default;
}

.lo-row.plain:hover {
  background: transparent;
}

.lo-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mounted-tag {
  flex: none;
  font-size: var(--soj-fs-12);
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  border-radius: 999px;
  padding: var(--soj-sp-2) var(--soj-sp-8);
}

.lo-none {
  padding: var(--soj-sp-8) var(--soj-sp-12);
}

.lo-failed {
  padding: var(--soj-sp-16) 0;
  text-align: center;
}
</style>

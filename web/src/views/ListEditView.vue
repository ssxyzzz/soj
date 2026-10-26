<script setup lang="ts">
// 题单编辑器（v0.18.3，plan §2.12）：左树右题双栏——
// 左：章节树（章/节增删改、上下移与拖拽排序，标题/描述就地编辑）；
// 右：选中节点的题目列表（ProblemPicker 加题 + 拖拽排序，ProblemPicker 自带）。
// 单章不混挂（拍板 3）：有子节的章不再收题、有题的章不再加节；保存整体 PATCH，
// 服务端 ValidateListShape 兜底（深度 ≤2、不混挂、标题 1-32 字、描述 ≤128 字）。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import MdEditor from '@/components/MdEditor.vue'
import ProblemPicker from '@/components/ProblemPicker.vue'
import RailCard from '@/components/RailCard.vue'
import SwitchRow from '@/components/SwitchRow.vue'
import { listApi } from '@/api'
import type { ListDetail, ListEntryDraft } from '@/types'

const route = useRoute()
const router = useRouter()

const id = Number(route.params.id)
const title = ref('')
const intro = ref('')
const hidden = ref(false)
const chs = ref<ListEntryDraft[]>([])
const selected = ref<string | null>(null) // 'ch:i' | 'sec:i-j'
const loading = ref(true)
const saving = ref(false)

function fromDetail(d: ListDetail): ListEntryDraft[] {
  return d.chapters.map((c) => ({
    title: c.title,
    desc: c.desc ?? '',
    problems: c.problems.map((e) => ({ id: e.pid, title: e.title, hidden: e.hidden })),
    children: c.sections.map((s) => ({
      title: s.title,
      desc: s.desc ?? '',
      problems: s.problems.map((e) => ({ id: e.pid, title: e.title, hidden: e.hidden })),
      children: [],
    })),
  }))
}

onMounted(async () => {
  try {
    const d = await listApi.detail(id)
    if (!d.manageable) {
      ElMessage.warning('只有题单创建者可以编辑')
      router.replace(`/lists/${id}`)
      return
    }
    title.value = d.title
    intro.value = d.intro
    hidden.value = !!d.hidden
    chs.value = fromDetail(d)
  } finally {
    loading.value = false
  }
})

// ---- 选中节点 ----
function secOf(key: string): { ci: number; si: number } | null {
  const m = /^sec:(\d+)-(\d+)$/.exec(key)
  return m ? { ci: Number(m[1]), si: Number(m[2]) } : null
}
const selectedNode = computed<ListEntryDraft | null>(() => {
  if (!selected.value) return null
  if (selected.value.startsWith('ch:')) {
    const ci = Number(selected.value.slice(3))
    return chs.value[ci] ?? null
  }
  const pos = secOf(selected.value)
  return pos ? (chs.value[pos.ci]?.children[pos.si] ?? null) : null
})
const selectedIsChapter = computed(() => !!selected.value?.startsWith('ch:'))
// 选中的章有子节时不可收题（单章不混挂）
const selectedChapterHasChildren = computed(() => {
  if (!selectedIsChapter.value || !selected.value) return false
  return (chs.value[Number(selected.value.slice(3))]?.children.length ?? 0) > 0
})

function select(key: string) {
  selected.value = key
}

// ---- 章节操作 ----
function addChapter() {
  chs.value.push({ title: `第${chs.value.length + 1}章`, desc: '', problems: [], children: [] })
  select(`ch:${chs.value.length - 1}`)
}

function addSection(ci: number) {
  const ch = chs.value[ci]
  if (!ch || ch.problems.length) return // 有直挂题的章不再加节（不混挂）
  ch.children.push({ title: `${ci + 1}.${ch.children.length + 1} 小节`, desc: '', problems: [], children: [] })
  select(`sec:${ci}-${ch.children.length - 1}`)
}

function nodeStat(n: ListEntryDraft): string {
  if (n.children.length) return `${n.children.length} 个小节`
  return `${n.problems.length} 道题`
}

async function removeChapter(ci: number) {
  const ch = chs.value[ci]
  const detail = ch.children.length
    ? `${ch.children.length} 个小节（含其下题目）`
    : `${ch.problems.length} 道题`
  try {
    await ElMessageBox.confirm(
      `删除章节「${ch.title}」？将一并移除 ${detail} 的挂载（题目本体不受影响）。`,
      '删除章节',
      { type: 'warning' },
    )
  } catch {
    return
  }
  chs.value.splice(ci, 1)
  selected.value = null
}

async function removeSection(ci: number, si: number) {
  const sec = chs.value[ci]?.children[si]
  if (!sec) return
  if (sec.problems.length) {
    try {
      await ElMessageBox.confirm(
        `删除小节「${sec.title}」？将一并移除其下 ${sec.problems.length} 道题的挂载（题目本体不受影响）。`,
        '删除小节',
        { type: 'warning' },
      )
    } catch {
      return
    }
  }
  chs.value[ci].children.splice(si, 1)
  if (selected.value === `sec:${ci}-${si}`) selected.value = null
}

function moveChapter(ci: number, dir: -1 | 1) {
  const j = ci + dir
  if (j < 0 || j >= chs.value.length) return
  const next = [...chs.value]
  ;[next[ci], next[j]] = [next[j], next[ci]]
  chs.value = next
}

function moveSection(ci: number, si: number, dir: -1 | 1) {
  const ch = chs.value[ci]
  const j = si + dir
  if (!ch || j < 0 || j >= ch.children.length) return
  const next = [...ch.children]
  ;[next[si], next[j]] = [next[j], next[si]]
  ch.children = next
}

// ---- 拖拽排序（章之间 / 章内小节之间） ----
const dragKey = ref<string | null>(null)
function onDragStart(key: string) {
  dragKey.value = key
}
function onDropChapter(ci: number) {
  if (!dragKey.value) return
  // 小节拖到章节上 = 移入该章（追加到末尾；拍板 4：支持单节跨章）
  const secPos = secOf(dragKey.value)
  if (secPos) {
    moveSectionToChapter(secPos.ci, secPos.si, ci, -1)
    return
  }
  const from = Number(dragKey.value.slice(3))
  if (from === ci) return
  const next = [...chs.value]
  const [moved] = next.splice(from, 1)
  next.splice(ci, 0, moved)
  chs.value = next
  dragKey.value = null
}
function onDropSection(ci: number, si: number) {
  const pos = dragKey.value ? secOf(dragKey.value) : null
  if (!pos) return // 章节拖到小节上忽略
  if (pos.ci === ci) {
    // 章内排序
    if (pos.si === si) return
    const ch = chs.value[ci]
    const next = [...ch.children]
    const [moved] = next.splice(pos.si, 1)
    next.splice(si, 0, moved)
    ch.children = next
  } else {
    // 跨章：移入目标章该位置（拍板 4）
    moveSectionToChapter(pos.ci, pos.si, ci, si)
  }
  dragKey.value = null
}

// 小节跨章移动（targetSi = -1 追加到末尾）；单章不混挂守卫：目标章已有直挂题则拒绝
function moveSectionToChapter(fromCi: number, fromSi: number, toCi: number, targetSi: number) {
  if (fromCi === toCi) {
    dragKey.value = null
    return
  }
  const src = chs.value[fromCi]
  const dst = chs.value[toCi]
  if (dst.problems.length) {
    ElMessage.warning(`「${dst.title || '未命名章节'}」已有直挂题目（单章不混挂），不能移入小节`)
    dragKey.value = null
    return
  }
  const [moved] = src.children.splice(fromSi, 1)
  if (targetSi < 0) dst.children.push(moved)
  else dst.children.splice(Math.min(targetSi, dst.children.length), 0, moved)
  selected.value = null
  dragKey.value = null
}

// ---- 保存 ----
function validate(): string | null {
  if (!title.value.trim()) return '题单名不能为空'
  if (title.value.trim().length > 64) return '题单名不超过 64 字'
  for (const [ci, c] of chs.value.entries()) {
    const t = c.title.trim()
    if (!t) return `第 ${ci + 1} 章标题不能为空`
    if (t.length > 32) return `第 ${ci + 1} 章标题超过 32 字`
    if (c.desc.length > 128) return `第 ${ci + 1} 章描述超过 128 字`
    if (c.children.length && c.problems.length) return `「${t}」不能同时包含小节和题目`
    for (const [si, s] of c.children.entries()) {
      const st = s.title.trim()
      if (!st) return `第 ${ci + 1}.${si + 1} 节标题不能为空`
      if (st.length > 32) return `第 ${ci + 1}.${si + 1} 节标题超过 32 字`
      if (s.desc.length > 128) return `第 ${ci + 1}.${si + 1} 节描述超过 128 字`
    }
  }
  return null
}

async function save() {
  const err = validate()
  if (err) {
    ElMessage.warning(err)
    return
  }
  saving.value = true
  try {
    const chapters = chs.value.map((c) => {
      const base = { title: c.title.trim(), desc: c.desc.trim() }
      if (c.children.length) {
        return {
          ...base,
          children: c.children.map((s) => ({
            title: s.title.trim(),
            desc: s.desc.trim(),
            problems: s.problems.map((p) => p.id),
          })),
        }
      }
      return { ...base, problems: c.problems.map((p) => p.id) }
    })
    await listApi.update(id, { title: title.value.trim(), intro: intro.value, hidden: hidden.value, chapters })
    ElMessage.success('题单已保存')
    router.push(`/lists/${id}`)
  } finally {
    saving.value = false
  }
}

async function removeList() {
  try {
    await ElMessageBox.confirm(`删除题单「${title.value}」？删除后不可恢复。`, '删除题单', { type: 'warning' })
    await ElMessageBox.confirm('再次确认：删除后立即从题单页消失，操作不可恢复。', '二次确认', {
      type: 'error',
      confirmButtonText: '确认删除',
    })
  } catch {
    return
  }
  await listApi.remove(id)
  ElMessage.success('已删除')
  router.push('/lists')
}
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-head">
      <div class="kicker">EDITOR</div>
      <h1 class="page-title">编辑题单<small>章节最多两层 · 题挂叶子 · 单章不混挂</small></h1>
    </div>

    <div class="edit-layout">
      <div class="edit-main">
        <div class="card meta-card">
          <div class="meta-row">
            <el-input v-model="title" maxlength="64" placeholder="题单名（1-64 字）" class="title-input" />
          </div>
          <div class="intro-wrap">
            <div class="fl">简介（Markdown）</div>
            <MdEditor v-model="intro" variant="compact" auto-height height="200px" />
          </div>
        </div>

        <div class="cols">
      <!-- 左：章节树 -->
      <div class="card tree-card">
        <div class="tree-head">
          <b>章节</b>
          <el-button size="small" type="primary" plain @click="addChapter">+ 新章节</el-button>
        </div>
        <el-empty v-if="!chs.length" description="还没有章节，点右上角新建" :image-size="60" />
        <div v-else class="tree">
          <div
            v-for="(c, ci) in chs"
            :key="ci"
            class="node chapter-node"
            :class="{ on: selected === `ch:${ci}` }"
            draggable="true"
            @dragstart="onDragStart(`ch:${ci}`)"
            @dragover.prevent
            @drop="onDropChapter(ci)"
          >
            <div class="node-row" @click="select(`ch:${ci}`)">
              <span class="drag">⠿</span>
              <span class="no mono">{{ ci + 1 }}</span>
              <el-input
                :model-value="c.title"
                maxlength="32"
                class="node-title"
                @update:model-value="c.title = $event"
                @click.stop
              />
              <span class="stat">{{ nodeStat(c) }}</span>
            </div>
            <div class="node-sub">
              <el-input
                :model-value="c.desc"
                size="small"
                maxlength="128"
                placeholder="本章描述（选填，一句以内）"
                class="node-desc"
                @update:model-value="c.desc = $event"
              />
              <span class="acts" @click.stop>
                <el-button link size="small" :disabled="ci === 0" @click="moveChapter(ci, -1)">↑</el-button>
                <el-button link size="small" :disabled="ci === chs.length - 1" @click="moveChapter(ci, 1)">↓</el-button>
                <el-button
                  v-if="!c.children.length"
                  link
                  size="small"
                  type="primary"
                  title="给本章直接挂题（挂题后不能再加小节）"
                  @click="select(`ch:${ci}`)"
                >加题</el-button>
                <el-button
                  v-if="!c.problems.length"
                  link
                  size="small"
                  type="primary"
                  title="本章下分小节（加节后不能再直挂题）"
                  @click="addSection(ci)"
                >+节</el-button>
                <el-button link size="small" type="danger" @click="removeChapter(ci)">删</el-button>
              </span>
            </div>
            <!-- 小节层 -->
            <div
              v-for="(s, si) in c.children"
              :key="si"
              class="node sec-node"
              :class="{ on: selected === `sec:${ci}-${si}` }"
              draggable="true"
              @dragstart="onDragStart(`sec:${ci}-${si}`)"
              @dragover.prevent
              @drop="onDropSection(ci, si)"
            >
              <div class="node-row" @click="select(`sec:${ci}-${si}`)">
                <span class="drag">⠿</span>
                <span class="no mono">{{ ci + 1 }}.{{ si + 1 }}</span>
                <el-input
                  :model-value="s.title"
                  maxlength="32"
                  class="node-title"
                  @update:model-value="s.title = $event"
                  @click.stop
                />
              </div>
              <div class="node-sub">
                <el-input
                  :model-value="s.desc"
                  size="small"
                  maxlength="128"
                  placeholder="小节描述（选填）"
                  class="node-desc"
                  @update:model-value="s.desc = $event"
                />
                <span class="acts" @click.stop>
                  <el-button link size="small" :disabled="si === 0" @click="moveSection(ci, si, -1)">↑</el-button>
                  <el-button link size="small" :disabled="si === c.children.length - 1" @click="moveSection(ci, si, 1)">↓</el-button>
                  <el-button link size="small" type="danger" @click="removeSection(ci, si)">删</el-button>
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 右：选中节点的题目 -->
      <div class="card probs-card">
        <template v-if="selectedNode">
          <div class="probs-head">
            <b>{{ selectedIsChapter ? '章' : '小节' }}「{{ selectedNode.title || '未命名' }}」的题目</b>
            <span class="stat">{{ selectedNode.problems.length }} 题</span>
          </div>
          <div v-if="selectedChapterHasChildren" class="mixed-hint">
            该章已包含小节——单章不混挂，题目请加到小节里。
          </div>
          <template v-else>
            <div v-if="!selectedNode.problems.length" class="empty-hint">还没有题，从下方题库选取加入。</div>
            <ProblemPicker v-model="selectedNode.problems" />
            <p class="hint">同题可出现在多个章节（复习章等）；进度按题去重，不会重复计。</p>
          </template>
        </template>
        <el-empty v-else description="在左侧选择章节或小节，在这里管理它的题目" :image-size="70" />
      </div>
        </div>
      </div>

      <!-- 右：管理卡（web-design.md 副列原则①；吸顶随手可存） -->
      <aside class="edit-aside">
        <RailCard ov="管理" title="题单管理" class="aside-card">
          <div class="mgmt-actions">
            <el-button type="primary" :loading="saving" @click="save">保存</el-button>
            <el-button @click="router.push(`/lists/${id}`)">预览</el-button>
            <el-button type="danger" plain @click="removeList">删除题单</el-button>
          </div>
          <div class="aside-divider" />
          <SwitchRow v-model="hidden" label="隐藏题单" tip="隐藏期间学生端不可见（列表与详情），仅你与 root 可见；可用于备题" />
        </RailCard>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.meta-card {
  padding: 16px 20px;
  margin-bottom: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.meta-row {
  display: flex;
  gap: 10px;
}

.title-input {
  max-width: 480px;
}

.intro-wrap .fl {
  font-size: 13px;
  font-weight: 700;
  color: var(--soj-navy, #0b1f45);
  margin-bottom: 6px;
}

/* 编辑器总布局：左主列（meta + 双列）/ 右管理卡（吸顶） */
.edit-layout {
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.edit-main {
  flex: 1 1 auto;
  min-width: 0;
}

.edit-aside {
  flex: 0 0 300px;
  width: 300px;
  position: sticky;
  top: 84px;
}

.mgmt-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mgmt-actions .el-button {
  width: 100%;
  margin-left: 0;
}

.aside-divider {
  height: 1px;
  background: var(--soj-line, #e3eaf6);
  margin: 14px 0;
}

.cols {
  /* 章节 : 题目 = 1:1（2026-10-26 用户拍板）；minmax(0,·) 锁轨道——
     否则 ProblemPicker 的 min-content（标签横滚行等）会把 1fr 撑出容器 */
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}

.tree-card,
.probs-card {
  min-width: 0;
}

.tree-card,
.probs-card {
  padding: 16px 18px;
  min-height: 320px;
}

.tree-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.tree-head b {
  font-size: 15px;
  color: var(--soj-navy, #0b1f45);
}

.tree {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.node {
  border: 1px solid var(--soj-line, #e3eaf6);
  border-radius: 10px;
  padding: 8px 10px;
  background: #fff;
}

.node.on {
  border-color: var(--soj-blue);
  box-shadow: 0 0 0 2px rgb(var(--soj-blue-hi-rgb) / 0.12);
}

.sec-node {
  margin: 6px 0 2px 26px;
  background: #fafcff;
}

.node-row {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.drag {
  cursor: grab;
  color: #b6c2d6;
  user-select: none;
}

.no {
  flex: none;
  min-width: 30px;
  font-weight: 700;
  color: var(--soj-blue);
  font-size: 13px;
}

.node-title {
  flex: 1 1 auto;
  min-width: 0;
}

/* 第二行：描述占满 + 操作簇靠右 */
.node-sub {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
}

.node-sub .node-desc {
  flex: 1 1 auto;
  min-width: 0;
  margin: 0;
  width: auto;
}

.node-sub .acts {
  flex: none;
}

.stat {
  flex: none;
  font-size: 13px;
  color: var(--soj-ink-3, #5b6b85);
  white-space: nowrap;
}

.acts {
  flex: none;
  display: inline-flex;
}

.acts .el-button + .el-button {
  margin-left: 2px;
}

.probs-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.probs-head b {
  font-size: 15px;
  color: var(--soj-navy, #0b1f45);
}

.mixed-hint,
.empty-hint {
  font-size: 13px;
  color: var(--soj-ink-3, #5b6b85);
  padding: 12px 0;
}

.hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--soj-ink-3, #5b6b85);
}

@media (max-width: 1100px) {
  .cols {
    grid-template-columns: 1fr;
  }
}
</style>

<script setup lang="ts">
// 题单编辑器（2026-11-01 重构批，plan 讨论拍板）：
// 主列 = 题单标题/简介 + 章节大纲（全部章/节平铺：拖拽排序含跨章、标题描述就地编辑、
// hover 出操作、0 题警示）；右栏 = 题单管理卡（保存/预览/删除/隐藏 + 脏标记）+
// 章节目录卡（迷你导航，点击滚动定位）。节点题目管理走大弹窗（SectionProblemsDialog：
// 双栏选题 + 上一节/下一节 + 跨节防重），标题/描述只在大纲就地编辑（只留一处）。
// 数据模型不动：两层章/节、单章不混挂、整体 PATCH；新增脏标记 + ⌘S + 离开拦截。
import { computed, onMounted, ref } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import MdEditor from '@/components/MdEditor.vue'
import InlineEdit from '@/components/InlineEdit.vue'
import ListOrganizeDialog from '@/components/ListOrganizeDialog.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import SectionProblemsDialog from '@/components/SectionProblemsDialog.vue'
import SwitchRow from '@/components/SwitchRow.vue'
import { listApi } from '@/api'
import { useDirtyFlag } from '@/composables/useDirtyFlag'
import { useListener } from '@/composables/useWindowListener'
import { draftToChapters } from '@/utils/listDraft'
import type { ListDetail, ListEntryDraft } from '@/types'

const route = useRoute()
const router = useRouter()

const id = Number(route.params.id)
const title = ref('')
const intro = ref('')
const hidden = ref(false)
const chs = ref<ListEntryDraft[]>([])
const loading = ref(true)
const saving = ref(false)

// ---- 脏标记：与加载时快照全量比对（结构/标题/简介/隐藏任何变化都算） ----
// v0.65 起走 useDirtyFlag（全站唯一实现）：击键零 stringify、300ms 防抖比对，
// 语义保留「改回原值 = 不脏」；takeSnapshot 复位基线后立即重查
const snapshot = ref('')

const { dirty, recheck: recheckDirty } = useDirtyFlag(
  () => (snapshot.value ? { title: title.value, intro: intro.value, hidden: hidden.value, chs: chs.value } : null),
  () => (snapshot.value ? snapshot.value : 'null'), // 未载入（空快照）恒不脏
)

function takeSnapshot() {
  snapshot.value = JSON.stringify({ title: title.value, intro: intro.value, hidden: hidden.value, chs: chs.value })
  recheckDirty()
}

const savedAt = ref('')

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
    takeSnapshot()
  } finally {
    loading.value = false
  }
})

// ⌘S / Ctrl+S 保存（监听走 useListener，v0.64 病根②收口：作用域卸载自动摘）
useListener(window, 'keydown', onKeydown)

// ⌘S / Ctrl+S 保存
function onKeydown(e: KeyboardEvent) {
  if (e.key === 's' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault()
    if (dirty.value) save()
  }
}

// 离开拦截：有未保存改动时确认
onBeforeRouteLeave(async () => {
  if (!dirty.value) return true
  try {
    await ElMessageBox.confirm('有未保存的改动，离开将丢失。确定离开？', '未保存', {
      type: 'warning',
      confirmButtonText: '离开',
      cancelButtonText: '留在本页',
    })
    return true
  } catch {
    return false
  }
})

// ---- 章节操作 ----
function addChapter() {
  chs.value.push({ title: `第${chs.value.length + 1}章`, desc: '', problems: [], children: [] })
  folded.value.delete(chs.value.length - 1)
}

function addSection(ci: number) {
  const ch = chs.value[ci]
  if (!ch) return
  if (ch.problems.length) {
    ElMessage.warning('该章已有直挂题目（单章不混挂），不能再加小节')
    return
  }
  ch.children.push({ title: `${ci + 1}.${ch.children.length + 1} 小节`, desc: '', problems: [], children: [] })
  folded.value.delete(ci)
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
}

// ---- 展开/收起（默认全展开；仅影响展示，拖拽折叠章时其小节随行移动） ----
const folded = ref(new Set<number>())

function toggleFold(ci: number) {
  const next = new Set(folded.value)
  if (next.has(ci)) next.delete(ci)
  else next.add(ci)
  folded.value = next
}

// ---- 大弹窗：当前管理的节点（'ch:i' | 'sec:i-j'） ----
const dialogKey = ref<string | null>(null)

function openDialog(key: string) {
  dialogKey.value = key
}

// ---- AI 一键整理弹窗（v0.71）：任务只产建议，应用写本页 chs 草稿再走保存管道 ----
const organizeOpen = ref(false)

// ---- 大纲拖拽（章排序 / 小节章内排序与跨章移动，不混挂守卫沿用） ----
const dragKey = ref<string | null>(null)
const dropKey = ref<string | null>(null)

function onDragStart(key: string) {
  dragKey.value = key
}

function onDragEnter(key: string) {
  if (dragKey.value && dragKey.value !== key) dropKey.value = key
}

function onDragLeave(key: string) {
  if (dropKey.value === key) dropKey.value = null
}

function onDropChapter(ci: number) {
  if (!dragKey.value) return
  dropKey.value = null
  // 小节拖到章节行 = 移入该章（追加到末尾）
  const m = /^sec:(\d+)-(\d+)$/.exec(dragKey.value)
  if (m) {
    moveSectionToChapter(Number(m[1]), Number(m[2]), ci, -1)
    return
  }
  const from = Number(dragKey.value.slice(3))
  if (from === ci) {
    dragKey.value = null
    return
  }
  const next = [...chs.value]
  const [moved] = next.splice(from, 1)
  next.splice(ci, 0, moved)
  chs.value = next
  dragKey.value = null
}

function onDropSection(ci: number, si: number) {
  dropKey.value = null
  const pos = dragKey.value ? (/^sec:(\d+)-(\d+)$/.exec(dragKey.value) ? { ci: Number(RegExp.$1), si: Number(RegExp.$2) } : null) : null
  if (!pos) {
    dragKey.value = null
    return // 章拖到小节上忽略
  }
  if (pos.ci === ci) {
    if (pos.si !== si) {
      const ch = chs.value[ci]
      const next = [...ch.children]
      const [moved] = next.splice(pos.si, 1)
      next.splice(si, 0, moved)
      ch.children = next
    }
  } else {
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
  dragKey.value = null
}

// ---- 章节目录卡（右栏迷你导航）：点击滚动定位 + 闪烁提示 ----
interface TocRow {
  key: string
  elId: string
  no: string
  title: string
  stat: string
  zero: boolean
  depth: 0 | 1
}

const toc = computed<TocRow[]>(() => {
  const rows: TocRow[] = []
  chs.value.forEach((c, ci) => {
    rows.push({
      key: `ch:${ci}`,
      elId: `ol-ch-${ci}`,
      no: `${ci + 1}`,
      title: c.title || '未命名',
      stat: c.children.length ? `${c.children.length} 小节` : `${c.problems.length} 题`,
      zero: !c.children.length && !c.problems.length,
      depth: 0,
    })
    c.children.forEach((s, si) => {
      rows.push({
        key: `sec:${ci}-${si}`,
        elId: `ol-sec-${ci}-${si}`,
        no: `${ci + 1}.${si + 1}`,
        title: s.title || '未命名',
        stat: `${s.problems.length} 题`,
        zero: !s.problems.length,
        depth: 1,
      })
    })
  })
  return rows
})

const flashId = ref('')

function tocGo(row: TocRow) {
  const el = document.getElementById(row.elId)
  if (!el) return
  el.scrollIntoView({ behavior: 'smooth', block: 'center' })
  flashId.value = row.elId
  window.setTimeout(() => (flashId.value = ''), 1200)
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
    const chapters = draftToChapters(chs.value)
    await listApi.update(id, { title: title.value.trim(), intro: intro.value, hidden: hidden.value, chapters })
    takeSnapshot()
    savedAt.value = new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    ElMessage.success('题单已保存')
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
      <h1 class="page-title">编辑题单<small>章节最多两层 · 单章不混挂 · 点小节行管理题目</small></h1>
    </div>

    <PageShell>
      <div>
        <div class="card meta-card">
          <el-input v-model="title" maxlength="64" placeholder="题单名（1-64 字）" class="title-input" />
          <div class="intro-wrap">
            <div class="fl">简介（Markdown）</div>
            <MdEditor v-model="intro" variant="compact" auto-height height="200px" />
          </div>
        </div>

        <div class="card outline-card">
          <div class="outline-head">
            <b>章节<TipLabel tip="⠿ 拖拽排序（小节可跨章移动）；点标题/描述就地编辑" /></b>
            <span class="dim">{{ chs.length }} 章</span>
            <el-tooltip content="AI 结合题面把题库题目归入章/节，预览确认后写入草稿" placement="top">
              <span class="head-organize">
                <el-button size="small" plain :disabled="!chs.length" @click="organizeOpen = true">AI 整理</el-button>
              </span>
            </el-tooltip>
            <el-button size="small" type="primary" plain @click="addChapter">＋ 新章节</el-button>
          </div>

          <el-empty v-if="!chs.length" description="还没有章节，先建第一章">
            <el-button type="primary" @click="addChapter">＋ 新章节</el-button>
          </el-empty>

          <div v-else class="outline">
            <template v-for="(c, ci) in chs" :key="ci">
              <!-- 章行 -->
              <div
                :id="`ol-ch-${ci}`"
                class="ol-row ch-row"
                :class="{ 'drop-target': dropKey === `ch:${ci}`, flash: flashId === `ol-ch-${ci}` }"
                draggable="true"
                @dragstart="onDragStart(`ch:${ci}`)"
                @dragenter="onDragEnter(`ch:${ci}`)"
                @dragleave="onDragLeave(`ch:${ci}`)"
                @dragover.prevent
                @drop="onDropChapter(ci)"
                @click="c.children.length ? toggleFold(ci) : openDialog(`ch:${ci}`)"
              >
                <span class="drag">⠿</span>
                <span class="fold" @click.stop="toggleFold(ci)">{{ c.children.length ? (folded.has(ci) ? '▸' : '▾') : '' }}</span>
                <span class="no mono">{{ ci + 1 }}</span>
                <InlineEdit
                  :model-value="c.title"
                  maxlength="32"
                  variant="title"
                  placeholder="未命名章节"
                  class="ol-title"
                  @update:model-value="c.title = $event"
                />
                <InlineEdit
                  :model-value="c.desc"
                  maxlength="128"
                  variant="desc"
                  placeholder="本章描述（选填，一句以内）"
                  class="ol-desc"
                  @update:model-value="c.desc = $event"
                />
                <span class="ol-stat dim">{{ c.children.length ? `${c.children.length} 小节` : `${c.problems.length} 题` }}</span>
                <span class="ol-acts" @click.stop>
                  <el-button v-if="!c.problems.length" link size="small" type="primary" @click="addSection(ci)">＋节</el-button>
                  <el-button link size="small" type="danger" @click="removeChapter(ci)">删</el-button>
                </span>
              </div>
              <!-- 小节行 -->
              <template v-if="!folded.has(ci)">
                <div
                  v-for="(s, si) in c.children"
                  :id="`ol-sec-${ci}-${si}`"
                  :key="si"
                  class="ol-row sec-row"
                  :class="{ 'drop-target': dropKey === `sec:${ci}-${si}`, flash: flashId === `ol-sec-${ci}-${si}` }"
                  draggable="true"
                  @dragstart="onDragStart(`sec:${ci}-${si}`)"
                  @dragenter="onDragEnter(`sec:${ci}-${si}`)"
                  @dragleave="onDragLeave(`sec:${ci}-${si}`)"
                  @dragover.prevent
                  @drop="onDropSection(ci, si)"
                  @click="openDialog(`sec:${ci}-${si}`)"
                >
                  <span class="drag">⠿</span>
                  <span class="fold" />
                  <span class="no mono">{{ ci + 1 }}.{{ si + 1 }}</span>
                  <InlineEdit
                    :model-value="s.title"
                    maxlength="32"
                    variant="title"
                    placeholder="未命名小节"
                    class="ol-title"
                    @update:model-value="s.title = $event"
                  />
                  <InlineEdit
                    :model-value="s.desc"
                    maxlength="128"
                    variant="desc"
                    placeholder="小节描述（选填）"
                    class="ol-desc"
                    @update:model-value="s.desc = $event"
                  />
                  <span class="ol-stat dim" :class="{ 'zero-warn': !s.problems.length }">
                    {{ s.problems.length ? `${s.problems.length} 题` : '0 题' }}
                  </span>
                  <span class="ol-acts" @click.stop>
                    <el-button size="small" @click="openDialog(`sec:${ci}-${si}`)">管理题目 ›</el-button>
                    <el-button link size="small" type="danger" @click="removeSection(ci, si)">删</el-button>
                  </span>
                </div>
              </template>
            </template>
          </div>
        </div>
      </div>

      <template #aside>
        <div class="edit-aside">
        <RailCard ov="管理" title="题单管理" class="aside-card">
          <div class="mgmt-actions">
            <el-button type="primary" :loading="saving" @click="save">
              保存<span v-if="dirty" class="dirty-dot" title="有未保存改动" />
            </el-button>
            <el-button @click="router.push(`/lists/${id}`)">预览</el-button>
            <el-button type="danger" plain @click="removeList">删除题单</el-button>
          </div>
          <div class="save-state">
            <span v-if="dirty" class="dirty-text">● 有未保存改动（⌘S 保存）</span>
            <span v-else-if="savedAt" class="dim">已保存 {{ savedAt }}</span>
          </div>
          <div class="aside-divider" />
          <SwitchRow v-model="hidden" label="隐藏题单" tip="隐藏期间学生端不可见（列表与详情），仅你与 root 可见；可用于备题" />
        </RailCard>

        <RailCard v-if="chs.length" ov="导航" title="章节目录" class="aside-card">
          <div class="toc">
            <button
              v-for="row in toc"
              :key="row.key"
              type="button"
              class="toc-row"
              :class="[`d-${row.depth}`, { 'zero-warn': row.zero }]"
              @click="tocGo(row)"
            >
              <span class="toc-no mono">{{ row.no }}</span>
              <span class="toc-title">{{ row.title }}</span>
              <span class="toc-stat">{{ row.stat }}</span>
            </button>
          </div>
        </RailCard>
        </div>
      </template>
    </PageShell>

    <SectionProblemsDialog
      :chs="chs"
      :node-key="dialogKey"
      @update:node-key="dialogKey = $event"
      @close="dialogKey = null"
    />

    <ListOrganizeDialog v-model="organizeOpen" :list-id="id" :chs="chs" />
  </div>
</template>

<style scoped>
.meta-card {
  padding: var(--soj-sp-16) var(--soj-sp-20);
  margin-bottom: var(--soj-sp-16);
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-12);
}

/* 副列吸顶（PageShell side-col 内；窄屏折叠由 PageShell 自理，此处只复位吸附） */
.edit-aside {
  position: sticky;
  top: var(--soj-sticky-top);
}

@media (max-width: 992px) {
  .edit-aside {
    position: static;
  }
}

/* ---- 章节大纲 ---- */
.outline-card {
  padding: var(--soj-sp-16) var(--soj-sp-20);
}

.outline-head {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  margin-bottom: var(--soj-sp-12);
}

.outline-head b {
  color: var(--soj-navy);
}

/* auto margin 落在 AI 整理包裹层：与「＋ 新章节」成对靠右，AI 整理在其左侧（用户拍板） */
.outline-head .head-organize {
  margin-left: auto;
}

.ol-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: var(--soj-sp-8) var(--soj-sp-12);
  border: 1px solid transparent;
  border-radius: var(--el-border-radius-base);
  cursor: pointer;
  min-width: 0;
}

.ol-row:hover {
  background: var(--el-fill-color-light);
}

.ol-row.drop-target {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.ol-row.flash {
  animation: ol-flash 1.2s ease;
}

@keyframes ol-flash {
  0%,
  60% {
    background: var(--el-color-primary-light-8);
  }

  100% {
    background: transparent;
  }
}

.ol-row.ch-row {
  background: var(--el-fill-color-lighter);
}

.ol-row.ch-row:hover {
  background: var(--el-fill-color-light);
}

.ol-row.sec-row {
  margin-left: var(--soj-sp-32);
}

.drag {
  color: var(--el-text-color-placeholder);
  cursor: grab;
  flex: none;
}

.fold {
  width: 12px;
  flex: none;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  text-align: center;
}

.no {
  flex: none;
  width: 34px;
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-13);
}

.ol-title {
  flex: 0 1 auto;
  min-width: 120px;
  max-width: 45%;
}

.ol-desc {
  flex: 1 1 120px;
  min-width: 60px;
}

.ol-stat {
  flex: none;
  font-size: var(--soj-fs-13);
}

.zero-warn {
  color: var(--el-color-warning);
}

.ol-acts {
  flex: none;
  display: inline-flex;
  align-items: center;
  opacity: 0;
  transition: opacity 0.15s;
}

.ol-row:hover .ol-acts {
  opacity: 1;
}

.ol-acts .el-button + .el-button {
  margin-left: var(--soj-sp-8);
}

/* ---- 右栏 ---- */
.mgmt-actions {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-10);
}

.mgmt-actions .el-button {
  width: 100%;
  margin-left: 0;
}

.save-state {
  min-height: 18px;
  margin-top: var(--soj-sp-8);
  font-size: var(--soj-fs-13);
  text-align: center;
}

.dirty-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--soj-white);
  margin-left: var(--soj-sp-6);
  vertical-align: middle;
}

.dirty-text {
  color: var(--el-color-warning);
}

.aside-divider {
  height: 1px;
  background: var(--soj-line);
  margin: var(--soj-sp-12) 0;
}

/* 章节目录（迷你导航） */
.toc {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-2);
  max-height: 420px;
  overflow-y: auto;
}

.toc-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  border: none;
  background: transparent;
  text-align: left;
  padding: var(--soj-sp-6) var(--soj-sp-8);
  border-radius: var(--el-border-radius-base);
  cursor: pointer;
  font-size: var(--soj-fs-13);
  min-width: 0;
}

.toc-row:hover {
  background: var(--el-fill-color-light);
}

.toc-row.d-1 {
  margin-left: var(--soj-sp-16);
}

.toc-no {
  flex: none;
  color: var(--el-text-color-secondary);
  width: 34px;
}

.toc-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--soj-ink);
}

.toc-stat {
  flex: none;
  color: var(--el-text-color-placeholder);
}
</style>

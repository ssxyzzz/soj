<script setup lang="ts">
// Markdown 编辑器内芯（v0.52 编辑器内芯换血：Vditor → CodeMirror 6 自建薄壳）。
// 红线（plan §2.52）：md 进 md 出——组件契约与 Vditor 时代完全一致，薄壳 MdEditor
// 与全部调用点零改动，存储模型/写入时渲染零触碰。两模式：split（分屏预览）/
// source（纯源码），偏好持久化 prefs.mdEditMode（存量 wysiwyg/ir/sv 一律归一
// split——三者的共同诉求都是「边写边看」）。
// 预览 = POST /api/render 服务端渲染（与写入口径同一把 lute 同一套消毒），
// 「编辑器里看到的 = 发布后看到的」；断网/限频预览区降级提示，编辑与保存不受阻。
// 图片：粘贴/拖入/工具栏直传 /api/uploads（服务端零改动，沿用 Vditor 约定信封）。
// Vditor 时代补丁全部退役：undo 补同步（updateListener 原生覆盖）、主题单例复位
// （预览恒站内 .soj-md 真实排版）、全屏 overflow 兜底（自管）、MutationObserver
// （按钮是自己的 DOM）、量高补丁（autoHeight 由 CSS overflow 自然生长）。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Bold, Code, Columns2, FileCode, Heading1, Heading2, Heading3, Image as ImageIcon, Italic,
  Link2, List, ListOrdered, ListTodo, Maximize, Minimize, Minus, Redo2, Sigma, SquareCode,
  SquareSigma, Strikethrough, Table as TableIcon, TextQuote, Undo2,
} from 'lucide-vue-next'
import { ElMessage } from 'element-plus'
import type { Component } from 'vue'

import { markdown } from '@codemirror/lang-markdown'
import { undo, redo } from '@codemirror/commands'
import { EditorView, keymap, placeholder } from '@codemirror/view'
import { basicSetup } from 'codemirror'

import { authApi, mdApi } from '@/api'
import MdView from '@/components/MdView.vue'
import { useUserStore } from '@/stores/user'

const props = withDefaults(
  defineProps<{
    modelValue: string
    height?: string
    /** 工具栏档位：full = 文档型场景（题面/公告/赛事介绍，默认）；compact = 短内容瘦身（题解/说明，去表格/图片/全屏） */
    variant?: 'full' | 'compact'
    /** 高度随内容自生长（v0.9.3；v0.9.5 起不设视口上限）：短内容不留空白，普通编辑随内容长高 */
    autoHeight?: boolean
  }>(),
  {
    height: '560px',
    variant: 'full',
    autoHeight: false,
  },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const userStore = useUserStore()

type MdEditMode = 'split' | 'source'
// 存量偏好归一：wysiwyg/ir/sv 的共同诉求都是「边写边看」，一律落 split
function normalizeMode(v: string | undefined): MdEditMode {
  return v === 'source' ? 'source' : 'split'
}

const mode = ref<MdEditMode>(normalizeMode(userStore.prefs.mdEditMode))
const fullscreen = ref(false)
const loading = ref(true)
const editorError = ref('')
const userSized = ref(false)
const splitPct = ref(50)
const uploadingCount = ref(0)

// 预览态：idle（空内容）/ loading / ok / error（断网、限频、超长——编辑不受阻）
const previewHtml = ref('')
const previewState = ref<'idle' | 'loading' | 'ok' | 'error'>('idle')

const rootEl = ref<HTMLDivElement>()
const toolbarEl = ref<HTMLDivElement>()
const editorHost = ref<HTMLDivElement>()
const previewPane = ref<HTMLDivElement>()
const fileInput = ref<HTMLInputElement>()

// CM6 经静态 import 进编辑器懒 chunk（薄壳 MdEditor 异步加载，主包零增量；
// 与 CodeEditor 共享 codemirror 公共 chunk）。类型走 any 与 CodeEditor 同口径。
let view: EditorView | null = null
let inner = props.modelValue // 防回显循环

// ---- 偏好回存（沿用 Vditor 时代节奏：400ms 防抖合并，只发变更字段） ----
let prefTimer: number | undefined
let pendingPref: { mdEditMode?: MdEditMode } = {}

function persistPref(patch: { mdEditMode?: MdEditMode }) {
  pendingPref = { ...pendingPref, ...patch }
  window.clearTimeout(prefTimer)
  prefTimer = window.setTimeout(async () => {
    const body = pendingPref
    pendingPref = {}
    userStore.setPrefs({ ...userStore.prefs, ...body })
    try {
      const p = await authApi.updateProfile({ prefs: body })
      userStore.setPrefs(p.prefs ?? { ...userStore.prefs, ...body })
    } catch {
      // 拦截器已 toast；本次未落库，下次切换会再存
    }
  }, 400)
}

function switchMode(m: MdEditMode) {
  if (mode.value === m) return
  mode.value = m
  persistPref({ mdEditMode: m })
  if (m === 'split') {
    previewState.value = inner.trim() ? 'loading' : 'idle'
    runPreview()
  }
  nextTick(() => view?.focus())
}

// ---- 预览：300ms 防抖 + 飞行中取消（AbortController），失败降级不阻塞编辑 ----
let renderTimer: number | undefined
let renderCtl: AbortController | undefined
let renderSeq = 0

function schedulePreview() {
  if (mode.value !== 'split') return
  if (!inner.trim()) {
    window.clearTimeout(renderTimer)
    previewHtml.value = ''
    previewState.value = 'idle'
    return
  }
  window.clearTimeout(renderTimer)
  renderTimer = window.setTimeout(runPreview, 300)
}

async function runPreview() {
  const md = inner
  if (!md.trim()) {
    previewHtml.value = ''
    previewState.value = 'idle'
    return
  }
  renderCtl?.abort()
  const ctl = new AbortController()
  renderCtl = ctl
  const seq = ++renderSeq
  previewState.value = 'loading'
  try {
    const { html } = await mdApi.render(md, ctl.signal)
    if (seq !== renderSeq || ctl.signal.aborted) return // 已被更新的一次顶掉
    previewHtml.value = html
    previewState.value = 'ok'
  } catch {
    if (ctl.signal.aborted || seq !== renderSeq) return
    previewState.value = 'error'
  }
}

// ---- 编辑器命令（工具栏与快捷键共用；CM6 selection 运算，无第三方状态） ----

/** 选区包裹（带 toggle：已包裹则剥标记）——加粗/斜体/删除线/行内代码/行内公式 */
function wrapSel(mark: string) {
  if (!view) return
  const s = view.state.selection.main
  const text = view.state.sliceDoc(s.from, s.to)
  const changes = text.startsWith(mark) && text.endsWith(mark) && text.length >= mark.length * 2
    ? [{ from: s.from, to: s.to, insert: text.slice(mark.length, text.length - mark.length) }]
    : s.empty
      ? [{ from: s.from, insert: mark + mark }]
      : [{ from: s.from, to: s.to, insert: mark + text + mark }]
  view.dispatch({
    changes,
    // 无选区时光标落两标记之间；剥标记后选中原文本，便于继续操作
    selection: { anchor: s.from + (text.startsWith(mark) && text.endsWith(mark) && text.length >= mark.length * 2 ? 0 : mark.length) },
  })
  view.focus()
}

/** 行前缀切换（标题/引用/无序列表）——作用于全部选中行，已带前缀则剥掉 */
function linePrefix(prefix: string) {
  if (!view) return
  const { state } = view
  const s = state.selection.main
  const first = state.doc.lineAt(s.from).number
  const last = state.doc.lineAt(s.to).number
  const changes: { from: number; to?: number; insert: string }[] = []
  for (let n = first; n <= last; n++) {
    const line = state.doc.line(n)
    if (line.text.startsWith(prefix)) {
      changes.push({ from: line.from, to: line.from + prefix.length, insert: '' })
    } else {
      changes.push({ from: line.from, insert: prefix })
    }
  }
  view.dispatch({ changes })
  view.focus()
}

/** 有序列表：逐行编号 1. 2. …（已是有序行则剥号，再点变无序可再套） */
function orderedList() {
  if (!view) return
  const { state } = view
  const s = state.selection.main
  const first = state.doc.lineAt(s.from).number
  const last = state.doc.lineAt(s.to).number
  const changes: { from: number; to?: number; insert: string }[] = []
  let allOrdered = true
  for (let n = first; n <= last; n++) {
    if (!/^\d+\.\s/.test(state.doc.line(n).text)) allOrdered = false
  }
  for (let n = first; n <= last; n++) {
    const line = state.doc.line(n)
    if (allOrdered) {
      const m = line.text.match(/^\d+\.\s/)
      if (m) changes.push({ from: line.from, to: line.from + m[0].length, insert: '' })
    } else {
      changes.push({ from: line.from, insert: `${n - first + 1}. ` })
    }
  }
  view.dispatch({ changes })
  view.focus()
}

/** 任务列表：- [ ] 前缀切换（渲染端 GFM 任务勾选框） */
function taskList() {
  linePrefix('- [ ] ')
}

/** 光标处插入块模板（块级公式/代码块/表格/分隔线），片段尾留光标 */
function insertBlock(text: string, cursorOffset?: number) {
  if (!view) return
  const s = view.state.selection.main
  view.dispatch({
    changes: [{ from: s.from, insert: text }],
    selection: { anchor: s.from + (cursorOffset ?? text.length) },
  })
  view.focus()
}

function insertLink() {
  if (!view) return
  const s = view.state.selection.main
  const text = s.empty ? '链接文字' : view.state.sliceDoc(s.from, s.to)
  const insert = `[${text}](url)`
  view.dispatch({
    changes: [{ from: s.from, to: s.to, insert }],
    // 光标落到 url 处待替换（insert 起点 + 标题 + ]( 共 3 定长字符前缀之外再让位 url）
    selection: { anchor: s.from + text.length + 3 },
  })
  view.focus()
}

const TABLE_SNIPPET = '\n| 列1 | 列2 | 列3 |\n| --- | --- | --- |\n|  |  |  |\n'
const CODE_SNIPPET = '\n```\n\n```\n'

// ---- 图片上传：粘贴/拖入/工具栏三入口，直传 /api/uploads 后插 md ----
const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
const IMAGE_MAX = 5 * 1024 * 1024

function pickImage() {
  fileInput.value?.click()
}

async function uploadFiles(files: FileList | File[]) {
  const list = Array.from(files).filter((f) => {
    if (!IMAGE_TYPES.includes(f.type)) {
      ElMessage.warning(`已跳过非图片文件：${f.name}`)
      return false
    }
    if (f.size > IMAGE_MAX) {
      ElMessage.warning(`已跳过超过 5MB 的图片：${f.name}`)
      return false
    }
    return true
  })
  if (!list.length) return
  uploadingCount.value++
  try {
    const form = new FormData()
    for (const f of list) form.append('file', f)
    const res = await mdApi.uploadImages(form)
    if (res.errFiles?.length) ElMessage.warning(`${res.errFiles.length} 张图片上传失败`)
    if (res.succMap && view) {
      const pos = view.state.selection.main.to
      let insert = ''
      for (const [name, url] of Object.entries(res.succMap)) insert += `![${name}](${url})\n`
      view.dispatch({ changes: [{ from: pos, insert }] })
      inner = view.state.doc.toString()
      emit('update:modelValue', inner)
    }
  } catch {
    // 拦截器已 toast
  } finally {
    uploadingCount.value--
  }
}

// ---- 分栏拖拽（split 模式，20%–80% 钳制） ----
function startDrag(e: MouseEvent) {
  if (e.button !== 0) return
  e.preventDefault()
  document.addEventListener('mousemove', onDrag)
  document.addEventListener('mouseup', endDrag)
  document.body.style.cursor = 'col-resize'
}
function onDrag(e: MouseEvent) {
  const host = editorHost.value?.parentElement
  if (!host) return
  const rect = host.getBoundingClientRect()
  const pct = ((e.clientX - rect.left) / rect.width) * 100
  splitPct.value = Math.min(80, Math.max(20, pct))
}
function endDrag() {
  document.removeEventListener('mousemove', onDrag)
  document.removeEventListener('mouseup', endDrag)
  document.body.style.cursor = ''
}

// ---- 滚动同步（编辑→预览单向比例同步，rAF 节流） ----
let syncPending = false
function syncScroll() {
  if (syncPending || mode.value !== 'split') return
  syncPending = true
  requestAnimationFrame(() => {
    syncPending = false
    const ed = view?.scrollDOM
    const pv = previewPane.value
    if (!ed || !pv) return
    const edMax = ed.scrollHeight - ed.clientHeight
    if (edMax <= 0) return
    pv.scrollTop = (ed.scrollTop / edMax) * (pv.scrollHeight - pv.clientHeight)
  })
}

// ---- 全屏（自管 body overflow；Esc 退出；z-index 盖过 EP 弹层 2000） ----
let bodyOverflowBefore = ''
function onEsc(e: KeyboardEvent) {
  if (e.key === 'Escape' && fullscreen.value) toggleFullscreen()
}
function toggleFullscreen() {
  const on = !fullscreen.value
  fullscreen.value = on
  if (on) {
    bodyOverflowBefore = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    window.addEventListener('keydown', onEsc)
  } else {
    document.body.style.overflow = bodyOverflowBefore
    window.removeEventListener('keydown', onEsc)
  }
  nextTick(() => view?.focus())
}

// ---- 高度：autoHeight = 内容自然生长（CSS overflow 方案，无 JS 量高） ----
// 底部手柄按下进入拖拽：高度随鼠标走（≥140px），松手锁定（userSized）此后内滚
function startResize(e: MouseEvent) {
  if (!props.autoHeight || userSized.value || e.button !== 0 || !rootEl.value) return
  e.preventDefault()
  userSized.value = true
  const startY = e.clientY
  const startH = rootEl.value.getBoundingClientRect().height
  document.body.style.cursor = 'ns-resize'
  const onMove = (ev: MouseEvent) => {
    if (!rootEl.value) return
    rootEl.value.style.height = `${Math.max(140, startH + (ev.clientY - startY))}px`
  }
  const onUp = () => {
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onUp)
    document.body.style.cursor = ''
  }
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
}

// ---- 工具栏 ----
interface ToolItem {
  key: string
  title: string
  icon?: Component
  run: () => void
}

function onFileChange() {
  if (fileInput.value?.files?.length) void uploadFiles(fileInput.value.files)
  if (fileInput.value) fileInput.value.value = ''
}

const toolbar = computed<{ items: ToolItem[]; dividerAfter?: boolean }[]>(() => {
  const group = (items: ToolItem[]): { items: ToolItem[] } => ({ items })
  const doc: { items: ToolItem[] }[] = [
    group([
      { key: 'h1', title: '一级标题', icon: Heading1, run: () => linePrefix('# ') },
      { key: 'h2', title: '二级标题', icon: Heading2, run: () => linePrefix('## ') },
      { key: 'h3', title: '三级标题', icon: Heading3, run: () => linePrefix('### ') },
      { key: 'bold', title: '加粗（Ctrl+B）', icon: Bold, run: () => wrapSel('**') },
      { key: 'italic', title: '斜体（Ctrl+I）', icon: Italic, run: () => wrapSel('*') },
      { key: 'strike', title: '删除线', icon: Strikethrough, run: () => wrapSel('~~') },
    ]),
    group([
      { key: 'ul', title: '无序列表', icon: List, run: () => linePrefix('- ') },
      { key: 'ol', title: '有序列表', icon: ListOrdered, run: orderedList },
      { key: 'task', title: '任务列表', icon: ListTodo, run: taskList },
      { key: 'quote', title: '引用', icon: TextQuote, run: () => linePrefix('> ') },
    ]),
    group([
      { key: 'inline-code', title: '行内代码', icon: Code, run: () => wrapSel('`') },
      { key: 'code', title: '代码块', icon: SquareCode, run: () => insertBlock(CODE_SNIPPET, CODE_SNIPPET.length - 4) },
      { key: 'link', title: '链接（Ctrl+K）', icon: Link2, run: insertLink },
      { key: 'math-inline', title: '行内公式 $…$', icon: Sigma, run: () => wrapSel('$') },
      { key: 'math-block', title: '块级公式 $$…$$', icon: SquareSigma, run: () => insertBlock('\n$$\n\n$$\n', 4) },
    ]),
    group([
      { key: 'table', title: '表格', icon: TableIcon, run: () => insertBlock(TABLE_SNIPPET) },
      { key: 'hr', title: '分隔线', icon: Minus, run: () => insertBlock('\n---\n') },
      { key: 'image', title: '插入图片（也可直接粘贴/拖入）', icon: ImageIcon, run: pickImage },
    ]),
    group([
      { key: 'undo', title: '撤销（Ctrl+Z）', icon: Undo2, run: () => view && undo(view) },
      { key: 'redo', title: '重做（Ctrl+Y）', icon: Redo2, run: () => view && redo(view) },
    ]),
  ]
  const compact: { items: ToolItem[] }[] = [
    group([
      { key: 'bold', title: '加粗（Ctrl+B）', icon: Bold, run: () => wrapSel('**') },
      { key: 'italic', title: '斜体（Ctrl+I）', icon: Italic, run: () => wrapSel('*') },
      { key: 'strike', title: '删除线', icon: Strikethrough, run: () => wrapSel('~~') },
    ]),
    group([
      { key: 'ul', title: '无序列表', icon: List, run: () => linePrefix('- ') },
      { key: 'ol', title: '有序列表', icon: ListOrdered, run: orderedList },
      { key: 'task', title: '任务列表', icon: ListTodo, run: taskList },
      { key: 'quote', title: '引用', icon: TextQuote, run: () => linePrefix('> ') },
    ]),
    group([
      { key: 'inline-code', title: '行内代码', icon: Code, run: () => wrapSel('`') },
      { key: 'code', title: '代码块', icon: SquareCode, run: () => insertBlock(CODE_SNIPPET, CODE_SNIPPET.length - 4) },
      { key: 'link', title: '链接（Ctrl+K）', icon: Link2, run: insertLink },
      { key: 'math-inline', title: '行内公式 $…$', icon: Sigma, run: () => wrapSel('$') },
    ]),
    group([
      { key: 'undo', title: '撤销（Ctrl+Z）', icon: Undo2, run: () => view && undo(view) },
      { key: 'redo', title: '重做（Ctrl+Y）', icon: Redo2, run: () => view && redo(view) },
    ]),
  ]
  return props.variant === 'full' ? doc : compact
})

// ---- CM6 挂载 ----
onMounted(() => {
  if (!editorHost.value) return
  try {
    view = new EditorView({
      doc: props.modelValue || '',
      parent: editorHost.value,
      extensions: [
        basicSetup,
        markdown({ codeLanguages: [] }), // GFM（表格/任务列表/删除线）是 lang-markdown 默认开启项
        EditorView.lineWrapping,
        keymap.of([
          {
            key: 'Mod-b',
            run: () => {
              wrapSel('**')
              return true
            },
          },
          {
            key: 'Mod-i',
            run: () => {
              wrapSel('*')
              return true
            },
          },
          {
            key: 'Mod-k',
            run: () => {
              insertLink()
              return true
            },
          },
        ]),
        EditorView.theme({
          '&': { height: '100%', fontSize: '15px' },
          '.cm-content': {
            fontFamily: 'Consolas, Menlo, Monaco, monospace',
            lineHeight: '1.7',
            padding: '10px 4px 54px 12px',
          },
          '.cm-gutters': { fontFamily: 'Consolas, Menlo, Monaco, monospace', fontSize: '13px' },
          '&.cm-focused': { outline: 'none' },
        }),
        placeholder('Markdown 编辑：工具栏排版、直接粘贴/拖入图片；公式用 $…$ / $$…$$'),
        EditorView.domEventHandlers({
          paste(event) {
            const files = event.clipboardData?.files
            if (files?.length) {
              event.preventDefault()
              void uploadFiles(files)
              return true
            }
            return false
          },
          drop(event) {
            const files = (event as DragEvent).dataTransfer?.files
            if (files?.length) {
              event.preventDefault()
              void uploadFiles(files)
              return true
            }
            return false
          },
        }),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) {
            inner = u.state.doc.toString()
            emit('update:modelValue', inner)
            schedulePreview()
          }
        }),
      ],
    })
    view.scrollDOM.addEventListener('scroll', syncScroll)
    loading.value = false
    if (mode.value === 'split' && inner.trim()) runPreview()
  } catch (e) {
    console.error('[MdEditor] CodeMirror 初始化失败', e)
    editorError.value = '编辑器初始化失败。'
    loading.value = false
  }
})

watch(
  () => props.modelValue,
  (v) => {
    if (view && v !== inner) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v ?? '' } })
      inner = v ?? ''
      schedulePreview()
    }
  },
)

onBeforeUnmount(() => {
  window.clearTimeout(renderTimer)
  window.clearTimeout(prefTimer)
  renderCtl?.abort()
  renderSeq++
  if (fullscreen.value) {
    document.body.style.overflow = bodyOverflowBefore
    window.removeEventListener('keydown', onEsc)
  }
  endDrag()
  view?.scrollDOM.removeEventListener('scroll', syncScroll)
  view?.destroy()
  view = null
})
</script>

<template>
  <!-- 包装层：撑满父容器（el-form-item__content 是 flex）；
       is-fullscreen 固定视口、is-sized 手动高度优先、md-auto 随内容自然生长 -->
  <div
    ref="rootEl"
    class="md-editor soj-md"
    :class="{
      'is-fullscreen': fullscreen,
      'is-sized': userSized,
      'md-auto': autoHeight && !userSized,
    }"
    :style="!autoHeight && !fullscreen ? { height: props.height } : undefined"
  >
    <div ref="toolbarEl" class="md-toolbar">
      <template v-for="(g, gi) in toolbar" :key="gi">
        <span v-if="gi > 0" class="md-toolbar-sep" />
        <button
          v-for="item in g.items"
          :key="item.key"
          type="button"
          class="md-tool"
          :title="item.title"
          @click="item.run"
        >
          <component :is="item.icon" v-if="item.icon" :size="15" :stroke-width="1.8" />
        </button>
      </template>
      <span v-if="uploadingCount > 0" class="md-upload-hint">图片上传中…</span>
      <span class="md-toolbar-spacer" />
      <button
        type="button"
        class="md-tool"
        :class="{ 'is-on': mode === 'split' }"
        title="分屏预览（左编辑右预览）"
        @click="switchMode('split')"
      >
        <Columns2 :size="15" :stroke-width="1.8" />
      </button>
      <button
        type="button"
        class="md-tool"
        :class="{ 'is-on': mode === 'source' }"
        title="纯源码（只显示编辑区）"
        @click="switchMode('source')"
      >
        <FileCode :size="15" :stroke-width="1.8" />
      </button>
      <button
        v-if="variant === 'full'"
        type="button"
        class="md-tool"
        :title="fullscreen ? '退出全屏（Esc）' : '全屏'"
        @click="toggleFullscreen"
      >
        <Minimize v-if="fullscreen" :size="15" :stroke-width="1.8" />
        <Maximize v-else :size="15" :stroke-width="1.8" />
      </button>
    </div>

    <div class="md-body">
      <div
        ref="editorHost"
        class="md-edit-pane"
        :style="mode === 'split' ? { flexBasis: splitPct + '%' } : { flexBasis: '100%' }"
      >
        <div v-if="loading" class="md-editor-status">编辑器加载中…</div>
        <div v-else-if="editorError" class="md-editor-status is-error">{{ editorError }}</div>
      </div>

      <template v-if="mode === 'split'">
        <div class="md-divider" @mousedown="startDrag" />
        <div ref="previewPane" class="md-preview-pane">
          <MdView v-if="previewState === 'ok' && previewHtml" :html="previewHtml" />
          <div v-else-if="previewState === 'idle'" class="md-preview-state">开始输入后此处实时预览</div>
          <div v-else-if="previewState === 'loading'" class="md-preview-state">渲染中…</div>
          <div v-else class="md-preview-state is-error">
            <span>预览暂不可用（网络或频率限制），编辑与保存不受影响</span>
            <button type="button" @click="runPreview">重试</button>
          </div>
        </div>
      </template>
    </div>

    <div v-if="autoHeight && !userSized" class="md-resize-handle" @mousedown="startResize" />

      <input
      ref="fileInput"
      type="file"
      accept="image/png,image/jpeg,image/gif,image/webp"
      multiple
      hidden
      @change="onFileChange"
    />
  </div>
</template>

<style scoped>
.md-editor {
  box-sizing: border-box; /* width:100% 含边框，避免卡片体内横向溢出 2px 出滚动条 */
  display: flex;
  flex-direction: column;
  width: 100%;
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  background: var(--el-bg-color);
  overflow: hidden;
}

/* 高度三态：定高（默认，内滚）/ 手动拖定（is-sized，内滚）/ 自生长（md-auto，随页滚）。
   is-sized 的具体高度由拖拽手柄写内联样式，这里只给下限兜底 */
.md-editor.is-sized {
  min-height: 140px;
}

.md-editor:not(.md-auto):not(.is-fullscreen) :deep(.cm-editor),
.md-editor.is-sized :deep(.cm-editor) {
  height: 100%;
}

.md-auto :deep(.cm-editor) {
  height: auto;
}

.md-auto :deep(.cm-scroller) {
  overflow: visible; /* 内容自然生长，页面滚动（Vditor 时代量高补丁的 CSS 等价物） */
}

/* 全屏：固定视口、面板内部滚动，盖过 EP 弹层（el-dialog ~2000） */
.md-editor.is-fullscreen {
  position: fixed;
  inset: 0;
  z-index: 2030;
  height: 100vh !important;
  max-height: none;
  border-radius: 0;
}

.md-editor.is-fullscreen :deep(.cm-editor) {
  height: 100%;
}

/* 全屏覆盖 md-auto 的自然生长口径：内部滚动 + 预览面板恢复内滚 + 藏拖高手柄 */
.md-editor.is-fullscreen .md-body {
  flex: 1 1 auto;
}

.md-editor.is-fullscreen .md-preview-pane {
  overflow-y: auto;
}

.md-editor.is-fullscreen .md-resize-handle {
  display: none;
}

/* 工具栏：对齐站内编辑器工具栏口径（28px 高、主题色图标） */
.md-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px;
  padding: 4px 6px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  line-height: normal;
}

.md-toolbar-sep {
  width: 1px;
  height: 16px;
  margin: 0 4px;
  background: var(--el-border-color-light);
}

.md-toolbar-spacer {
  flex: 1;
}

.md-tool {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: 0;
  border-radius: 4px;
  background: none;
  color: var(--el-text-color-regular);
  cursor: pointer;
}

.md-tool:hover {
  background: var(--el-fill-color-light);
  color: var(--el-color-primary);
}

.md-tool.is-on {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.md-upload-hint {
  margin-left: 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

/* 主体：编辑/预览双栏，分栏宽可拖（20%–80% 钳制在 JS） */
.md-body {
  display: flex;
  flex: 1 1 auto;
  min-height: 0; /* 允许子面板收缩内滚 */
}

.md-auto .md-body {
  flex: 0 0 auto;
}

.md-edit-pane {
  flex-grow: 1;
  min-width: 0;
  position: relative;
}

.md-divider {
  flex: 0 0 5px;
  cursor: col-resize;
  background: var(--el-border-color-lighter);
  transition: background 0.15s;
}

.md-divider:hover {
  background: var(--el-color-primary-light-7);
}

.md-preview-pane {
  flex-grow: 1;
  min-width: 0;
  overflow-y: auto;
  padding: 12px 16px;
  border-left: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-blank);
}

.md-auto .md-preview-pane {
  overflow-y: visible;
}

.md-preview-state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-body, 14px);
}

.md-preview-state.is-error {
  color: var(--el-color-danger);
}

.md-preview-state button {
  padding: 0;
  border: 0;
  background: none;
  color: var(--el-color-primary);
  font: inherit;
  cursor: pointer;
}

/* 自生长模式的底部拖高手柄：按下即锁定当前高度（userSized），此后内滚 */
.md-resize-handle {
  height: 6px;
  cursor: ns-resize;
  background: var(--el-border-color-lighter);
  transition: background 0.15s;
}

.md-resize-handle:hover {
  background: var(--el-color-primary-light-7);
}

/* 加载/错误提示 */
.md-editor-status {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-body, 14px);
}

.md-editor-status.is-error {
  color: var(--el-color-danger);
}
</style>

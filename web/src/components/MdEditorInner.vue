<script setup lang="ts">
// 富文本编辑器内芯（v0.6 换血：vditor → Tiptap，2026-10-19 定稿见 plan.md §2.2）。
// headless Tiptap + Element Plus 自搭工具栏（延续视觉融合批次的 Editor.js 气质）；
// 内容 HTML 进出（modelValue），公式为官方 Mathematics 扩展（TeX 源码存 data-latex 契约，
// 双击弹层改 TeX + 实时预览）；粘贴/拖入/按钮上传图片走 /api/uploads（与 v0.5.1 同端点）。
// 本组件经 MdEditor 薄壳异步加载：Tiptap 全家桶只进编辑器懒 chunk，主包零增量。
// 注意必须用 useEditor（而非裸 new Editor）：工具栏点选态依赖 Vue 响应式事务订阅。
import { computed, ref, watch } from 'vue'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import { Table, TableRow, TableHeader, TableCell } from '@tiptap/extension-table'
import { Mathematics } from '@tiptap/extension-mathematics'
import { Image as TiptapImage } from '@tiptap/extension-image'
import { Placeholder } from '@tiptap/extension-placeholder'
import CodeBlockLowlight from '@tiptap/extension-code-block-lowlight'
import { common, createLowlight } from 'lowlight'
import { ElMessage, ElMessageBox } from 'element-plus'

import MdMathDialog from '@/components/MdMathDialog.vue'
import { MD_TOOLBAR_ICONS } from '@/utils/md-toolbar-icons'
import { useUserStore } from '@/stores/user'

const props = withDefaults(
  defineProps<{
    modelValue: string
    height?: string
    /** 自生长上限：传入后编辑器高度随内容在 height → maxHeight 间生长，超出内部滚动；不传维持固定高 */
    maxHeight?: string
    /** 工具栏档位：full = 文档型场景（题面/公告/赛事介绍，默认）；compact = 短内容瘦身（题解/说明，去表格/图片/全屏） */
    variant?: 'full' | 'compact'
  }>(),
  {
    height: '420px',
    maxHeight: undefined,
    variant: 'full',
  },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const lowlight = createLowlight(common)

const editor = useEditor({
  extensions: [
    StarterKit.configure({
      heading: { levels: [1, 2, 3, 4] },
      codeBlock: false, // 由 CodeBlockLowlight 接管（语法高亮与语言属性）
    }),
    CodeBlockLowlight.configure({ lowlight }),
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
    Mathematics.configure({ katexOptions: { throwOnError: false } }),
    TiptapImage.configure({ inline: false, allowBase64: false }),
    Placeholder.configure({ placeholder: '富文本编辑：工具栏排版、直接粘贴/拖入图片；公式双击可改 TeX' }),
  ],
  content: props.modelValue || '',
  editorProps: {
    // 排版规则与查看端共用 main.css .rich 段；attributes 走 editorProps.view
    attributes: { class: 'rich tiptap' },
    handlePaste: (_view, event) => uploadPastedImages(event),
    handleDrop: (_view, event, _slice, moved) => uploadDroppedImages(event, moved),
  },
  onUpdate: ({ editor: ed }) => {
    inner = ed.isEmpty ? '' : ed.getHTML()
    emit('update:modelValue', inner)
  },
})

// 调试句柄：浏览器控制台/自动化探针可用（生产保留；useEditor 挂载后才创建，须 watch 追踪）
watch(editor, (e) => {
  ;(window as unknown as { __mdEd?: unknown }).__mdEd = e
})

// ---------- 工具栏 ----------
const icon = (name: string) => MD_TOOLBAR_ICONS[name] ?? ''

function chain() {
  return editor.value!.chain().focus()
}

const headingLevel = computed(() => {
  const ed = editor.value
  if (!ed) return 0
  for (const lv of [1, 2, 3, 4]) {
    if (ed.isActive('heading', { level: lv })) return lv
  }
  return 0
})

function setHeading(level: number | 0) {
  if (level === 0) chain().setParagraph().run()
  else chain().setHeading({ level: level as 1 | 2 | 3 | 4 }).run()
}

function setLink() {
  const prev = (editor.value!.getAttributes('link').href as string) ?? ''
  ElMessageBox.prompt('链接地址', '插入链接', {
    inputValue: prev,
    inputPattern: /^https?:\/\/\S+$/,
    inputErrorMessage: '需为 http(s):// 开头的地址',
  })
    .then(({ value }) => chain().extendMarkRange('link').setLink({ href: value }).run())
    .catch(() => undefined)
}

// ---------- 表格 ----------
const inTable = computed(() => editor.value?.isActive('table') ?? false)

// ---------- 公式 ----------
// create 模式 = 工具栏按钮先弹层、确定后在光标处插入（官方 insertBlockMath 拒绝空 latex，
// 不能先插空节点再编辑）；否则为双击已有节点的编辑模式（更新须传显式 pos，见 PoC 结论）。
const mathEditing = ref<{ pos?: number; latex: string; block: boolean; create?: boolean } | null>(null)

function insertInlineMath() {
  mathEditing.value = { latex: '', block: false, create: true }
}
function insertBlockMath() {
  mathEditing.value = { latex: '', block: true, create: true }
}
// 双击公式节点：弹层改 TeX
function onEditorDblClick(e: MouseEvent) {
  const target = (e.target as HTMLElement)?.closest('[data-type="inline-math"],[data-type="block-math"]')
  if (!target || !editor.value) return
  const pos = editor.value.view.posAtDOM(target, 0)
  const node = editor.value.state.doc.nodeAt(pos)
  if (!node) return
  mathEditing.value = {
    pos,
    latex: String(node.attrs.latex ?? ''),
    block: node.type.name === 'blockMath',
  }
}
function saveMath(latex: string) {
  const m = mathEditing.value
  if (!m) return
  if (m.create) {
    if (latex.trim()) {
      // 弹层抢焦点先恢复选区；insertContent 为 schema 驱动的标准插入（块节点插在
      // 文本中段时自动切段——官方 insertBlockMath 命令在行内节点后等位置会静默失败）
      editor.value!.chain().focus().insertContent({ type: m.block ? 'blockMath' : 'inlineMath', attrs: { latex } }).run()
    }
  } else if (m.pos !== undefined) {
    if (m.block) editor.value!.commands.updateBlockMath({ pos: m.pos, latex })
    else editor.value!.commands.updateInlineMath({ pos: m.pos, latex })
  }
  mathEditing.value = null
}

// ---------- 图片上传 ----------
const fileInput = ref<HTMLInputElement>()
function pickImage() {
  fileInput.value?.click()
}

async function uploadFile(file: File): Promise<string | null> {
  if (!/^image\/(png|jpeg|gif|webp)$/.test(file.type)) {
    ElMessage.warning('仅支持 png/jpg/gif/webp 图片')
    return null
  }
  if (file.size > 5 * 1024 * 1024) {
    ElMessage.warning('单图上限 5MB')
    return null
  }
  const fd = new FormData()
  fd.append('file', file)
  try {
    const res = await fetch('/api/uploads', {
      method: 'POST',
      headers: { Authorization: `Bearer ${useUserStore().token}` },
      body: fd,
    })
    const body = await res.json()
    const name: string | undefined = body?.succMap?.[file.name]
    if (!res.ok || !name) {
      ElMessage.error(body?.message ? `上传失败：${body.message}` : '图片上传失败')
      return null
    }
    return name
  } catch {
    ElMessage.error('图片上传失败')
    return null
  }
}

async function insertImageFile(file: File) {
  const url = await uploadFile(file)
  if (url) chain().setImage({ src: url }).run()
}

function uploadPastedImages(event: ClipboardEvent): boolean {
  const files = Array.from(event.clipboardData?.files ?? []).filter((f) => f.type.startsWith('image/'))
  if (!files.length) return false
  event.preventDefault()
  files.forEach(insertImageFile)
  return true
}

function uploadDroppedImages(event: DragEvent, moved: boolean): boolean {
  if (moved) return false
  const files = Array.from(event.dataTransfer?.files ?? []).filter((f) => f.type.startsWith('image/'))
  if (!files.length) return false
  event.preventDefault()
  files.forEach(insertImageFile)
  return true
}

function onFileInput(e: Event) {
  const files = Array.from((e.target as HTMLInputElement).files ?? [])
  files.forEach(insertImageFile)
  ;(e.target as HTMLInputElement).value = ''
}

// ---------- 全屏 ----------
const fullscreen = ref(false)
function toggleFullscreen() {
  fullscreen.value = !fullscreen.value
}
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && fullscreen.value) fullscreen.value = false
}

// ---------- v-model ----------
let inner = props.modelValue // 防回显循环
watch(
  () => props.modelValue,
  (v) => {
    if (editor.value && v !== inner && v !== editor.value.getHTML()) {
      editor.value.commands.setContent(v || '', { emitUpdate: false })
    }
    inner = v
  },
)
</script>

<template>
  <!-- 包装层撑满父容器（el-form-item__content 是 flex，子项不会自动拉伸） -->
  <div
    class="md-editor"
    :class="{ 'md-editor--grow': !!props.maxHeight, 'md-editor--fullscreen': fullscreen }"
    @keydown="onKeydown"
  >
    <div class="md-editor-toolbar">
      <template v-if="props.variant === 'full'">
        <el-dropdown trigger="click" @command="setHeading">
          <button type="button" class="tb-btn" :title="headingLevel ? `标题 ${headingLevel}` : '标题'" v-html="icon('headings')" />
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item :command="0" :class="{ active: headingLevel === 0 }">正文</el-dropdown-item>
              <el-dropdown-item v-for="lv in 4" :key="lv" :command="lv" :class="{ active: headingLevel === lv }">
                标题 {{ lv }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('bold') }" title="加粗" @click="chain().toggleBold().run()" v-html="icon('bold')" />
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('italic') }" title="斜体" @click="chain().toggleItalic().run()" v-html="icon('italic')" />
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('strike') }" title="删除线" @click="chain().toggleStrike().run()" v-html="icon('strike')" />
      <span class="tb-sep" />
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('bulletList') }" title="无序列表" @click="chain().toggleBulletList().run()" v-html="icon('list')" />
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('orderedList') }" title="有序列表" @click="chain().toggleOrderedList().run()" v-html="icon('ordered-list')" />
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('taskList') }" title="任务列表" @click="chain().toggleTaskList().run()" v-html="icon('check')" />
      <span class="tb-sep" />
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('blockquote') }" title="引用" @click="chain().toggleBlockquote().run()" v-html="icon('quote')" />
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('code') }" title="行内代码" @click="chain().toggleCode().run()" v-html="icon('inline-code')" />
      <button type="button" class="tb-btn" :class="{ 'is-active': editor?.isActive('codeBlock') }" title="代码块" @click="chain().toggleCodeBlock().run()" v-html="icon('code')" />
      <span class="tb-sep" />
      <button type="button" class="tb-btn" title="链接" @click="setLink" v-html="icon('link')" />
      <template v-if="props.variant === 'full'">
        <button type="button" class="tb-btn" title="表格" @click="chain().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()" v-html="icon('table')" />
        <button type="button" class="tb-btn" title="图片" @click="pickImage" v-html="icon('image')" />
      </template>
      <button type="button" class="tb-btn" title="行内公式" @click="insertInlineMath" v-html="icon('math')" />
      <button type="button" class="tb-btn" title="块级公式" @click="insertBlockMath" v-html="icon('insert-code')" />
      <template v-if="inTable">
        <span class="tb-sep" />
        <button type="button" class="tb-btn" title="下方插入行" @click="chain().addRowAfter().run()">行+</button>
        <button type="button" class="tb-btn" title="删除行" @click="chain().deleteRow().run()">行-</button>
        <button type="button" class="tb-btn" title="右侧插入列" @click="chain().addColumnAfter().run()">列+</button>
        <button type="button" class="tb-btn" title="删除列" @click="chain().deleteColumn().run()">列-</button>
      </template>
      <span class="tb-flex" />
      <button type="button" class="tb-btn" :disabled="!editor?.can().undo()" title="撤销" @click="chain().undo().run()" v-html="icon('undo')" />
      <button type="button" class="tb-btn" :disabled="!editor?.can().redo()" title="重做" @click="chain().redo().run()" v-html="icon('redo')" />
      <button v-if="props.variant === 'full'" type="button" class="tb-btn" :class="{ 'is-active': fullscreen }" title="全屏" @click="toggleFullscreen" v-html="icon('fullscreen')" />
    </div>
    <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple hidden @change="onFileInput" />
    <EditorContent :editor="editor" class="md-editor-content" @dblclick="onEditorDblClick" />
    <MdMathDialog v-if="mathEditing" :initial-latex="mathEditing.latex" @save="saveMath" @close="mathEditing = null" />
  </div>
</template>

<style scoped>
.md-editor {
  box-sizing: border-box; /* width:100% 含边框，避免卡片体内横向溢出 2px 出滚动条 */
  width: 100%;
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  background: var(--el-bg-color);
}

/* 全屏：fixed 覆盖层（Escape 退出），内容列仍按 .rich 排版 */
.md-editor--fullscreen {
  position: fixed;
  inset: 0;
  z-index: 2000;
  display: flex;
  flex-direction: column;
  border: 0;
  border-radius: 0;
}

.md-editor--fullscreen .md-editor-content {
  flex: 1 1 auto;
  overflow-y: auto;
  height: auto;
}

/* 工具栏：白底与卡片边框融合（Editor.js 气质），点选态主色 */
.md-editor-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  padding: 4px 6px;
  background: var(--el-bg-color);
  border-bottom: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base) var(--el-border-radius-base) 0 0;
}

.tb-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 30px;
  height: 28px;
  padding: 0 4px;
  border: 0;
  border-radius: var(--el-border-radius-small);
  background: transparent;
  color: var(--el-text-color-regular);
  font-size: var(--soj-fs-editor-toolbar);
  cursor: pointer;
}

.tb-btn:disabled {
  opacity: 0.4;
  cursor: default;
}

.tb-btn :deep(svg) {
  fill: none;
  stroke: currentColor;
  stroke-width: 2;
  width: 20px;
  height: 20px;
}

.tb-btn :deep(svg.tb-md-lucide) {
  width: 17px;
  height: 17px;
}

.tb-btn:hover {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
}

.tb-btn.is-active {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.tb-sep {
  width: 1px;
  height: 18px;
  margin: 0 4px;
  background: var(--el-border-color-lighter);
}

.tb-flex {
  flex: 1 1 auto;
}

/* 内容区：固定高/自生长两态 */
.md-editor-content {
  height: v-bind('props.height');
  overflow-y: auto;
}

.md-editor--grow:not(.md-editor--fullscreen) {
  display: flex;
  flex-direction: column;
  height: auto;
  min-height: 300px; /* 空内容时的可用下限，其余高度完全跟内容走 */
  max-height: v-bind('props.maxHeight');
}

.md-editor--grow:not(.md-editor--fullscreen) .md-editor-content {
  flex: 1 1 auto;
  height: auto;
  min-height: 0;
  overflow-y: auto;
}

/* 内容内边距（排版在 main.css .rich 公共段） */
.md-editor-content :deep(.tiptap) {
  box-sizing: border-box; /* min-height:100% 含内边距，空内容也不撑出竖向滚动条 */
  padding: 14px 18px;
  min-height: 100%;
  outline: none;
}

.md-editor-content :deep(.tiptap p.is-editor-empty:first-child)::before {
  content: attr(data-placeholder);
  float: left;
  height: 0;
  pointer-events: none;
  color: var(--el-text-color-placeholder);
}
</style>

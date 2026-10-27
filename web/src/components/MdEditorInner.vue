<script setup lang="ts">
// Markdown 编辑器内芯（v0.8.1：Vditor 4）。
// 内容 md 进出（modelValue）；编辑模式（wysiwyg / ir / sv）与内容主题（light / ant-design /
// dark / wechat）读自个人设置（prefs.mdEditMode / prefs.mdTheme），在工具栏切换时自动回存
// 到账号——换浏览器/换设备仍跟人走。预览顶部的 Desktop/Tablet/Mobile 设备条经
// preview.actions: [] 关闭。
// 懒加载子资源（KaTeX/highlight/内容主题）自托管 /vditor——scripts/copy-vditor.mjs 把
// node_modules/vditor/dist 拷进 public（零 CDN，缓存语义见 server static_test）。
// 上传直连 /api/uploads：响应信封 {code,data:{succMap}} 与 Vditor 4 原生钩子兼容；
// 字段名服务端读 form.File["file"]，必须显式覆盖 Vditor 默认的 file[]。
// 本组件经 MdEditor 薄壳异步加载：Vditor 只进编辑器懒 chunk，主包零增量。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type Vditor from 'vditor'
import 'vditor/dist/index.css'

import { authApi } from '@/api'
import { useUserStore } from '@/stores/user'
import type { UserPrefs } from '@/types'
import { bindCopyFeedback } from '@/utils/md'

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

type MdEditMode = 'wysiwyg' | 'ir' | 'sv'
type MdTheme = Exclude<NonNullable<UserPrefs['mdTheme']>, ''>

// 合法主题集合：工具栏调色板的图标按钮自身也带 data-type="content-theme"（Vditor MenuItem
// 统一构造），点开面板的第一次点击会读到它——非主题值必须在这里滤掉，不能直传后端
const MD_THEMES = new Set<string>(['light', 'ant-design', 'dark', 'wechat'])

const initialMode = (userStore.prefs.mdEditMode || 'wysiwyg') as MdEditMode
const currentTheme = ref<MdTheme>((userStore.prefs.mdTheme || 'light') as MdTheme)
const lightTheme = computed(() => currentTheme.value === 'light')
// 用户拖过 Vditor 右下角 resize 手柄后，手动尺寸优先于内容自适应
const userSized = ref(false)
const loading = ref(true)
const editorError = ref('')

const el = ref<HTMLDivElement>()
let vd: Vditor | null = null
let VditorCtor: typeof Vditor | null = null
let modeObserver: MutationObserver | null = null
let unbindCopy: (() => void) | null = null
let inner = props.modelValue // 防回显循环

// 偏好回存：工具栏切模式/主题只发本次变更的字段（服务端在既有偏好上合并，
// 不会冲掉默认语言/代码模板等其它偏好）；400ms 防抖合并连续切换。
let prefTimer: number | undefined
let pendingPref: { mdEditMode?: MdEditMode; mdTheme?: MdTheme } = {}

// ---- 自生长高度（v0.9.3，v0.9.5 取消视口上限）----
// Vditor 的 height:'auto' 只在部分模式生效（sv/ir 内容区是定高 flex + 内部滚动），
// 这里直接量内容区各子面板的 scrollHeight，把编辑器高度设为「内容高 + 工具栏」，
// 不设 62vh 上限：普通编辑也随内容长高，页面整体滚动；全屏仍由 Vditor 的 100vh 接管。
let fitTimer: number | undefined

// 现代浏览器（Chrome 123+）支持 field-sizing: content——textarea 按内容原生自撑，
// 比脚本量 scrollHeight 精确且无抖动。支持时交给 CSS，不支持才走 JS 测量。
const fieldSizing = typeof CSS !== 'undefined' && typeof CSS.supports === 'function' && CSS.supports('field-sizing', 'content')

// Vditor 把 .vditor 类直接加在 ref 宿主 div 上，不再另建子节点
function editorRoot(): HTMLElement | null {
  const host = el.value
  if (!host) return null
  return host.classList.contains('vditor') ? host : host.querySelector<HTMLElement>('.vditor')
}

function fitHeight() {
  if (!props.autoHeight || userSized.value) return
  const root = editorRoot()
  if (!root) return
  if (fieldSizing) {
    // CSS 接管：清掉可能残留的内联高度，让 .md-field 规则生效
    root.style.removeProperty('height')
    return
  }
  const tb = root.querySelector<HTMLElement>('.vditor-toolbar')
  const tbH = tb ? tb.offsetHeight : 0

  // 内容高度：各编辑面板的 scrollHeight 反映「内容全高」，与当前可视高度无关。
  // 注意不能按 offsetParent 过滤——sv 的 textarea 在部分布局下 offsetParent 为 null，
  // 上一版就是因此量不到内容、高度停在两三行还带滚动条。
  let contentH = 0
  const ta = root.querySelector<HTMLTextAreaElement>('textarea.vditor-sv')
  if (ta) contentH = Math.max(contentH, ta.scrollHeight)
  const pv = root.querySelector<HTMLElement>('.vditor-preview')
  if (pv) contentH = Math.max(contentH, pv.scrollHeight)
  const editable = root.querySelector<HTMLElement>('.vditor-ir .vditor-reset, .vditor-wysiwyg .vditor-reset')
  if (editable) contentH = Math.max(contentH, editable.scrollHeight)
  if (contentH <= 0) return

  // 高度 = 内容 + 工具栏（2026-10-27 重新设定）：内容元素的 padding-bottom（.md-auto
  // 规则，恒定 2 行 = 54px）已计入 scrollHeight——底部留白恒定，空编辑器也有 2 行；
  // 不再设内容下限（min-height 会随内容拉伸容器，造成各编辑器底部留白忽大忽小）
  const h = contentH + tbH
  root.style.height = `${h}px`
}

function scheduleFit() {
  if (!props.autoHeight) return
  if (fitTimer) window.clearTimeout(fitTimer)
  fitTimer = window.setTimeout(fitHeight, 120)
}

function onWindowResize() {
  scheduleFit()
}

// Vditor 懒 chunk 在服务刚启动/版本刚切换时可能瞬时 404：重试两次，
// 仍失败则交给 UI 提示刷新页面，避免留下一块什么都不显示的空白。
async function loadVditor() {
  let lastErr: unknown
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      return (await import('vditor')).default
    } catch (e) {
      lastErr = e
      await new Promise((resolve) => window.setTimeout(resolve, 400 * (attempt + 1)))
    }
  }
  throw lastErr
}

function reloadPage() {
  window.location.reload()
}

let bodyOverflowBeforeFullscreen = ''

// 全屏按钮切 body overflow；退出时 Vditor 偶发未恢复会让整页无法滚动。
// 捕获阶段记录进入前的值，退出后一帧兜底恢复（保留弹窗自身的 overflow:hidden）。
function onToolbarClick(e: MouseEvent) {
  if (!(e.target as HTMLElement | null)?.closest('[data-type="fullscreen"]')) return
  const root = editorRoot()
  if (!root) return
  if (root.classList.contains('vditor--fullscreen')) {
    window.setTimeout(() => {
      if (!document.querySelector('.vditor--fullscreen')) {
        document.body.style.overflow = bodyOverflowBeforeFullscreen
      }
    }, 0)
  } else {
    bodyOverflowBeforeFullscreen = document.body.style.overflow
  }
}

// undo/redo 补同步（2026-10-27）：Vditor 的撤销/重做（⌘/Ctrl+Z・Y 热键与工具栏按钮）
// 走 renderDiff 直改 DOM，不触发 options.input——编辑器内容已变而 update:modelValue
// 没发，保存时把旧值写回，表现为「公告/简介等改了却显示旧内容」。捕获到这两类操作
// 后补发一次同步（setTimeout 等 Vditor 处理完再取值）。
function syncValue() {
  if (!vd) return
  const v = vd.getValue()
  inner = v
  emit('update:modelValue', v)
}

function onEditorKeydown(e: KeyboardEvent) {
  if (!(e.ctrlKey || e.metaKey) || e.altKey) return
  const k = e.key.toLowerCase()
  if (k === 'z' || k === 'y') window.setTimeout(syncValue, 0)
}

function onUndoRedoClick(e: MouseEvent) {
  if (!(e.target as HTMLElement | null)?.closest('[data-type="undo"],[data-type="redo"]')) return
  window.setTimeout(syncValue, 0)
}

// Vditor 的 resize 手柄改的是 .vditor 内联高度，而 .md-field 的 height:auto !important
// 会把它盖掉，表现为拖了没反应。按下手柄时先锁定当前高度并退出自适应，让内联高度接管；
// 此后手动尺寸优先，内容增长不再改高。
function onEditorMouseDown(e: MouseEvent) {
  if (!props.autoHeight || userSized.value) return
  const target = e.target as HTMLElement | null
  if (!target?.closest('.vditor-resize')) return
  const root = editorRoot()
  if (!root) return
  root.style.height = `${root.getBoundingClientRect().height}px`
  userSized.value = true
}

function persistPref(patch: { mdEditMode?: MdEditMode; mdTheme?: MdTheme }) {
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

// 公式插入按钮：Vditor 工具栏没有内置公式项，自定义两枚（prefix/suffix 夹住光标，
// 直接补写 TeX 即时渲染——与在正文里敲 $ 定界符同一条路）
const mathInlineItem = {
  name: 'math-inline',
  icon: '<svg viewBox="0 0 24 24"><path d="M4 20c2 0 3-6 5-10 1-3 2.5-5 4-5 2 0 3 2 3 6s1 9 3 9M15 11H7"/></svg>',
  tip: '行内公式 $…$',
  prefix: '$',
  suffix: '$',
}
const mathBlockItem = {
  name: 'math-block',
  icon: '<svg viewBox="0 0 24 24"><path d="M6 5v14M3 8l3-3 3 3M18 19V5m-3 3l3 3 3-3"/></svg>',
  tip: '块级公式 $$…$$',
  prefix: '\n$$\n',
  suffix: '\n$$\n',
}

function toolbar(): Array<string | Record<string, unknown>> {
  const doc: Array<string | Record<string, unknown>> = [
    'headings', 'bold', 'italic', 'strike', '|',
    'list', 'ordered-list', 'check', 'outdent', 'indent', '|',
    'quote', 'line', 'inline-code', 'code', '|',
    'link', 'table', 'upload', mathInlineItem, mathBlockItem, '|',
    'insert-before', 'insert-after', 'outline', '|',
    'content-theme', 'export', '|',
    'undo', 'redo', '|', 'edit-mode', 'fullscreen',
  ]
  const compact: Array<string | Record<string, unknown>> = [
    'headings', 'bold', 'italic', 'strike', '|',
    'list', 'ordered-list', 'check', '|',
    'quote', 'line', 'inline-code', 'code', '|',
    'link', mathInlineItem, '|',
    'undo', 'redo', '|', 'edit-mode',
  ]
  return props.variant === 'full' ? doc : compact
}

onMounted(async () => {
  let Vditor: Awaited<ReturnType<typeof loadVditor>>
  try {
    Vditor = await loadVditor()
  } catch (e) {
    console.error('[MdEditor] Vditor 加载失败', e)
    editorError.value = '编辑器加载失败，可能是页面版本已更新。'
    loading.value = false
    return
  }
  VditorCtor = Vditor
  if (!el.value) {
    loading.value = false
    return
  }
  vd = new Vditor(el.value, {
    mode: initialMode,
    lang: 'zh_CN',
    cdn: '/vditor',
    height: props.autoHeight ? '132px' : props.height,
    placeholder: 'Markdown 编辑：工具栏排版、直接粘贴/拖入图片；公式用 $…$ / $$…$$',
    toolbar: toolbar() as never,
    cache: { enable: false }, // 内容由 v-model 管理，关掉 localStorage 草稿
    fullscreen: { index: 2030 }, // 盖过 Element Plus 弹层（el-dialog ~2000）
    resize: { enable: true }, // 保留右下角拖拽手柄；自动拟合只在内容变化时调整
    value: props.modelValue || '',
    preview: {
      // actions 置空：去掉 sv 分屏预览顶部的 Device/Wechat 设备切换条
      actions: [],
      // 内容主题：跟随个人偏好（工具栏右侧调色板可随时切换并自动回存）
      theme: { current: currentTheme.value },
      maxWidth: 860,
      hljs: { lineNumber: false },
      math: { engine: 'KaTeX', inlineDigit: true }, // inlineDigit: $ 后允许数字开头（默认 false，会让 $1 \le N$ 不被识别）
    },
    upload: {
      url: '/api/uploads',
      fieldName: 'file', // 服务端读 form.File["file"]；Vditor 默认 file[] 不匹配
      accept: 'image/png,image/jpeg,image/gif,image/webp',
      multiple: true,
      max: 5 * 1024 * 1024,
      headers: { Authorization: `Bearer ${userStore.token}` },
    },
    input: (v: string) => {
      inner = v
      emit('update:modelValue', v)
      scheduleFit()
    },
    after: () => {
      if (!vd) return
      loading.value = false
      editorRoot()?.addEventListener('click', onToolbarClick, true)
      editorRoot()?.addEventListener('keydown', onEditorKeydown, true)
      editorRoot()?.addEventListener('click', onUndoRedoClick, true)
      scheduleFit()
      window.addEventListener('resize', onWindowResize)
      // 模式切换（工具栏菜单或 ⌥⌘7/8/9 热键）都会重排 edit-mode 按钮的 current 类，
      // 用它做变更信号，把最新模式回存到个人偏好
      const modeItem = vd.vditor?.toolbar?.elements?.['edit-mode']
      if (modeItem) {
        modeObserver = new MutationObserver(() => {
          const m = vd?.getCurrentMode() as MdEditMode | undefined
          if (m && m !== (userStore.prefs.mdEditMode || 'wysiwyg')) persistPref({ mdEditMode: m })
        })
        modeObserver.observe(modeItem, { subtree: true, attributes: true, attributeFilter: ['class'] })
      }
      // 主题菜单：内置 content-theme 项点击后自行换肤，这里只记录选择
      // （图标按钮带 data-type="content-theme"，非主题值在 MD_THEMES 处过滤）
      const themeItem = vd.vditor?.toolbar?.elements?.['content-theme']
      themeItem?.addEventListener(
        'click',
        (e) => {
          const btn = (e.target as HTMLElement | null)?.closest('button[data-type]') as HTMLElement | null
          const t = btn?.dataset.type as MdTheme | undefined
          if (!t || !MD_THEMES.has(t) || t === currentTheme.value) return
          currentTheme.value = t
          persistPref({ mdTheme: t })
        },
        true,
      )
      // 复制反馈（复制 → 已复制，3s 恢复）：绑在包装层，捕获阶段命中所有代码块按钮
      if (el.value) unbindCopy = bindCopyFeedback(el.value)
    },
  })
})

watch(
  () => props.modelValue,
  (v) => {
    if (vd && v !== inner) {
      vd.setValue(v ?? '')
      inner = v
    }
    // 外部内容变化（编辑回填、切换小题）后重新拟合高度
    scheduleFit()
  },
)

onBeforeUnmount(() => {
  window.removeEventListener('resize', onWindowResize)
  if (fitTimer) window.clearTimeout(fitTimer)
  window.clearTimeout(prefTimer)
  modeObserver?.disconnect()
  modeObserver = null
  unbindCopy?.()
  unbindCopy = null
  const root = editorRoot()
  root?.removeEventListener('click', onToolbarClick, true)
  root?.removeEventListener('keydown', onEditorKeydown, true)
  root?.removeEventListener('click', onUndoRedoClick, true)
  if (root?.classList.contains('vditor--fullscreen')) {
    document.body.style.overflow = bodyOverflowBeforeFullscreen
  }
  vd?.destroy()
  vd = null
  // 内容主题是全局单例 <link>：编辑器用了非 light 主题时不复位，会把背后的
  // 查看端（如题解弹窗里的 MdView）也染成暗色。关闭编辑器即回到站内 light。
  VditorCtor?.setContentTheme('light', '/vditor/dist/css/content-theme')
})
</script>

<template>
  <!-- 包装层撑满父容器（el-form-item__content 是 flex，子项不会自动拉伸）；
       soj-md = 站内 Markdown 排版（main.css），md-light = 仅 light 主题下启用站内配色 -->
  <div
    class="md-editor soj-md"
    :class="{
      'md-light': lightTheme,
      'md-auto': autoHeight,
      'md-field': autoHeight && fieldSizing && !userSized,
    }"
    @mousedown.capture="onEditorMouseDown"
  >
    <div ref="el"></div>
    <div v-if="loading" class="md-editor-status">编辑器加载中…</div>
    <div v-else-if="editorError" class="md-editor-status is-error">
      <span>{{ editorError }}</span>
      <button type="button" @click="reloadPage">刷新页面</button>
    </div>
  </div>
</template>

<style scoped>
.md-editor {
  box-sizing: border-box; /* width:100% 含边框，避免卡片体内横向溢出 2px 出滚动条 */
  width: 100%;
}

/* 自生长高度（v0.9.3）：高度由脚本按内容量出（见 fitHeight）；v0.9.5 起不设视口上限 */

/* field-sizing 原生自撑（Chrome 123+）：textarea 按内容长高，空内容约两行。
   不设 max-height，普通编辑状态跟内容一起长高，由页面滚动。 */
.md-field :deep(.vditor) {
  height: auto !important;
  overflow: hidden;
}

.md-field :deep(.vditor-content) {
  flex: 1 1 auto;
  /* 不设 min-height（2026-10-27）：min-height 会拉伸内容容器，底部留白随内容多少漂移；
     空编辑器的高度由内容自身的 1 行 + 底部 2 行 padding（.md-auto 规则）自然构成 */
}

.md-field :deep(textarea.vditor-sv) {
  field-sizing: content;
  overflow-y: auto;
}

/* 其余模式/分屏预览：随内容长高，不再按视口截断 */
.md-field :deep(.vditor-preview),
.md-field :deep(.vditor-ir),
.md-field :deep(.vditor-wysiwyg) {
  overflow-y: auto;
}

/* 自动高度模式内容底部留 2 行空白（2026-10-27）：写完不贴底边。两条量高路径
   （field-sizing 原生自撑 / JS scrollHeight）都把该 padding 计入总高，与 fitHeight 口径一致 */
.md-auto :deep(textarea.vditor-sv),
.md-auto :deep(.vditor-reset) {
  padding-bottom: 54px;
}

/* 视觉融合：边框圆角对齐 Element Plus 控件口径 */
.md-editor :deep(.vditor) {
  border-radius: var(--el-border-radius-base);
}

/* 工具栏按钮尺寸与站内编辑器工具栏一致（28px 高），图标继承主题色 */
.md-editor :deep(.vditor-toolbar) {
  padding: 4px 6px;
  line-height: normal;
}

.md-editor :deep(.vditor-toolbar__item) {
  font-size: var(--soj-fs-editor-toolbar, 14px);
}

/* 懒加载/加载失败提示：避免服务刚启动或版本切换时只剩一块空白 */
.md-editor-status {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 0;
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-body, 14px);
}

.md-editor-status.is-error {
  color: var(--el-color-danger);
}

.md-editor-status button {
  padding: 0;
  border: 0;
  background: none;
  color: var(--el-color-primary);
  font: inherit;
  cursor: pointer;
}

/* 全屏：固定 100vh，面板内部滚动 */
.md-auto :deep(.vditor--fullscreen),
.md-field :deep(.vditor--fullscreen) {
  height: 100vh !important;
  max-height: none;
  overflow: hidden;
}

.md-field :deep(.vditor--fullscreen) textarea.vditor-sv {
  field-sizing: auto;
  min-height: 0;
  max-height: none;
}

.md-field :deep(.vditor--fullscreen) .vditor-preview,
.md-field :deep(.vditor--fullscreen) .vditor-ir,
.md-field :deep(.vditor--fullscreen) .vditor-wysiwyg {
  max-height: none;
}
</style>

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
  }>(),
  {
    height: '560px',
    variant: 'full',
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
  const { default: Vditor } = await import('vditor')
  VditorCtor = Vditor
  if (!el.value) return
  vd = new Vditor(el.value, {
    mode: initialMode,
    lang: 'zh_CN',
    cdn: '/vditor',
    height: props.height,
    placeholder: 'Markdown 编辑：工具栏排版、直接粘贴/拖入图片；公式用 $…$ / $$…$$',
    toolbar: toolbar() as never,
    cache: { enable: false }, // 内容由 v-model 管理，关掉 localStorage 草稿
    fullscreen: { index: 2030 }, // 盖过 Element Plus 弹层（el-dialog ~2000）
    resize: { enable: true },
    value: props.modelValue || '',
    preview: {
      // actions 置空：去掉 sv 分屏预览顶部的 Device/Wechat 设备切换条
      actions: [],
      // 内容主题：跟随个人偏好（工具栏右侧调色板可随时切换并自动回存）
      theme: { current: currentTheme.value },
      maxWidth: 860,
      hljs: { lineNumber: false },
      math: { engine: 'KaTeX' },
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
    },
    after: () => {
      if (!vd) return
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
  },
)

onBeforeUnmount(() => {
  window.clearTimeout(prefTimer)
  modeObserver?.disconnect()
  modeObserver = null
  unbindCopy?.()
  unbindCopy = null
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
  <div class="md-editor soj-md" :class="{ 'md-light': lightTheme }">
    <div ref="el"></div>
  </div>
</template>

<style scoped>
.md-editor {
  box-sizing: border-box; /* width:100% 含边框，避免卡片体内横向溢出 2px 出滚动条 */
  width: 100%;
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
</style>

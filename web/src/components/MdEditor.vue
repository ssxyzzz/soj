<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'

import { useUserStore } from '@/stores/user'
import { loadVditor, type VditorInstance } from '@/utils/vditor'
import { MD_TOOLBAR_ICONS } from '@/utils/md-toolbar-icons'

const props = withDefaults(
  defineProps<{
    modelValue: string
    height?: string
    /** 工具栏档位：full = 文档型场景（题面/公告/赛事介绍，默认）；compact = 短内容瘦身（题解/说明，去 fullscreen/export/缩进等低频项） */
    variant?: 'full' | 'compact'
  }>(),
  {
    height: '420px',
    variant: 'full',
  },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const el = ref<HTMLDivElement>()
let inst: VditorInstance | null = null
let inner = props.modelValue // 防止 setValue 触发的回显循环

// 两档工具栏（视觉融合·批二）。图标统一换 Editor.js 官方图标包 @codexteam/icons
// （Lucide 补齐功能键），经 Vditor 原生 toolbar 项的 icon 字段注入（见 md-toolbar-icons.ts）。
// TIPS：显式中文提示兜底——自托管的 zh_CN 语言文件是旧版拷贝，缺新工具的 key 时
// Vditor 的 aria-label 会渲染成字面量 "undefined"（已发现缺 insert-code）。
type ToolbarItem = string | { name: string; icon: string; tip?: string }

const TIPS: Record<string, string> = { 'insert-code': '插入代码' }

function mdItems(names: string[]): ToolbarItem[] {
  return names.map((n) =>
    n === '|'
      ? '|'
      : { name: n, icon: MD_TOOLBAR_ICONS[n] ?? '', ...(TIPS[n] ? { tip: TIPS[n] } : {}) },
  )
}

const TOOLBARS: Record<'full' | 'compact', ToolbarItem[]> = {
  full: mdItems([
    'headings', 'bold', 'italic', 'strike', '|',
    'list', 'ordered-list', 'check', 'outdent', 'indent', '|',
    'quote', 'code', 'inline-code', 'insert-code', '|',
    'link', 'table', '|',
    'undo', 'redo', '|',
    'edit-mode', '|',
    'fullscreen', 'export',
  ]),
  compact: mdItems([
    'headings', 'bold', 'italic', 'strike', '|',
    'list', 'ordered-list', 'check', '|',
    'quote', 'inline-code', 'code', '|',
    'link', 'table', '|',
    'undo', 'redo', '|',
    'edit-mode',
  ]),
}

onMounted(async () => {
  const Vditor = await loadVditor()
  if (!el.value) return // 等待 chunk 加载期间组件已卸载
  // 初始编辑模式取个人设置（wysiwyg/ir/sv），未设置或非法值回退所见即所得；编辑中仍可随时切换
  const m = useUserStore().prefs.mdEditMode
  const mode = m === 'ir' || m === 'sv' ? m : 'wysiwyg'
  const token = useUserStore().token
  inst = new Vditor(el.value, {
    cdn: '/vditor', // 自托管资源（构建时随镜像分发，不依赖外网）
    lang: 'zh_CN', // 工具栏悬停提示等界面文案中文化
    height: props.height ?? '420px',
    mode,
    value: props.modelValue,
    cache: { enable: false },
    toolbar: TOOLBARS[props.variant] ?? TOOLBARS.full,
    placeholder: '支持 Markdown 与所见即所得，右上角可切换编辑模式；可直接粘贴/拖入图片',
    // 题面图片上传（v0.5a）：粘贴截图或拖入图片自动上传，成功后插入 /uploads/ 引用。
    // 后端响应即 Vditor 约定格式（succMap/errFiles）；鉴权头与全局拦截器同一身份源。
    upload: {
      url: '/api/uploads',
      fieldName: 'file',
      accept: 'image/png, image/jpeg, image/gif, image/webp',
      max: 5 * 1024 * 1024,
      multiple: true,
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    },
    input: (v) => {
      inner = v
      emit('update:modelValue', v)
    },
  })
})

watch(() => props.modelValue, (v) => {
  if (inst && v !== inner) {
    inst.setValue(v ?? '')
    inner = v
  }
})

onUnmounted(() => inst?.destroy())
</script>

<template>
  <!-- 包装层撑满父容器（el-form-item__content 是 flex，子项不会自动拉伸）；
       挂载点不能直接设宽——Vditor 初始化会改写挂载点的 class 与内联样式 -->
  <div class="md-editor">
    <div ref="el"></div>
  </div>
</template>

<style scoped>
.md-editor {
  width: 100%;
}
</style>

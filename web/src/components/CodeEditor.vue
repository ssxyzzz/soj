<script setup lang="ts">
// 代码编辑器（v0.5.3）：CodeMirror 6 共享组件，提交弹窗与在线编程复用。
// CM6 全量走动态 import → 独立懒加载 chunk（不进主包、零 CDN）；
// 语言与自动补全用 Compartment 热切换；自动补全开关走 utils/code.ts 的策略接口（预留等级门槛）。
// 补全三层源：include 上下文 → 精选清单（utils/completions.ts）→ 文档内词（completeAnyWord）。
import { onMounted, onUnmounted, ref, watch } from 'vue'

import { CPP_COMPLETIONS, CPP_HEADERS, PY_COMPLETIONS } from '@/utils/completions'

const props = withDefaults(
  defineProps<{
    modelValue: string
    language?: string // cpp / c / python3（c 复用 cpp 高亮）
    height?: string // 如 '420px'；'100%' 时父容器需有确定高度
    autocomplete?: boolean
    placeholder?: string
    fontSize?: number // px（12-48）
    fontFamily?: string // Consolas / Menlo / Monaco
    tabIndent?: boolean // Tab 键插入缩进，补全弹层开着时优先接受补全（在线编程开启；关闭时 Tab 移出焦点便于表单导航）
  }>(),
  {
    language: 'cpp',
    height: '420px',
    autocomplete: true,
    placeholder: '',
    fontSize: 14,
    fontFamily: 'Consolas',
    tabIndent: false,
  },
)

const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const el = ref<HTMLDivElement>()
let view: any = null // EditorView（CM6 经动态 import，类型取不到静态声明，统一 any）
let inner = props.modelValue // 防止 setValue 触发的回显循环
let langComp: any = null
let acComp: any = null
let themeComp: any = null
let acFactory: (() => any) | null = null
let viewMod: any = null

// 语言包懒加载（C 与 C++ 共用 C++ 高亮；Java/Go 届时加包）
async function langExt(language: string) {
  if (language === 'python3') {
    const { python } = await import('@codemirror/lang-python')
    return python()
  }
  const { cpp } = await import('@codemirror/lang-cpp')
  return cpp()
}

// 编辑器主题：字号与字体家族（跟随用户偏好热切换；平台缺字体时浏览器回退 monospace）
function buildTheme(vm: any, fontFamily: string, fontSize: number) {
  return vm.EditorView.theme({
    '&': { height: '100%', fontSize: fontSize + 'px' },
    '.cm-content': { fontFamily: `"${fontFamily}", Menlo, Monaco, monospace` },
    '.cm-gutters': { fontFamily: `"${fontFamily}", monospace` },
    '.cm-line': { lineHeight: '1.6' },
  })
}

onMounted(async () => {
  const [cm, state, vm, autocompleteMod, languageMod, commandsMod, lang] = await Promise.all([
    import('codemirror'),
    import('@codemirror/state'),
    import('@codemirror/view'),
    import('@codemirror/autocomplete'),
    import('@codemirror/language'),
    import('@codemirror/commands'),
    langExt(props.language),
  ])
  // 三层补全源（数据在 utils/completions.ts；Ctrl+Space 可显式唤起）：
  // ① include 上下文（#include <…> 内只出标准头，路径字符放宽到 [\w./+]，否则 bits/stdc++.h 打一半即断）
  // ② 精选清单（关键字/std/常用函数/片段，仅词首触发）
  // ③ 文档内词（completeAnyWord：buffer 里出现过的标识符也能补）
  const includeFrom = (ctx: any) => {
    const line = ctx.state.doc.lineAt(ctx.pos)
    const m = line.text.slice(0, ctx.pos - line.from).match(/#include\s*<([^<>]*)$/)
    return m ? ctx.pos - m[1].length : -1
  }
  const includeSource = (ctx: any) => {
    const from = includeFrom(ctx)
    return from < 0 ? null : { from, options: CPP_HEADERS, validFor: /^[\w./+]*$/ }
  }
  const listSource = (ctx: any) => {
    if (includeFrom(ctx) >= 0) return null // include 上下文不混普通词
    const word = ctx.matchBefore(/\w*/)
    if (!word || (word.from === word.to && !ctx.explicit)) return null
    const items = props.language === 'python3' ? PY_COMPLETIONS : CPP_COMPLETIONS
    return { from: word.from, options: items, validFor: /^\w*$/ }
  }
  const wordSource = (ctx: any) => (includeFrom(ctx) >= 0 ? null : autocompleteMod.completeAnyWord(ctx))
  langComp = new state.Compartment()
  acComp = new state.Compartment()
  themeComp = new state.Compartment()
  viewMod = vm
  acFactory = () => autocompleteMod.autocompletion({ override: [includeSource, listSource, wordSource] })
  const exts: any[] = [
    cm.basicSetup,
    langComp.of(lang),
    themeComp.of(buildTheme(vm, props.fontFamily, props.fontSize)),
    languageMod.indentUnit.of('    '), // 缩进单位统一 4 空格（自动缩进与 Tab 插入共用）
  ]
  // Tab：补全弹层开着时优先接受补全（CM6 默认键位不含 Tab，此前 indentWithTab 独占导致弹层开着也缩进）；
  // 无弹层时 acceptCompletion 返回 false 自然落回缩进（VS Code 同款）。补全关闭时无弹层，该绑定恒落空无害。
  if (props.tabIndent)
    exts.push(vm.keymap.of([{ key: 'Tab', run: autocompleteMod.acceptCompletion }, commandsMod.indentWithTab]))
  if (props.autocomplete) exts.push(acComp.of(acFactory()))
  if (props.placeholder) exts.push(vm.placeholder(props.placeholder))
  // 控制流语句（for/if/while 等）以 ) 结尾时，换行新行与该行对齐而非 +1 缩进——
  // 适配大括号独立成行的写法：for (...)\n{\n    ...\n}（函数调用的多行参数不受影响，仍 +1）
  if (props.language !== 'python3') {
    exts.push(
      languageMod.indentService.of((context: any, pos: number) => {
        const line = context.lineAt(pos)
        const t = line.text.trimEnd()
        // ① 控制流行悬空：新行与 for/if/while 对齐（大括号独立成行的写法）
        if (/(^|\s)(for|if|else if|while|switch)\s*\(.*\)\s*$/.test(t)) {
          return (t.match(/^\s*/)?.[0] ?? '').length
        }
        // ② 上一行以 { 结尾：新行进入块内，+1 缩进单位（4 空格）
        if (pos > 0) {
          const prev = context.lineAt(pos - 1)
          if (/\{\s*$/.test(prev.text.trimEnd())) {
            return (prev.text.match(/^\s*/)?.[0] ?? '').length + 4
          }
        }
        return null
      }),
    )
  }
  exts.push(
    vm.EditorView.updateListener.of((u: any) => {
      if (u.docChanged) {
        inner = u.state.doc.toString()
        emit('update:modelValue', inner)
      }
    }),
  )
  if (!el.value) return // 等待 chunk 加载期间组件已卸载
  view = new cm.EditorView({ doc: props.modelValue, extensions: exts, parent: el.value })
})

watch(
  () => props.modelValue,
  (v) => {
    if (view && v !== inner) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v ?? '' } })
      inner = v
    }
  },
)

watch(
  () => props.language,
  async (lang) => {
    // Compartment.reconfigure 只产出 effect，必须 dispatch 才会生效
    if (!view || !langComp) return
    view.dispatch({ effects: langComp.reconfigure(await langExt(lang)) })
  },
)

watch(
  () => props.autocomplete,
  (v) => {
    if (!view || !acComp) return
    view.dispatch({ effects: acComp.reconfigure(v && acFactory ? acFactory() : []) })
  },
)

watch([() => props.fontFamily, () => props.fontSize], ([fontFamily, fontSize]) => {
  // 同上：reconfigure 的 effect 经 view.dispatch 应用，字号/字体才能实时重绘
  if (!view || !themeComp || !viewMod) return
  view.dispatch({ effects: themeComp.reconfigure(buildTheme(viewMod, fontFamily, fontSize)) })
})

onUnmounted(() => view?.destroy())
</script>

<template>
  <!-- 包装层撑满父容器（el-form-item__content 是 flex，子项不会自动拉伸）；CM6 高度由包装层决定 -->
  <div class="code-editor" :style="{ height: props.height }">
    <div ref="el"></div>
  </div>
</template>

<style scoped>
.code-editor {
  width: 100%;
  /* border-box：自带 1px 边框必须算进 100%，否则在卡体里恒溢出 2px 画出横向滚动条 */
  box-sizing: border-box;
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  overflow: hidden;
}

/* 高度链：包装层(高度 prop) → 挂载层(100%) → .cm-editor(100%,主题) → .cm-scroller(滚动) */
.code-editor > div {
  height: 100%;
}

.code-editor :deep(.cm-editor) {
  height: 100%;
}

.code-editor :deep(.cm-editor.cm-focused) {
  outline: none;
}

.code-editor :deep(.cm-scroller) {
  overflow: auto;
}

.code-editor :deep(.cm-gutters) {
  border-right: 1px solid var(--el-border-color);
  background: var(--el-fill-color-lighter);
}

</style>

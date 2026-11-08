<script setup lang="ts">
// 代码对比工具页（v0.47）：双栏贴代码 → @codemirror/merge 分屏/上下 diff，页脚「代码对比」入口。
// 登录后可用（requiresAuth）。v0.83 起显示恒为**原文**（原格式 + 注释保留），「过滤注释/忽略
// 空白」开关只改比对口径——merge 的 diffConfig.override 内先归一两侧再 diff，坐标映射回原文
// 行边界（utils/codeDiff.ts mapNormalizedChanges）。观感与站内代码查看器同源：CM6 高亮 +
// 只读态（readOnly + editable:false）+ Consolas 主题，CodeEditor.vue 同款链路全量动态 import。
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'

import { ElMessage } from 'element-plus'

import CodeEditor from '@/components/CodeEditor.vue'
import { languageOptions } from '@/utils/code'
import { mapNormalizedChanges, prepareForDiff } from '@/utils/codeDiff'

const lang = ref('cpp')
const filterComments = ref(true)
// 忽略空白（v0.83）：同算法两份代码一份格式化一份压缩时整篇标差淹没真差异——字面量外
// 行内空白移除后仅剩内容差异。Python 缩进即语法禁用（watch 里联动回落）
const ignoreWs = ref(true)
const mode = ref<'split' | 'unified'>('split')
const srcA = ref('') // 参考代码（标程 / 正确版本）
const srcB = ref('') // 待比代码（学生 / 待查版本）
const compared = ref(false) // 首次「开始对比」后展示结果卡
const resultEl = ref<HTMLDivElement>()
const resultCard = ref<HTMLDivElement>()

// 视图实例用 import type 持有类型（纯类型引用不产生静态 import，懒加载分包不受影响）
let splitView: import('@codemirror/merge').MergeView | null = null
let unifiedView: import('codemirror').EditorView | null = null

// 语言包懒加载（与 CodeEditor.vue 同款：C 与 C++ 共用 cpp 高亮）
async function langExt(language: string) {
  if (language === 'python3') {
    const { python } = await import('@codemirror/lang-python')
    return python()
  }
  const { cpp } = await import('@codemirror/lang-cpp')
  return cpp()
}

// 主题与 CodeEditor.buildTheme 同款（结果区只读，字号字体固定 14/Consolas）
function buildTheme(vm: typeof import('@codemirror/view')) {
  return vm.EditorView.theme({
    '&': { fontSize: '14px' },
    '.cm-content': { fontFamily: '"Consolas", Menlo, Monaco, monospace' },
    '.cm-gutters': { fontFamily: '"Consolas", Menlo, monospace' },
    '.cm-line': { lineHeight: '1.6' },
  })
}

function destroyViews() {
  splitView?.destroy()
  splitView = null
  unifiedView?.destroy()
  unifiedView = null
}

async function rebuild() {
  if (!compared.value || !resultEl.value) return
  // v0.83「原文显示」：merge 展示原文（原格式 + 注释保留），比对口径走 diffConfig.override
  // ——override 内先按开关归一两侧（行数 1:1），用包内 presentableDiff 求 diff，再把归一
  // 坐标映射回原文行边界（mapNormalizedChanges）。纯注释差异/空白差异因此不高亮且原文可见。
  const ws = ignoreWs.value && !wsDisabled.value
  const strip = filterComments.value
  const textA = srcA.value
  const textB = srcB.value
  destroyViews()
  const [cm, state, vm, merge, language, searchZhMod] = await Promise.all([
    import('codemirror'),
    import('@codemirror/state'),
    import('@codemirror/view'),
    import('@codemirror/merge'),
    langExt(lang.value),
    import('@/utils/cmSearchZh'), // 搜索面板中文化（默认面板英文裸样式；utils/cmSearchZh）
  ])
  if (!resultEl.value) return // chunk 加载期间组件已卸载或已清空
  const theme = buildTheme(vm)
  // 只读态同 CodeEditor：readOnly 挡编辑命令，editable:false 去光标，仍可选中复制
  const readOnly = [state.EditorState.readOnly.of(true), vm.EditorView.editable.of(false)]
  const diffConfig: import('@codemirror/merge').DiffConfig = {
    override: (ra, rb) => {
      const na = prepareForDiff(ra, lang.value, strip, ws)
      const nb = prepareForDiff(rb, lang.value, strip, ws)
      const raw = merge.presentableDiff(na, nb, { scanLimit: 500 })
      return mapNormalizedChanges(ra, rb, na, nb, raw).map(
        (c) => new merge.Change(c.fromA, c.toA, c.fromB, c.toB),
      )
    },
  }
  if (mode.value === 'split') {
    splitView = new merge.MergeView({
      a: { doc: textA, extensions: [cm.basicSetup, searchZhMod.searchZh(), language, theme, ...readOnly] },
      b: { doc: textB, extensions: [cm.basicSetup, searchZhMod.searchZh(), language, theme, ...readOnly] },
      gutter: true,
      highlightChanges: true,
      // 相同段落折叠：长代码只展开差异段附近
      collapseUnchanged: { margin: 3, minSize: 4 },
      diffConfig,
    })
    resultEl.value.replaceChildren(splitView.dom)
  } else {
    unifiedView = new cm.EditorView({
      doc: textB,
      extensions: [
        cm.basicSetup,
        searchZhMod.searchZh(),
        language,
        theme,
        ...readOnly,
        merge.unifiedMergeView({ original: textA, diffConfig }),
      ],
      parent: resultEl.value,
    })
  }
}

function compare() {
  if (!srcA.value.trim() || !srcB.value.trim()) {
    ElMessage.warning('两侧代码都要填写')
    return
  }
  compared.value = true
  void nextTick()
    .then(rebuild)
    .then(() => resultCard.value?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
}

function swap() {
  ;[srcA.value, srcB.value] = [srcB.value, srcA.value]
  if (compared.value) void nextTick().then(rebuild)
}

function clearAll() {
  srcA.value = ''
  srcB.value = ''
  compared.value = false
  destroyViews()
}

// python3 切换时禁用忽略空白：缩进即语法，行首缩进移除会把不同逻辑错误等价——
// 已开则回落为关（提示一次），开关置灰
const wsDisabled = computed(() => lang.value === 'python3')
watch(wsDisabled, (d) => {
  if (d && ignoreWs.value) {
    ignoreWs.value = false
    ElMessage.info('Python 缩进即语法，不支持忽略空白')
  }
})

// 语言/过滤/模式变化：已出结果则即时重比（输入文本是唯一真源，结果区不回写）
watch([lang, filterComments, ignoreWs, mode], () => {
  if (compared.value) void nextTick().then(rebuild)
})

onUnmounted(destroyViews)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div class="kicker">CODE DIFF</div>
      <h1 class="page-title">代码对比<small>贴入两份代码查看差异；开关只改比对口径，差异区始终显示原文</small></h1>
    </div>

    <!-- 输入卡：工具栏 + 双栏编辑器 -->
    <div class="card tool-card">
      <div class="toolbar">
        <el-select v-model="lang" class="lang-sel">
          <el-option v-for="o in languageOptions" :key="o.value" :label="o.label" :value="o.value" />
        </el-select>
        <el-switch v-model="filterComments" active-text="过滤注释" />
        <el-tooltip content="移除缩进与运算符旁空格再比对（字符串内空格保留）；Python 缩进即语法不可用" placement="top">
          <span>
            <el-switch v-model="ignoreWs" :disabled="wsDisabled" active-text="忽略空白" />
          </span>
        </el-tooltip>
        <el-radio-group v-model="mode">
          <el-radio-button value="split">分屏对比</el-radio-button>
          <el-radio-button value="unified">上下对比</el-radio-button>
        </el-radio-group>
        <span class="tb-spacer"></span>
        <el-button @click="swap">交换左右</el-button>
        <el-button @click="clearAll">清空</el-button>
        <el-button type="primary" @click="compare">开始对比</el-button>
      </div>
      <div class="inputs">
        <div class="input-col">
          <div class="input-label">参考代码（标程 / 正确版本）</div>
          <CodeEditor v-model="srcA" :language="lang" height="300px" :autocomplete="false" placeholder="贴入参考代码…" />
        </div>
        <div class="input-col">
          <div class="input-label">待比代码（学生 / 待查版本）</div>
          <CodeEditor v-model="srcB" :language="lang" height="300px" :autocomplete="false" placeholder="贴入待比代码…" />
        </div>
      </div>
    </div>

    <!-- 结果卡：首次对比后出现；高度收口，长代码靠相同段折叠 + 区内滚动 -->
    <div v-if="compared" ref="resultCard" class="card tool-card result-card">
      <div class="result-head">
        <span class="result-title">差异结果</span>
        <span class="result-hint">{{
          [
            ignoreWs && !wsDisabled ? '忽略空白：空白差异不高亮' : '',
            filterComments ? '过滤注释：纯注释差异不高亮' : '',
            ignoreWs || filterComments ? '显示原文' : '原文对比：空白与注释差异同样标出',
          ]
            .filter(Boolean)
            .join('，')
        }}</span>
      </div>
      <div ref="resultEl" class="diff-result"></div>
    </div>
  </div>
</template>

<style scoped>
.tool-card {
  padding: var(--soj-sp-20);
}

/* toolbar 形态收编全局 .toolbar（flex/wrap/gap/mb），本地只留增量 */

.lang-sel {
  width: 128px;
}

.tb-spacer {
  flex: 1;
}

.inputs {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--soj-sp-16);
  margin-top: var(--soj-sp-16);
}

.input-label {
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-ink-2);
  margin-bottom: var(--soj-sp-8);
}

/* <992px 折单列（与 PageShell 双列折叠同断点口径） */
@media (max-width: 992px) {
  .inputs {
    grid-template-columns: 1fr;
  }
}

.result-card {
  margin-top: var(--soj-sp-16);
}

.result-head {
  display: flex;
  align-items: baseline;
  gap: var(--soj-sp-12);
  margin-bottom: var(--soj-sp-12);
}

.result-title {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
}

.result-hint {
  font-size: var(--soj-fs-small);
  color: var(--soj-ink-3);
}

/* 结果区高度链：容器定高 → mergeView 双编辑器铺满；上下模式为单 .cm-editor */
.diff-result {
  height: calc(100vh - 260px);
  min-height: 380px;
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  overflow: hidden;
}

.diff-result :deep(.cm-mergeView),
.diff-result :deep(.cm-mergeView-editor) {
  height: 100%;
}

.diff-result :deep(.cm-editor) {
  height: 100%;
}

.diff-result :deep(.cm-editor.cm-focused) {
  outline: none;
}
</style>

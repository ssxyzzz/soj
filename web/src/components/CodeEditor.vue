<script setup lang="ts">
// 代码编辑器（v0.5d）：CodeMirror 6 共享组件，提交弹窗与在线编程复用。
// CM6 全量走动态 import → 独立懒加载 chunk（不进主包、零 CDN）；
// 语言与自动补全用 Compartment 热切换；自动补全开关走 utils/code.ts 的策略接口（预留等级门槛）。
import { onMounted, onUnmounted, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    language?: string // cpp / c / python3（c 复用 cpp 高亮）
    height?: string // 如 '420px'；'100%' 时父容器需有确定高度
    autocomplete?: boolean
    placeholder?: string
  }>(),
  { language: 'cpp', height: '420px', autocomplete: true, placeholder: '' },
)

const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const el = ref<HTMLDivElement>()
let view: any = null // EditorView（CM6 经动态 import，类型取不到静态声明，统一 any）
let inner = props.modelValue // 防止 setValue 触发的回显循环
let langComp: any = null
let acComp: any = null
let acFactory: (() => any) | null = null

// 语言包懒加载（C 与 C++ 共用 C++ 高亮；Java/Go 届时加包）
async function langExt(language: string) {
  if (language === 'python3') {
    const { python } = await import('@codemirror/lang-python')
    return python()
  }
  const { cpp } = await import('@codemirror/lang-cpp')
  return cpp()
}

// 常用函数 / 关键字精选清单（v0.5d 功能基线）：静态数据，取自题面语言即生效。
// 语义级补全（clangd/LSP）在边界之外——这里只有词法 / 关键字 / 片段级。
const COMPLETIONS: Record<string, { label: string; type: string; detail: string; apply: string }[]> = {
  cpp: [
    { label: 'sort', type: 'function', detail: '排序 [first, last)', apply: 'sort(begin, end)' },
    { label: 'push_back', type: 'method', detail: 'vector 尾部插入', apply: 'push_back()' },
    { label: 'pop_back', type: 'method', detail: 'vector 尾部弹出', apply: 'pop_back()' },
    { label: 'size', type: 'method', detail: '容器大小', apply: 'size()' },
    { label: 'begin', type: 'method', detail: '起始迭代器', apply: 'begin()' },
    { label: 'end', type: 'method', detail: '末尾迭代器', apply: 'end()' },
    { label: 'freopen', type: 'function', detail: '重定向输入输出（文件 IO）', apply: 'freopen("", "r", stdin)' },
    { label: 'scanf', type: 'function', detail: '格式化读入', apply: 'scanf("", )' },
    { label: 'printf', type: 'function', detail: '格式化输出', apply: 'printf("", )' },
    { label: 'cin', type: 'keyword', detail: '标准输入流', apply: 'cin >> ' },
    { label: 'cout', type: 'keyword', detail: '标准输出流', apply: 'cout << ' },
    { label: 'endl', type: 'keyword', detail: '换行并刷新', apply: 'endl' },
    { label: 'vector', type: 'class', detail: '动态数组', apply: 'vector<' },
    { label: 'pair', type: 'class', detail: '键值对', apply: 'pair<' },
    { label: 'string', type: 'class', detail: '字符串', apply: 'string' },
    { label: 'queue', type: 'class', detail: '队列', apply: 'queue<' },
    { label: 'stack', type: 'class', detail: '栈', apply: 'stack<' },
    { label: 'unordered_map', type: 'class', detail: '哈希表', apply: 'unordered_map<' },
    { label: 'min', type: 'function', detail: '最小值', apply: 'min(a, b)' },
    { label: 'max', type: 'function', detail: '最大值', apply: 'max(a, b)' },
    { label: 'abs', type: 'function', detail: '绝对值', apply: 'abs(x)' },
    { label: 'memset', type: 'function', detail: '内存填充初始化', apply: 'memset(a, 0, sizeof(a))' },
    { label: 'long long', type: 'keyword', detail: '64 位整型', apply: 'long long ' },
    { label: '#include', type: 'keyword', detail: '头文件引入', apply: '#include <>' },
    { label: 'int main', type: 'function', detail: '主函数骨架', apply: 'int main() {\n    \n    return 0;\n}' },
  ],
  python3: [
    { label: 'print', type: 'function', detail: '输出', apply: 'print()' },
    { label: 'input', type: 'function', detail: '读入一行', apply: 'input()' },
    { label: 'int', type: 'function', detail: '转整型', apply: 'int()' },
    { label: 'float', type: 'function', detail: '转浮点', apply: 'float()' },
    { label: 'str', type: 'function', detail: '转字符串', apply: 'str()' },
    { label: 'len', type: 'function', detail: '长度', apply: 'len()' },
    { label: 'range', type: 'function', detail: '整数区间', apply: 'range()' },
    { label: 'sort', type: 'method', detail: '列表原地排序', apply: 'sort()' },
    { label: 'append', type: 'method', detail: '列表尾部插入', apply: 'append()' },
    { label: 'split', type: 'method', detail: '按分隔符切分', apply: 'split()' },
    { label: 'map', type: 'function', detail: '映射转换', apply: 'map()' },
    { label: 'max', type: 'function', detail: '最大值', apply: 'max()' },
    { label: 'min', type: 'function', detail: '最小值', apply: 'min()' },
    { label: 'sum', type: 'function', detail: '求和', apply: 'sum()' },
    { label: 'abs', type: 'function', detail: '绝对值', apply: 'abs()' },
    { label: 'enumerate', type: 'function', detail: '带下标遍历', apply: 'enumerate()' },
    { label: 'for', type: 'keyword', detail: '循环骨架', apply: 'for i in range(n):\n    ' },
    { label: 'while', type: 'keyword', detail: '条件循环', apply: 'while :\n    ' },
    { label: 'if __name__ == "__main__"', type: 'snippet', detail: '主入口骨架', apply: 'if __name__ == "__main__":\n    ' },
  ],
}

onMounted(async () => {
  const [cm, state, viewMod, autocompleteMod, lang] = await Promise.all([
    import('codemirror'),
    import('@codemirror/state'),
    import('@codemirror/view'),
    import('@codemirror/autocomplete'),
    langExt(props.language),
  ])
  // 常用清单补全源：仅在词首触发（Ctrl+Space 可显式唤起）
  const listSource = (ctx: any) => {
    const word = ctx.matchBefore(/\w*/)
    if (!word || (word.from === word.to && !ctx.explicit)) return null
    const items = COMPLETIONS[props.language] ?? COMPLETIONS.cpp
    return { from: word.from, options: items, validFor: /^\w*$/ }
  }
  langComp = new state.Compartment()
  acComp = new state.Compartment()
  acFactory = () => autocompleteMod.autocompletion({ override: [listSource] })
  const exts: any[] = [cm.basicSetup, langComp.of(lang)]
  if (props.autocomplete) exts.push(acComp.of(acFactory()))
  if (props.placeholder) exts.push(viewMod.placeholder(props.placeholder))
  exts.push(
    viewMod.EditorView.updateListener.of((u: any) => {
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
    if (!view) return
    langComp?.reconfigure(await langExt(lang))
  },
)

watch(
  () => props.autocomplete,
  (v) => {
    acComp?.reconfigure(v && acFactory ? acFactory() : [])
  },
)

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
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  overflow: hidden;
}

.code-editor :deep(.cm-editor) {
  height: 100%;
}

.code-editor :deep(.cm-editor.cm-focused) {
  outline: none;
}

.code-editor :deep(.cm-gutters) {
  border-right: 1px solid #dcdfe6;
  background: #fafafa;
}
</style>

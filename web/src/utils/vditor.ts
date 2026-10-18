// Vditor（Markdown 编辑/渲染内核）体积大，动态 import 拆出主包：
// 首次挂载编辑器/渲染组件时才加载 JS chunk，不拖累首屏。
// 样式（index.css）不走这里——它在 main.ts 静态引入，保证排在 main.css 之前，
// 否则懒加载样式会反压 main.css 里的 vditor-reset 排版规则（见 main.ts 注释）。
type VditorCtor = (typeof import('vditor'))['default']

let loading: Promise<VditorCtor> | null = null

export function loadVditor(): Promise<VditorCtor> {
  loading ??= import('vditor').then((mod) => mod.default)
  return loading
}

export type VditorInstance = InstanceType<VditorCtor>

// 与服务端 maxStatementSize（server/internal/server/problem.go，256KB 字节）对应。
// Lute 在浏览器主线程渲染，超限内容会卡死页面：服务端拒绝保存，查看端拒绝渲染。
export const MAX_MARKDOWN_BYTES = 256 << 10

const utf8 = new TextEncoder()
export function markdownBytes(s: string): number {
  return utf8.encode(s).length
}

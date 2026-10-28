// Vditor 懒加载子资源自托管（v0.8；v0.26.2 起版本化）：把 node_modules/vditor/dist
// 拷进 public/vditor/<version>/dist——版本号进路径后，服务端对该前缀发 immutable
// （版本即内容锁，升级 vditor 时版本变路径变缓存自动失效），lute.min.js 3.6MB 从
// 「每次 no-cache 全量重下」变为「同版本磁盘缓存直读 + V8 code cache 命中」。
// 前端 cdn 基址统一从 src/utils/vditorCdn.ts 取（读同一份 package.json，构建期内联）。
// public/ 不入库（.gitignore 已含 web/public/vditor/），dev 与 build 前都重拷保证新鲜。
import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = dirname(dirname(fileURLToPath(import.meta.url)))
const pkg = JSON.parse(readFileSync(join(webRoot, 'node_modules/vditor/package.json'), 'utf8'))
const version = pkg.version
if (!version) {
  console.error('node_modules/vditor/package.json 缺 version，无法版本化')
  process.exit(1)
}
const src = join(webRoot, 'node_modules/vditor/dist')
// 目标必须带 /dist 段（与注释及 vditor 运行时一致）：vditor 硬编码请求
// `${cdn}/dist/js/...`，拷成 public/vditor/<version>/js 会让所有子资源 404——
// 且服务端 vditorVersionRedirect 对「同版本文件缺失」会 302 到同路径造成重定向死循环
//（v0.26.2 验收期实锤：题面/题解/讨论 Markdown 全部渲染瘫痪）
const dst = join(webRoot, 'public/vditor', version, 'dist')

if (!existsSync(src)) {
  console.error('node_modules/vditor/dist 不存在：请先 npm install')
  process.exit(1)
}
mkdirSync(dst, { recursive: true })
// 清掉全部历史版本目录再拷当前版（public 是构建中转目录，只保留一个当前版本）
for (const name of readdirSync(join(webRoot, 'public/vditor'))) {
  rmSync(join(webRoot, 'public/vditor', name), { recursive: true, force: true })
}
cpSync(src, dst, { recursive: true })
console.log(`vditor dist → public/vditor/${version} 完成`)

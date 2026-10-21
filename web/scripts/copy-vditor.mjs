// Vditor 懒加载子资源自托管（v0.8）：把 node_modules/vditor/dist 拷进 public/vditor/dist，
// 编辑器/查看端配置 cdn:'/vditor' 后零外部 CDN（KaTeX/highlight/内容主题都从这走）。
// public/ 不入库（.gitignore 已含 web/public/vditor/），dev 与 build 前都重拷保证新鲜。
import { cpSync, existsSync, mkdirSync, rmSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = dirname(dirname(fileURLToPath(import.meta.url)))
const src = join(webRoot, 'node_modules/vditor/dist')
const dst = join(webRoot, 'public/vditor/dist')

if (!existsSync(src)) {
  console.error('node_modules/vditor/dist 不存在：请先 npm install')
  process.exit(1)
}
mkdirSync(join(webRoot, 'public/vditor'), { recursive: true })
rmSync(dst, { recursive: true, force: true })
cpSync(src, dst, { recursive: true })
console.log('vditor dist → public/vditor/dist 完成')

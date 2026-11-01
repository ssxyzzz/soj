// Vditor 自托管 CDN 基址（v0.26.2 版本化）：版本号进路径 = 内容锁，服务端对该
// 前缀发 immutable（同版本磁盘缓存直读 + V8 code cache 命中，lute 3.6MB 不再每次
// 回源重下）；升级 vditor 时版本变、路径变、旧缓存自动失效。
// 版本读自 node_modules/vditor/package.json，与 copy-vditor.mjs 同源，构建期内联。
// v0.52 编辑器内芯换血后 Vditor 仅剩查看端 md 模式（首页公告/讨论区等无 html 缓存
// 的直渲场景，见 MdViewInner）——编辑端已换 CodeMirror 6（MdEditorInner），
// VDITOR_LUTE_URL 预取导出随编辑端退役。
import pkg from 'vditor/package.json'

export const VDITOR_VERSION = pkg.version
export const VDITOR_CDN = `/vditor/${pkg.version}`

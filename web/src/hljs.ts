// hljs 精简包（加载优化批）：题面/题解/讨论/编辑器预览的代码高亮唯一实现
// （v0.53 渲染收口后无第二引擎）。highlight.min.js 全语言包（304KB gzip）换成
// core + 实际语言集——判题语言只有 c/cpp/python3，题面围栏里常见的是配置/脚本类，
// 全部约 25KB gzip 级。经 vite 打包成独立 hashed chunk（immutable 一年 +
// 会话内 import 缓存），按需动态 import（首个含代码块的内容触发）。
// 主题（v0.53 渲染收口随批修复）：github 浅色主题随本 chunk 加载——原主题捆绑在
// 已退役的 vditor index.css 里，删依赖时一并消失，样例卡 code 失去 display:block
// 退化 inline（行号列被正文压住）且 token 全无色。`pre code.hljs{padding:1em}` 与
// main.css 显式内距 (0,2,1) 相比恒败，对齐契约（rows.top = padding-top = 0.5em）
// 不受影响；布局基础（display:block/overflow-x）在 main.css 另有显式声明双保险。
import 'highlight.js/styles/github.css'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import c from 'highlight.js/lib/languages/c'
import cmake from 'highlight.js/lib/languages/cmake'
import cpp from 'highlight.js/lib/languages/cpp'
import delphi from 'highlight.js/lib/languages/delphi' // Pascal 语法（hljs 命名 delphi）
import ini from 'highlight.js/lib/languages/ini'
import java from 'highlight.js/lib/languages/java'
import json from 'highlight.js/lib/languages/json'
import python from 'highlight.js/lib/languages/python'
import sql from 'highlight.js/lib/languages/sql'
import yaml from 'highlight.js/lib/languages/yaml'

const langs = { bash, c, cmake, cpp, delphi, ini, java, json, python, sql, yaml }
for (const [name, def] of Object.entries(langs)) hljs.registerLanguage(name, def)

export default hljs

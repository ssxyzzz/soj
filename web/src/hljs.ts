// hljs 精简包（加载优化批）：题面/题解/讨论的代码高亮从 vditor 自带的
// highlight.min.js 全语言包（304KB gzip）换成 core + 实际语言集——判题语言只有
// c/cpp/python3，题面围栏里常见的是配置/脚本类，全部约 25KB gzip 级。
// 经 vite 打包成独立 hashed chunk（immutable 一年 + 会话内 import 缓存），
// 取代 MdViewInner 原 loadScript 拉 /vditor CDN 全量包的链路。
// 编辑器内部预览仍走 vditor 自带包（编辑器路由、教师群体，不在本链路）。
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

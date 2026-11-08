// 代码对比预处理（v0.47，/diff 工具页）：行尾归一 + 注释剥离。
// 「过滤注释」没有任何 diff 组件原生认识注释（对 differ 注释只是普通文本），根解法是在
// 喂给 @codemirror/merge 之前先把两侧代码过一遍本模块：C/C++ 剥 // 与 /* */（状态机识别
// 字符串/字符字面量防误剥，含 R"(...)" 原始字符串），Python 剥 #（三引号字符串是字符串
// 不是注释，内容保留）。注释文本清空但**换行原样保留**——纯注释行变空行，行号与原代码
// 严格对齐；剥离后逐行去行尾空白（两侧一致处理，消行尾空白噪音）。

// CRLF/CR → LF：贴入的学生代码常见 Windows 行尾，与注释过滤无关、恒定执行
export function normalizeLineEndings(code: string): string {
  return code.replace(/\r\n?/g, '\n')
}

// code[idx] 前紧邻的标识符串（识别 R"..." 的原始字符串前缀 R/uR/UR/LR/u8R）
function identRunEndingAt(code: string, idx: number): string {
  let j = idx - 1
  while (j >= 0 && /[A-Za-z0-9_]/.test(code[j])) j--
  return code.slice(j + 1, idx)
}

function stripCpp(code: string): string {
  const n = code.length
  let out = ''
  let i = 0
  while (i < n) {
    const c = code[i]
    // 行注释：// 到行尾（换行留给 normal 态输出）
    if (c === '/' && code[i + 1] === '/') {
      while (i < n && code[i] !== '\n') i++
      continue
    }
    // 块注释：/* 到 */，跨行时换行原样输出保行号
    if (c === '/' && code[i + 1] === '*') {
      i += 2
      while (i < n) {
        if (code[i] === '*' && code[i + 1] === '/') {
          i += 2
          break
        }
        if (code[i] === '\n') out += '\n'
        i++
      }
      continue
    }
    if (c === '"') {
      // 原始字符串 R"delim(...)delim"：括号定界内无转义/注释语义，整体原样保留
      if (/^(u8|u|U|L)?R$/.test(identRunEndingAt(code, i))) {
        const open = code.indexOf('(', i + 1) // delim 不含括号，首个 ( 即开界
        if (open !== -1) {
          const closeStr = ')' + code.slice(i + 1, open) + '"'
          const close = code.indexOf(closeStr, open + 1)
          const end = close === -1 ? n : close + closeStr.length
          out += code.slice(i, end) // 未闭合按原始字符串吞到 EOF（词法错误兜底）
          i = end
          continue
        }
      }
      // 普通字符串：转义原样保留；裸换行即收口（字符串本不跨行，防未闭合吞掉后续注释）
      out += c
      i++
      while (i < n) {
        const s = code[i]
        if (s === '\\' && i + 1 < n) {
          out += code.slice(i, i + 2)
          i += 2
          continue
        }
        if (s === '"' || s === '\n') {
          if (s === '"') {
            out += s
            i++
          }
          break
        }
        out += s
        i++
      }
      continue
    }
    if (c === "'") {
      // 字符字面量：同字符串处理（'/' 常见，别把内容当注释剥）
      out += c
      i++
      while (i < n) {
        const s = code[i]
        if (s === '\\' && i + 1 < n) {
          out += code.slice(i, i + 2)
          i += 2
          continue
        }
        if (s === "'" || s === '\n') {
          if (s === "'") {
            out += s
            i++
          }
          break
        }
        out += s
        i++
      }
      continue
    }
    out += c
    i++
  }
  return out
}

function stripPython(code: string): string {
  const n = code.length
  let out = ''
  let i = 0
  while (i < n) {
    const c = code[i]
    // 行注释：# 到行尾（Python 无块注释）
    if (c === '#') {
      while (i < n && code[i] !== '\n') i++
      continue
    }
    if (c === '"' || c === "'") {
      const q = c
      const triple = code.slice(i, i + 3) === q.repeat(3)
      if (triple) {
        // 三引号字符串（含 docstring）：是字符串不是注释，整体原样保留
        const end = code.indexOf(q.repeat(3), i + 3)
        const stop = end === -1 ? n : end + 3
        out += code.slice(i, stop)
        i = stop
        continue
      }
      // 普通字符串：转义原样保留；裸换行收口
      out += c
      i++
      while (i < n) {
        const s = code[i]
        if (s === '\\' && i + 1 < n) {
          out += code.slice(i, i + 2)
          i += 2
          continue
        }
        if (s === q || s === '\n') {
          if (s === q) {
            out += s
            i++
          }
          break
        }
        out += s
        i++
      }
      continue
    }
    out += c
    i++
  }
  return out
}

// 剥注释（lang 取 languageOptions 口径：c 走 cpp 同套注释语法）+ 逐行去行尾空白
export function stripComments(code: string, lang: string): string {
  const stripped = lang === 'python3' ? stripPython(code) : stripCpp(code)
  return stripped
    .split('\n')
    .map((line) => line.replace(/\s+$/, ''))
    .join('\n')
}

// ---- 忽略空白（v0.83）：字符串感知的逐行行内空白移除 ----
// 场景（用户直拍）：同算法两份代码一份格式化（bs = 1）一份压缩（bs=1），merge 整篇标差，
// 真差异（a[105] vs a[101]）被淹没。归一规则：字符串/字符/原始字符串字面量**内部**空白
// 原样保留（" " 与 " " 是不同输出），字面量**外部**空白（缩进 + 运算符旁空格）全移除——
// 须全移而非压单空格，否则 bs = 1 与 bs=1 仍不等价。换行保留：merge 按行 diff，「行号与
// 原代码对齐」原则不破。已知取舍（对比启发式，非编译器）：字面量外删空白会把 a + +b 与
// a ++b 错误等价——学生 OJ 代码编译可过，此类写法实际不可出现，接受。

// C/C++ 版：状态机与 stripCpp 同款字符串/字符/原始字符串识别（防把 " " 内空白移走）
function collapseCpp(code: string): string {
  const n = code.length
  let out = ''
  let i = 0
  while (i < n) {
    const c = code[i]
    if (c === '"') {
      if (/^(u8|u|U|L)?R$/.test(identRunEndingAt(code, i))) {
        const open = code.indexOf('(', i + 1)
        if (open !== -1) {
          const closeStr = ')' + code.slice(i + 1, open) + '"'
          const close = code.indexOf(closeStr, open + 1)
          const end = close === -1 ? n : close + closeStr.length
          out += code.slice(i, end)
          i = end
          continue
        }
      }
      out += c
      i++
      while (i < n) {
        const s = code[i]
        if (s === '\\' && i + 1 < n) {
          out += code.slice(i, i + 2)
          i += 2
          continue
        }
        if (s === '"' || s === '\n') {
          if (s === '"') {
            out += s
            i++
          }
          break
        }
        out += s
        i++
      }
      continue
    }
    if (c === "'") {
      out += c
      i++
      while (i < n) {
        const s = code[i]
        if (s === '\\' && i + 1 < n) {
          out += code.slice(i, i + 2)
          i += 2
          continue
        }
        if (s === "'" || s === '\n') {
          if (s === "'") {
            out += s
            i++
          }
          break
        }
        out += s
        i++
      }
      continue
    }
    if (/\s/.test(c)) {
      i++ // 字面量外空白移除（缩进 + 运算符旁空格）；换行原样保留保行结构
      if (c === '\n') out += '\n'
      continue
    }
    out += c
    i++
  }
  return out
}

// Python 版：三引号/单双引号字符串内部原样；外部空白同款移除。
// 注意：仅 C/C++ 提供「忽略空白」开关（Python 缩进即语法，行首缩进移除会把不同逻辑
// 错误等价），python3 不走本函数（DiffView 开关对 python3 置灰）。
function collapsePython(code: string): string {
  const n = code.length
  let out = ''
  let i = 0
  while (i < n) {
    const c = code[i]
    if (c === '"' || c === "'") {
      const q = c
      const triple = code.slice(i, i + 3) === q.repeat(3)
      if (triple) {
        const end = code.indexOf(q.repeat(3), i + 3)
        const stop = end === -1 ? n : end + 3
        out += code.slice(i, stop)
        i = stop
        continue
      }
      out += c
      i++
      while (i < n) {
        const s = code[i]
        if (s === '\\' && i + 1 < n) {
          out += code.slice(i, i + 2)
          i += 2
          continue
        }
        if (s === q || s === '\n') {
          if (s === q) {
            out += s
            i++
          }
          break
        }
        out += s
        i++
      }
      continue
    }
    if (/\s/.test(c)) {
      i++
      if (c === '\n') out += '\n'
      continue
    }
    out += c
    i++
  }
  return out
}

// 忽略空白归一（lang 同 stripComments 口径）；换行原样保留、行号与原代码严格对齐
export function collapseWhitespace(code: string, lang: string): string {
  const collapsed = lang === 'python3' ? collapsePython(code) : collapseCpp(code)
  return collapsed
    .split('\n')
    .map((line) => line.replace(/\s+$/, ''))
    .join('\n')
}

// /diff 页统一入口：恒做行尾归一；strip 时剥注释；collapse 时忽略行内空白（先剥注释后压
// 空白，压空白只作用于最终参与对比的文本）
export function prepareForDiff(
  code: string,
  lang: string,
  strip: boolean,
  collapse: boolean,
): string {
  const normalized = normalizeLineEndings(code)
  const noComments = strip ? stripComments(normalized, lang) : normalized
  return collapse ? collapseWhitespace(noComments, lang) : noComments
}

// ---- 归一坐标 → 原文坐标映射（v0.83「原文显示」） ----
// merge 的 diffConfig.override 只能替换 diff 算法、不能替换参与比对的文本，而「原文显示 +
// 归一口径比对」需要：两侧原文喂给 merge 展示，override 内先归一再 diff，最后把归一坐标
// 映射回原文坐标。因 stripComments/collapseWhitespace 均保行数与换行位置不变（行 1:1），
// 映射只需行号中转：归一 offset → 行号 → 原文行边界（吸到行首/行尾，merge 以行为单位染色）。

// 每行行首 offset + 末尾哨兵（值 = doc.length；末尾恰为换行时与最后一空行行首重合，
// 二分取「最后一个 ≤ pos」仍落哨兵位，lineOf 正确返回 lines.length）
function lineStarts(text: string): number[] {
  const starts = [0]
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '\n') starts.push(i + 1)
  }
  starts.push(text.length)
  return starts
}

// offset 所在行号（末尾哨兵兜底：pos == doc.length 时返回最后一行 +1，插入尾部的空段）
function lineOf(starts: number[], pos: number): number {
  let lo = 0
  let hi = starts.length - 1
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (starts[mid] <= pos) lo = mid
    else hi = mid - 1
  }
  return lo
}

// 把归一文本上的 Change（字符坐标）映射到原文行边界坐标；四份文本行数两两相同
// changes 输入/输出都是纯对象（不 import @codemirror/merge，保持本模块零合并依赖，
// DiffView 动态 import merge 后自行 new Change 包装）
export interface RawChange {
  fromA: number
  toA: number
  fromB: number
  toB: number
}

export function mapNormalizedChanges(
  origA: string,
  origB: string,
  normA: string,
  normB: string,
  changes: readonly RawChange[],
): RawChange[] {
  const saA = lineStarts(origA)
  const saB = lineStarts(origB)
  const snA = lineStarts(normA)
  const snB = lineStarts(normB)
  return changes.map((c) => ({
    fromA: saA[lineOf(snA, c.fromA)],
    toA: saA[lineOf(snA, c.toA)],
    fromB: saB[lineOf(snB, c.fromB)],
    toB: saB[lineOf(snB, c.toB)],
  }))
}

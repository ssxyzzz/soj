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

// /diff 页统一入口：恒做行尾归一；strip 为 true 时再剥注释
export function prepareForDiff(code: string, lang: string, strip: boolean): string {
  const normalized = normalizeLineEndings(code)
  return strip ? stripComments(normalized, lang) : normalized
}

// 题库存量题面样例 → ```inputN/```outputN 围栏一次性迁移（mongosh 脚本，样例双列小批配套）
// 背景：渲染层样例双列（web/src/utils/md.ts pairIOSamples）只认紧邻同号的
// inputN/outputN 围栏；本脚本把库内旧写法（「输入：」+裸围栏、### 输入#1+```none、
// **输入 #1**复制 等）批量转换为规范围栏，并删除样例标签行。
//
// 用法（在 mongo 容器内对 soj 库执行）：
//   docker exec -i soj-mongo-1 mongosh soj --quiet --file /dev/stdin < scripts/migrate-io-samples.js
// 默认 dry-run（只打印变更摘要，不写库）；审阅无误后把下方 COMMIT 改为 true 再跑一遍。
//
// 规则：
//   - 删除样例标签行，紧随的可改写围栏（裸/none/plain/c++/cpp/text）改写为 ```inputN；
//     输出侧同号 ```outputN。编号：标签显式编号（#1/数据 1）优先，否则自增；输出缺编号
//     沿用最近输入的编号。
//   - 已成形围栏（```inputN/```outputN）不改，但其前面的残留标签行仍删除。
//   - 22 类形态：输入侧无围栏（"本题没有输入。"文字），插空 ```input1 与 output1 配对
//     （语义同 seed 2_hello）。
//   - 31 类形态：样例解释段落移到各自配对块之后（否则解释隔断第 2/3 组配对）。
//     ※ 专属转换（SPECIAL 表）不幂等：对已转换文本重跑会抛错，不影响库内数据。
//   - 只改 problems.statement；围栏内内容一行不动。
//   - 兜底清扫：洛谷复制按钮残渣行「[Copy]()」删除（隔断配对）。
const COMMIT = false

// 段内/标题/粗体三类样例标签行（整行精确匹配，防误伤正文句子）
function parseLabel(t) {
  let m = t.match(/^#{2,6}\s*(?:\*\*)?\s*(输入|输出)(?:样例|数据)?\s*[#＃]?\s*(\d+)?\s*(?:\*\*)?\s*(?:复制)?[:：]?\s*$/)
  if (!m) m = t.match(/^\*\*\s*(输入|输出)\s*(?:样例|数据)?\s*[#＃]?\s*(\d+)?\s*\*\*\s*(?:复制)?[:：]?\s*$/)
  if (!m) m = t.match(/^(?:\*\*)?\s*(输入|输出)(?:样例)?\s*[:：]\s*(?:\*\*)?$/)
  if (!m) return null
  return { dir: m[1] === '输入' ? 'in' : 'out', num: m[2] ? parseInt(m[2], 10) : null }
}

const REWRITABLE = new Set(['', 'none', 'plain', 'c++', 'cpp', 'text'])
const FENCE_OPEN = /^(`{3,})\s*([^`]*)$/
const FENCE_CLOSE = /^`{3,}\s*$/
const IS_IO_FENCE = /^`{3,}\s*(?:input|output)\d+/

// 通用状态机转换；返回 { text, changes[] }（changes 为空表示无需改动）
function convertGeneric(text) {
  const lines = text.split('\n')
  const out = []
  const changes = []
  let i = 0
  let autoN = 0
  let lastN = 0
  let inFence = false
  let pending = null // { dir, num, blanks: [] }
  while (i < lines.length) {
    const raw = lines[i]
    const t = raw.trim()
    if (inFence) {
      out.push(raw)
      if (FENCE_CLOSE.test(t)) inFence = false
      i++
      continue
    }
    const open = t.match(FENCE_OPEN)
    if (pending && open) {
      const lang = open[2].trim()
      const io = IS_IO_FENCE.test(t)
      if (io || REWRITABLE.has(lang)) {
        const n = pending.num ?? (pending.dir === 'in' ? ++autoN : lastN)
        if (pending.dir === 'in') lastN = n
        const newFence = '```' + (pending.dir === 'in' ? 'input' : 'output') + n
        out.push(newFence)
        changes.push('~ @' + i + ' ' + t + ' → ' + newFence)
        // 标签行已删，标签与围栏间的空行随之丢弃（补回会把空行注进围栏首行）
        pending = null
        inFence = true
        i++
        continue
      }
      // 不可改写的围栏（如函数签名 ```cpp）：标签作废，缓存行原样补回
      pending.blanks.forEach((b) => out.push(lines[b]))
      out.push(raw)
      changes.push('× @' + i + ' 标签作废（后随不可改写围栏 ' + t + '）')
      pending = null
      inFence = true
      i++
      continue
    }
    if (pending && t === '') {
      pending.blanks.push(i)
      i++
      continue
    }
    if (pending) {
      // pending 中遇到其他非空行：作废（原样补回），当前行走正常流程
      pending.blanks.forEach((b) => out.push(lines[b]))
      pending = null
      // fallthrough 处理当前行
    }
    const lab = parseLabel(t)
    if (lab) {
      pending = { dir: lab.dir, num: lab.num, blanks: [] }
      changes.push('- @' + i + ' ' + t)
      i++
      continue
    }
    out.push(raw)
    i++
  }
  if (pending) {
    // 尾部悬挂标签（不应出现）：原样补回
    pending.blanks.forEach((b) => out.push(lines[b]))
    out.push(lines[lines.length - 1])
  }
  return { text: out.join('\n'), changes }
}

// 22 题：输入侧无围栏 → 删两标题，插空 ```input1，"本题没有输入。"并入空围栏语义（同 seed 2_hello）
function convertP22(text) {
  const from = '### 输入#1\n\n本题没有输入。\n\n### 输出#1\n\n```output1'
  if (!text.includes(from)) return text // 幂等：锚点不存在（已转换/非此形态）原样返回
  return text.replace(from, '```input1\n\n```\n\n```output1')
}
// 31 题：样例区整体重排（删输入/输出标题、c++→inputN/outputN、解释移到配对块后）
function convertP31(text) {
  if (!text.includes('## 数据样例') || !/### (?:输入样例|样例输入)#1/.test(text)) return text // 幂等守卫
  const head = text.slice(0, text.indexOf('## 数据样例'))
  const tail = text.slice(text.indexOf('## 数据范围'))
  const exp = (n) => text.match(new RegExp('### 样例解释#' + n + '\\n\\n([\\s\\S]*?)(?=\\n\\n|\\n## )'))[1]
  return (
    head +
    '## 数据样例\n\n' +
    [1, 2, 3]
      .map((n) => {
        const inb = text.match(new RegExp('### (?:输入样例|样例输入)#' + n + '\\n\\n```c\\+\\+\\n([\\s\\S]*?)\\n```'))
        const outb = text.match(new RegExp('### (?:输出样例|样例输出)#' + n + '\\n\\n```c\\+\\+\\n([\\s\\S]*?)\\n```'))
        return '```input' + n + '\n' + inb[1] + '\n```\n\n```output' + n + '\n' + outb[1] + '\n```\n\n### 样例解释#' + n + '\n\n' + exp(n)
      })
      .join('\n\n') +
    '\n\n' +
    tail
  )
}

const SPECIAL = { 22: convertP22, 31: convertP31 }

let total = 0
db.problems
  .find({}, { _id: 1, title: 1, statement: 1 })
  .sort({ _id: 1 })
  .forEach((p) => {
    const original = p.statement || ''
    const notes = []
    // 兜底：洛谷复制按钮残渣（空链接行，隔断 inputN/outputN 配对）
    let before = original.replace(/\n\[Copy\]\(\)\n/g, '\n')
    if (before !== original) notes.push('清 [Copy]() 残渣')
    const fn = SPECIAL[p._id.toNumber ? p._id.toNumber() : Number(p._id)]
    const r = fn ? { text: fn(before), changes: ['（专属转换）'] } : convertGeneric(before)
    if (r.text === original) return
    total++
    print('[' + p._id + '] ' + p.title + ' — ' + (r.changes.length + notes.length) + ' 处变更')
    notes.forEach((c) => print('    ' + c))
    r.changes.forEach((c) => print('    ' + c))
    if (COMMIT) db.problems.updateOne({ _id: p._id }, { $set: { statement: r.text } })
  })
print(COMMIT ? '已提交 ' + total + ' 题' : 'DRY-RUN：共 ' + total + ' 题将变更（未写库）')

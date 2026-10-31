// 题面数据修复脚本（mongosh）——题包生成器转换缺陷批量修复（2026-10-31）
//
// 病灶与修法（三遍清扫：A = MathML 还原+残影剥离，B = 黏连围栏拆分，C = 干净 TeX 后残影清扫）：
//   A. MathML：$TeX$ 被生成器转成 $<math…><annotation encoding="application/x-tex">TeX</annotation>…</math>$
//      + KaTeX 渲染残影。修复：还原为 $TeX$（TeX 完整存于 annotation，无损）；display="block" 提升为 $$…$$；
//      残缺 annotation（无 </annotation>）时从 mrow 结构重建 TeX（仅 mi/mn/mo/mtext/msub/msup 一层嵌套）。
//   B. 黏连围栏：生成器吃掉闭合围栏后的换行，"```\n## 标题"黏成 "```## 标题"，围栏吞到下一个裸围栏/文件尾。
//      修复：```#{1,6} 标题 行拆回「```」+ 空行 + 标题。
//   残影剥离两步（作用于还原点/每个 $TeX$ 之后）：
//     A2 归一化等值：闭合定界符后到行尾，去空白/零宽、丢 ^ _、−→-、∗/×→*、÷→/ 后与 TeX 全等 → 整段剥
//        （TeX 含中文时残影必含 CJK，字符集法剥不动；句尾标点保留不剥）；
//     A1 残影字符集：\w + 零宽段 + 数学符号区段 + ASCII 数学标点（不含空格/CJK/全角/反引号/反斜杠/$）。
//   C. 干净 TeX 后跟残影（A 的姊妹形态 / 部分剥离残余）：$TeX$残影 ——— 同 A2/A1 剥离。
//      仅在**围栏外**的行上做（样例数据里可能有 $ 字符）；TeX 含全角标点视为散文误配、该处不剥；
//      单个运算符残影且紧跟另一个 $ 时跳过（防 "$a$+$b$" 误剥）。
//
// 用法（本地验证通过才上线；两库同脚本）：
//   dry-run: docker exec -i soj-mongo-1 mongosh --quiet soj < scripts/repair-statement-md.js
//   写入:    echo "globalThis.APPLY = true" | cat - scripts/repair-statement-md.js | docker exec -i soj-mongo-1 mongosh --quiet soj
// 写入动作：$set statement=修复后 md + updatedAt；$unset statementHtml（前端不消费，回退客户端渲染，
//           下次服务启动 MigrateRenderedHtml 按「缺失补齐」自动重刷）。
// 不变量断言（不满足则该题跳过写入并报出）：反引号数不变；`$` 数差 = 2×display-block 提升数。

const APPLY = globalThis.APPLY === true;

// ---------- 病灶 A：MathML → TeX ----------
// 统一匹配所有 $ 包裹的 math 元素（含残缺 annotation）；内部 tempered dot 禁止跨元素（块公式缺
// 闭合定界符时，懒匹配会一路吞到下一个公式才凑齐 \1，曾靠不变量断言拦下 6 题）
const MATH_RE = /(\${1,2})<math xmlns="http:\/\/www\.w3\.org\/1998\/Math\/MathML"([^>]*)>((?:(?!<math)[\s\S])*?)<\/math>\1/g;
// 块级缺闭合形态：$$<math display=block>…</math>渲染残影$$——生成器把 TeX 本体换成
// 「MathML+渲染残影」，闭合 $$ 落在残影之后；残影可内嵌 <svg> 片段
const BLOCK_RE = /\$\$<math xmlns="http:\/\/www\.w3\.org\/1998\/Math\/MathML"([^>]*)>((?:(?!<math)[\s\S])*?)<\/math>((?:(?!<math|\$)[\s\S])*)\$\$/g;

// 迷你 MathML→TeX（残缺 annotation 兜底重建；语料内案例均为简单式子）
function ml2tex(s) {
  let out = "";
  for (const m of s.matchAll(/<(mi|mn|mo|mtext|msub|msup)([^>]*)>([\s\S]*?)<\/\1>/g)) {
    if (m[1] === "msub" || m[1] === "msup") {
      const kids = [...m[3].matchAll(/<(mi|mn|mo|mtext)([^>]*)>([\s\S]*?)<\/\1>/g)].map(k => k[3]);
      out += (kids[0] || "") + (m[1] === "msub" ? "_{" : "^{") + (kids[1] || "") + "}";
    } else {
      out += m[3];
    }
  }
  return out;
}

// A1 残影字符集：\w + 零宽/窄空格段 + 数学符号区段 + ASCII 数学标点
const JUNK_CHAR = "[\\w\\u0300-\\u036f\\u2000-\\u200f\\u2060-\\u2064\\ufeff\\u00a0\\u2026\\u2030-\\u205e\\u20d0-\\u20f0\\u2190-\\u21ff\\u2200-\\u22ff\\u2300-\\u23ff\\u27e8-\\u27ef\\u00d7\\u00f7\\u00b1\\u00b0\\u00b7\\u00b5=<>%()+*/,.;:!?'" + '"\\[\\]{}\\-]';
const JUNK_RUN_RE = new RegExp("^(?:" + JUNK_CHAR + ")*");

// A2 归一化：渲染层把 ^ _ 画成上下标、TeX 命令画成符号——两边对齐到「渲染纯文本」再比
function normTex(t) {
  return t
    .replace(/[\s\u200b-\u200f\u2060\ufeff\u00a0\uE000-\uF8FF]/g, "")
    .replace(/\\(left|right|big|Big|bigg|Bigg)/g, "")
    .replace(/\\(le|leq|leqslant)/g, "\u2264")
    .replace(/\\(ge|geq|geqslant)/g, "\u2265")
    .replace(/\\ne(q)?/g, "\u2260")
    .replace(/\\times/g, "*")
    .replace(/\\cdot/g, "\u22c5")
    .replace(/\\mid/g, "\u2223")
    .replace(/\\div/g, "/")
    .replace(/\\pm/g, "\u00b1")
    .replace(/\\(dots|ldots|cdots|vdots|ddots)/g, "\u2026")
    .replace(/\\[a-zA-Z]+/g, "")
    .replace(/[{}]/g, "")
    .replace(/[\^_]/g, "")
    .replace(/[\u2212\u2012\u2013]/g, "-")
    .replace(/[\u2217\u00d7]/g, "*")
    .replace(/\u00f7/g, "/");
}

// PUA 锚定残渣正则：渲染器把 ≠ 等字形落在私用区（U+E020 等），残留形态 = PUA + "=" + 渲染尾巴。
// 正文不会有 PUA，锚定即残渣；尾巴连同空格一起剥，止于 CJK/全角/$/换行
const PUA_JUNK_RE = /^[\uE000-\uF8FF]=?[ \t\u200b-\u200f\u2060\ufeff\u00a0\uE000-\uF8FF\w\u2030-\u205e\u20d0-\u20f0\u2190-\u21ff\u2200-\u22ff\u2300-\u23ff\u27e8-\u27ef\u00d7\u00f7\u00b1\u00b0\u00b7\u00b5\u2026=<>%()+*/,.;:!?'"{}\[\]\-−∗×÷]*/;

// 定界符后残影剥离：先 A2（归一化等值整段剥，句尾标点保留）后 A1。
// mode="A"：MathML 还原点——残影即渲染文本，全量字符集剥。
// mode="C"：干净 TeX 旁——只剥「norm(TeX) 后缀片段」形态的残影（如 0<n≤1000→<n≤1000、T=1→=1），
//           其余（正文标点、-th、后续公式）一律不碰。
function stripJunk(rest, tex, junkHist, stat, mode) {
  const lineRun = (rest.match(/^[^\n]{0,300}/) || [""])[0];
  const cand = lineRun.replace(/[，。；！？\s]+$/, "");
  if (cand && normTex(cand) === normTex(tex)) {
    junkHist["[A2] " + String(tex).slice(0, 36)] = (junkHist["[A2] " + String(tex).slice(0, 36)] || 0) + 1;
    stat.a2++;
    return cand;
  }
  // PUA 锚定残渣（A/C 两模式通用，且不受 C 类长度闸限制——见 C 走串 implausible 分支）
  const puaM = rest.match(PUA_JUNK_RE);
  if (puaM && puaM[0]) {
    junkHist["[PUA] " + puaM[0].replace(/[\uE000-\uF8FF]/g, "@").slice(0, 36)] =
      (junkHist["[PUA] " + puaM[0].replace(/[\uE000-\uF8FF]/g, "@").slice(0, 36)] || 0) + 1;
    stat.junk++;
    return puaM[0];
  }
  let junk = (rest.match(JUNK_RUN_RE) || [""])[0];
  if (mode === "A") {
    // 渲染残影可内嵌 <svg> 片段（块级公式/根线等），连同其后的字符集残影一并剥
    let r = rest.slice(junk.length);
    for (;;) {
      const svg = r.match(/^<\/?svg\b[^>]*>/);
      if (!svg) break;
      const j2 = (r.slice(svg[0].length).match(JUNK_RUN_RE) || [""])[0];
      junk += svg[0] + j2;
      r = r.slice(svg[0].length + j2.length);
    }
  }
  if (mode === "C" && junk) {
    const nj = normTex(junk), nt = normTex(tex);
    const zwOnly = /^[\u200b-\u200f\u2060\ufeff]+$/.test(junk);
    if (!zwOnly && (!nj || !nt.endsWith(nj))) junk = "";
  }
  // 防误剥：单个运算符残影且紧跟另一个 $（如 "$a$+$b$" 的 + 是正文）
  if (junk.length === 1 && "+-−*×÷=<>".includes(junk) && rest.slice(1, 2) === "$") {
    return "";
  }
  if (junk) {
    junkHist[(mode === "C" ? "[C] " : "[A1] ") + junk.slice(0, 36)] =
      (junkHist[(mode === "C" ? "[C] " : "[A1] ") + junk.slice(0, 36)] || 0) + 1;
    stat.junk++;
  }
  return junk;
}

// ---------- 主修复 ----------
// stat: {mathml, malformed, promoted, blockOpen, fence, junk, a2}
function texFromInner(inner, stat) {
  if (/<\/annotation>/.test(inner)) {
    stat.mathml++;
    return (inner.match(/<annotation encoding="application\/x-tex">([\s\S]*?)<\/annotation>/) || ["", ""])[1];
  }
  stat.malformed++;
  return ml2tex(inner);
}

function repairStatement(s, junkHist) {
  const stat = { mathml: 0, malformed: 0, promoted: 0, blockOpen: 0, fence: 0, junk: 0, a2: 0 };

  // B：黏连围栏拆分
  s = s.replace(/^(`{3,})[ \t]*(#{1,6}[^\n]*)$/gm, (_w, ticks, head) => {
    stat.fence++;
    return ticks + "\n\n" + head;
  });

  // A：MathML → $TeX$（还原公式用 \u0001 包裹做锚点）；再兜底块级缺闭合形态（连残影带闭合 $$ 一起收）
  s = s.replace(MATH_RE, (_w, delim, attrs, inner) => {
    const block = /display="block"/.test(attrs || "");
    if (block && delim === "$") stat.promoted++;
    const d = block ? "$$" : delim;
    return "\u0001" + d + texFromInner(inner, stat) + d + "\u0001";
  });
  s = s.replace(BLOCK_RE, (_w, attrs, inner, dup) => {
    stat.blockOpen++;
    const tex = texFromInner(inner, stat);
    const junk = stripJunk(dup, tex, junkHist, stat, "A");
    return "\u0001$$" + tex + "$$\u0001" + dup.slice(junk.length);
  });
  const parts = s.split("\u0001");

  // A 残影剥离：奇数段 = 还原公式，其后偶数段开头即残影
  for (let i = 1; i < parts.length; i += 2) {
    const tex = parts[i].slice(1, -1);
    const rest = parts[i + 1] || "";
    const junk = stripJunk(rest, tex, junkHist, stat, "A");
    parts[i + 1] = rest.slice(junk.length);
  }
  s = parts.join("");

  // C：围栏外行上的 $TeX$ 后残影清扫（顺序走串：从上个闭合 $ 后继续找下一对，避开配对假象）
  const lines = s.split("\n");
  let inFence = false;
  for (let li = 0; li < lines.length; li++) {
    if (/^\s*`{3,}/.test(lines[li])) { inFence = !inFence; continue; }
    if (inFence) continue;
    const line = lines[li];
    let out = "", last = 0;
    for (const m of line.matchAll(/\$([^$\n]{0,80}?)\$/g)) {
      const tex = m[1];
      const plausible = tex.length <= 60 && !/[，。；：？！、（）“”‘’《》]/.test(tex);
      out += line.slice(last, m.index) + m[0];
      if (plausible) {
        const rest = line.slice(m.index + m[0].length);
        const junk = stripJunk(rest, tex, junkHist, stat, "C");
        last = m.index + m[0].length + junk.length;
      } else {
        // tex 不可信（过长/含全角=疑似散文误配）只放行 PUA 锚定——PUA 即残渣铁证
        const rest = line.slice(m.index + m[0].length);
        const pm = rest.match(PUA_JUNK_RE);
        if (pm && pm[0]) {
          junkHist["[PUA] " + pm[0].replace(/[\uE000-\uF8FF]/g, "@").slice(0, 36)] =
            (junkHist["[PUA] " + pm[0].replace(/[\uE000-\uF8FF]/g, "@").slice(0, 36)] || 0) + 1;
          stat.junk++;
          last = m.index + m[0].length + pm[0].length;
        } else {
          last = m.index + m[0].length;
        }
      }
    }
    out += line.slice(last);
    lines[li] = out;
  }

  // D：块级闭合后残渣扫尾——残影含组合字符序列（如 ≠ = U+003D+U+0338）时字符集剥离提前停，
  // 闭块 $$ 后留下 "=0" 类短渣滓（渣滓可含不可见组合符）。只认纯标点/数字/数学符号，杜绝误伤正文。
  // 替换串用函数：字符串形式的 "$$" 会被 replace 当「字面 $」转义。
  return {
    fixed: lines.join("\n").replace(
      /\$\$([=\d.,;:!?\-\u0300-\u036f\u20d0-\u20f0−∗*×÷%+]{1,12})\n\n/g,
      () => "$$\n\n"
    ),
    stat,
  };
}

// ---------- 主流程 ----------
const junkHist = {};
let scanned = 0, changed = 0, skipped = 0;
let mathmlTotal = 0, malTotal = 0, promotedTotal = 0, fenceTotal = 0, junkTotal = 0, a2Total = 0;
const badSamples = [];

db.problems.find(
  { $or: [
    { statement: /<math/ },
    { statement: /`{3,}#{1,6}/ },
    // C 类候选（JS 侧 repairStatement 再精确判断；查询侧不能用 \u 转义，PCRE2 不支持）
    { statement: /\$[^$\n]{1,80}\$/ },
  ] },
  { title: 1, statement: 1 }
).sort({ _id: 1 }).forEach(p => {
  scanned++;
  const s = p.statement;
  const { fixed, stat } = repairStatement(s, junkHist);
  if (fixed === s) return;
  changed++;
  mathmlTotal += stat.mathml;
  malTotal += stat.malformed;
  promotedTotal += stat.promoted;
  fenceTotal += stat.fence;
  junkTotal += stat.junk;
  a2Total += stat.a2;

  // 不变量断言
  const ticks = t => (t.match(/`/g) || []).length;
  const dol = t => (t.match(/\$/g) || []).length;
  const fail = [];
  if (ticks(fixed) !== ticks(s)) fail.push("反引号数变化");
  if (dol(fixed) - dol(s) !== 2 * stat.promoted) fail.push("$数差与block提升数不符");
  if (fail.length) {
    skipped++;
    badSamples.push(p._id + " " + p.title + " :: " + fail.join("; "));
    return;
  }
  if (APPLY) {
    db.problems.updateOne({ _id: p._id }, {
      $set: { statement: fixed, updatedAt: new Date() },
      $unset: { statementHtml: "" },
    });
  }
});

// ---------- 报告 ----------
print("mode: " + (APPLY ? "APPLY(已写入)" : "DRY-RUN(未写入)"));
print("scanned=" + scanned + " changed=" + changed + " skipped=" + skipped);
print("mathml=" + mathmlTotal + " malformed=" + malTotal + " promoted=" + promotedTotal +
  " fence=" + fenceTotal + " junkA1=" + junkTotal + " junkA2=" + a2Total);
const junkKeys = Object.keys(junkHist);
const junkSum = junkKeys.reduce((a, k) => a + junkHist[k], 0);
print("剥离残影共 " + junkSum + " 处、去重 " + junkKeys.length + " 种");
junkKeys.sort((a, b) => junkHist[b] - junkHist[a]).slice(0, 400).forEach(k =>
  print("  junk x" + junkHist[k] + " " + JSON.stringify(k)));
if (badSamples.length) {
  print("!! 断言失败（未写入）" + badSamples.length + " 题：");
  badSamples.slice(0, 20).forEach(x => print("  " + x));
}
print("残余 MathML 题数: " + db.problems.countDocuments({ statement: /Math\/MathML/ }));
print("残余黏连围栏题数: " + db.problems.countDocuments({ statement: /`{3,}#{1,6}/ }));
[2551, 2304, 1307].forEach(id => {
  const p = db.problems.findOne({ _id: id }, { statement: 1 });
  if (p) print("### 修复后 P" + id + " 全文 ###\n" + p.statement);
});

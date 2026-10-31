// 题面【】小节标记归一为 markdown 标题（mongosh，2026-10-31 用户指令）
//
// 规则：
//   - 去行首空白后以【X】开头的行 → "## X"；同行剩余内容另起一行（【数据范围】3≤n≤1000 →
//     "## 数据范围" + 换行 + "3≤n≤1000"）。
//   - 代码围栏内（``` 切换）的行不处理——样例数据可能含【】。
//   - 行中部出现的【】（真正的行内嵌段）不动。
// 幂等：转换后的 "## X" 行不再匹配。写入动作同 repair-statement-md.js：
//   $set statement + updatedAt，$unset statementHtml（启动迁移缺失补齐自动重刷）。
//
// 用法：
//   dry-run: docker exec -i soj-mongo-1 mongosh --quiet soj < scripts/repair-statement-brackets.js
//   写入:    echo "globalThis.APPLY = true" | cat - scripts/repair-statement-brackets.js | docker exec -i soj-mongo-1 mongosh --quiet soj
// 断言：修复前后 CJK 字符数不变（【】不在 CJK 区，转换只动标记与换行）。

const APPLY = globalThis.APPLY === true;

function repairBrackets(s, stat) {
  const lines = s.split("\n");
  let inFence = false;
  for (let li = 0; li < lines.length; li++) {
    if (/^\s*`{3,}/.test(lines[li])) { inFence = !inFence; continue; }
    if (inFence) continue;
    const m = lines[li].match(/^\s*【([^【】]{1,40})】\s*(.*)$/);
    if (!m) continue;
    const label = m[1].trim();
    const rest = m[2].replace(/\s+$/, "");
    if (!label) continue;
    stat.n++;
    const key = label.replace(/\d+/g, "#");
    stat.labels[key] = (stat.labels[key] || 0) + 1;
    lines[li] = "## " + label + (rest ? "\n" + rest : "");
  }
  return lines.join("\n");
}

// ---------- 主流程 ----------
let scanned = 0, changed = 0, skipped = 0, nTotal = 0;
const labels = {};
const badSamples = [];
const previews = [];

db.problems.find({ statement: /【/ }, { title: 1, statement: 1 }).sort({ _id: 1 }).forEach(p => {
  scanned++;
  const s = p.statement;
  const stat = { n: 0, labels: {} };
  const fixed = repairBrackets(s, stat);
  if (fixed === s) return;
  changed++;
  nTotal += stat.n;
  Object.entries(stat.labels).forEach(([k, v]) => { labels[k] = (labels[k] || 0) + v; });

  // 断言：CJK 字符数不变
  const cjk = t => (t.match(/[\u4e00-\u9fff]/g) || []).length;
  if (cjk(fixed) !== cjk(s)) {
    skipped++;
    badSamples.push(p._id + " " + p.title + " :: CJK数变化 " + cjk(s) + "->" + cjk(fixed));
    return;
  }
  if (previews.length < 3) {
    const before = s.split("\n").filter(l => l.includes("【")).slice(0, 3);
    const after = fixed.split("\n").filter(l => /^## /.test(l)).slice(0, 4);
    previews.push(p._id + " " + p.title + "\n  BEFORE " + JSON.stringify(before) + "\n  AFTER  " + JSON.stringify(after));
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
print("scanned=" + scanned + " changed=" + changed + " skipped=" + skipped + " 标记数=" + nTotal);
print("标签谱系：");
Object.entries(labels).sort((a, b) => b[1] - a[1]).forEach(([k, v]) => print("  " + v + "  ## " + k));
if (badSamples.length) {
  print("!! 断言失败（未写入）" + badSamples.length + " 题：");
  badSamples.slice(0, 10).forEach(x => print("  " + x));
}
previews.forEach(x => print(x));
print("残余行首【 标记题数: " + db.problems.countDocuments({ statement: /(^|\n)\s*【[^【】]{1,40}】/ }) + "");

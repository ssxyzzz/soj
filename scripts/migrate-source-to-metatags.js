// 存量来源迁移（2026-10-25 双树定稿）：problems 的来源四字段 + rawTags
//   → meta_tags（来源标签树节点 ID）+ 移除旧字段。
// 规则（用户拍板）：能对上词条的保留（词条 + 年份），对不上的直接删；
// 组别/学段修饰（普及组/小学）丢弃。
// 用法：mongosh "<uri>/<库名>" scripts/migrate-source-to-metatags.js [--apply]
//   默认 dry-run 只打印；加 --apply 才写库。管道执行（--file /dev/stdin）时可用
//   --eval='var APPLY_FLAG = true' 预置写库开关（脚本自身在 stdin 上拿不到 argv）。
// 前置：来源标签树种子已注入（后端启动一次即可，tags._id="tree:meta"）。

const APPLY =
  (typeof APPLY_FLAG !== 'undefined' && APPLY_FLAG === true) ||
  (typeof process !== 'undefined' && process.argv.includes('--apply'));

const treeDoc = db.tags.findOne({ _id: 'tree:meta' });
if (!treeDoc || !treeDoc.nodes || !treeDoc.nodes.length) {
  print('❌ 来源标签树为空：请先启动一次后端（注入种子 tree:meta）再迁移');
  quit(1);
}
const byName = {};
(function walk(list) {
  list.forEach((n) => {
    if (!(n.name in byName)) byName[n.name] = n.id;
    walk(n.children || []);
  });
})(treeDoc.nodes);

let scanned = 0;
let changed = 0;
db.problems
  .find({
    $or: [
      { source: { $exists: true } },
      { source_type: { $exists: true } },
      { year: { $exists: true } },
      { region: { $exists: true } },
      { rawTags: { $exists: true } },
    ],
  })
  .forEach((p) => {
    scanned += 1;
    const meta = [];
    if (p.year && byName[String(p.year)]) meta.push(byName[String(p.year)]);
    if (p.region && byName[p.region.trim()]) meta.push(byName[p.region.trim()]);
    if (p.source) {
      const s = p.source.trim();
      if (byName[s]) {
        meta.push(byName[s]);
      } else {
        // 家族前缀：「NOIP 普及组」→ NOIP；「洛谷P1001」→ 洛谷（词条名 ≥2 字防误命中）
        for (const name in byName) {
          if (name.length >= 2 && s.startsWith(name)) {
            meta.push(byName[name]);
            break;
          }
        }
      }
    }
    const uniq = [...new Set(meta)];
    print(
      `${p._id} ${p.title} [source=${p.source || '-'} region=${p.region || '-'} year=${p.year || '-'} rawTags=${(p.rawTags || []).join(',') || '-'}] → meta_tags=[${uniq}]`,
    );
    if (APPLY) {
      db.problems.updateOne(
        { _id: p._id },
        {
          $set: { meta_tags: uniq },
          $unset: { source: '', source_type: '', year: '', region: '', rawTags: '' },
        },
      );
    }
    changed += 1;
  });

print(APPLY ? `✅ 已迁移 ${changed}/${scanned} 题` : ` dry-run：扫描 ${scanned} 题，加 --apply 写库`);

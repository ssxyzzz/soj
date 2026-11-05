// 题单编辑草稿 → 提交形态（v0.71 抽公共）：保存（整体 PATCH）与 AI 一键整理 run
// 同一口径——标题/描述去首尾空白，children 优先（单章不混挂），题目只留 id 数组。
import type { ListEntry, ListEntryDraft } from '@/types'

export function draftToChapters(chs: ListEntryDraft[]): ListEntry[] {
  return chs.map((c) => {
    const base = { title: c.title.trim(), desc: c.desc.trim() }
    if (c.children.length) {
      return {
        ...base,
        children: c.children.map((s) => ({
          title: s.title.trim(),
          desc: s.desc.trim(),
          problems: s.problems.filter((p) => p.id != null).map((p) => p.id),
        })),
      }
    }
    return { ...base, problems: c.problems.filter((p) => p.id != null).map((p) => p.id) }
  })
}

import type { RankItem } from '@/types'

/**
 * 排行榜排序链（v0.100 四榜合一；纯逻辑，esbuild 打包直跑断言）。
 *
 * 点表头切换排序列，严格序无并列：当前列降序 → 等级降序 → 经验降序 → 解题数降序 →
 * uid 升序。连击/坚持四列只列大于 0 的人，等级、解题数两列列出全部有 AC 的人。
 * 名次 = 排序后的位置（从 1 起），同一个人在不同列下的名次可以不同。
 * 这里的函数都不改传入的整表，返回的一律是新数组。
 */

/** 可排序列（表格列顺序） */
export const RANK_SORT_KEYS = [
  'level',
  'solved',
  'combo_cur',
  'combo_best',
  'streak_days',
  'streak_best',
] as const

export type RankSortKey = (typeof RANK_SORT_KEYS)[number]

/** 默认按等级 */
export const DEFAULT_RANK_SORT: RankSortKey = 'level'

/** 列名（表头、领奖台、未上榜提示共用） */
export const RANK_SORT_LABELS: Record<RankSortKey, string> = {
  level: '等级',
  solved: '解题数',
  combo_cur: '当前连击',
  combo_best: '历史连击',
  streak_days: '坚持天数',
  streak_best: '历史坚持',
}

/** 只列大于 0 的列（连击/坚持） */
const POSITIVE_ONLY: ReadonlySet<RankSortKey> = new Set<RankSortKey>([
  'combo_cur',
  'combo_best',
  'streak_days',
  'streak_best',
])

export function isRankSortKey(v: unknown): v is RankSortKey {
  return typeof v === 'string' && (RANK_SORT_KEYS as readonly string[]).includes(v)
}

/** 该行在当前排序列下是否入榜：连击/坚持列要求本列值大于 0 */
export function rankOnView(key: RankSortKey, row: RankItem): boolean {
  return !POSITIVE_ONLY.has(key) || (row[key] ?? 0) > 0
}

/** 数值列（排序链取值面） */
type NumKey = 'level' | 'xp' | 'solved' | 'combo_cur' | 'combo_best' | 'streak_days' | 'streak_best'

function num(row: RankItem, k: NumKey): number {
  return row[k] ?? 0
}

/** 排序链：当前列 → 等级 → 经验 → 解题数（均降序）→ uid 升序 */
export function compareRankItems(key: RankSortKey, a: RankItem, b: RankItem): number {
  const keys: NumKey[] = key === 'level' ? ['level', 'xp', 'solved'] : [key, 'level', 'xp', 'solved']
  for (const k of keys) {
    const d = num(b, k) - num(a, k)
    if (d !== 0) return d
  }
  return a.user_id - b.user_id
}

/** 当前视图：过滤 → 排序 → 编名次（rank = 位置，从 1 起） */
export function buildRankView(items: RankItem[], key: RankSortKey): RankItem[] {
  return items
    .filter((row) => rankOnView(key, row))
    .sort((a, b) => compareRankItems(key, a, b))
    .map((row, i) => ({ ...row, rank: i + 1 }))
}

/**
 * 当前页的表格行。「我」不在本页时置顶一行：名次落在别的页带名次；被「只列大于 0」
 * 滤掉则不带名次（表格显示「—」）。在整表里都找不到（没有 AC）时不置顶。
 */
export function rankPageRows(
  view: RankItem[],
  all: RankItem[],
  uid: number,
  page: number,
  pageSize: number,
): RankItem[] {
  const start = (page - 1) * pageSize
  const rows = view.slice(start, start + pageSize)
  if (!uid || rows.some((row) => row.user_id === uid)) return rows
  const mine = view.find((row) => row.user_id === uid)
  if (mine) return [mine, ...rows]
  const off = all.find((row) => row.user_id === uid)
  return off ? [{ ...off, rank: undefined }, ...rows] : rows
}

/** 空榜文案：连击/坚持列没人大于 0 与「还没有任何人通过」分开说 */
export function rankEmptyText(key: RankSortKey): string {
  switch (key) {
    case 'combo_cur':
    case 'combo_best':
      return '还没有连击记录——连续 AC 不失误就能上榜'
    case 'streak_days':
      return '还没有人正在坚持——今天 AC 一题就开始积累'
    case 'streak_best':
      return '还没有坚持记录——每天至少 AC 一题开始积累'
    default:
      return '还没有人通过题目，快去抢占第一！'
  }
}

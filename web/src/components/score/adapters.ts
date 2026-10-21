import type { AssignmentDetail, AssignmentProblem, ContestDetail, LeaderRow, ProgressRow } from '@/types'

import type { ScoreBoardView, ScoreCellView, ScoreProblemView, ScoreRowView } from './types'
import { progressPct, scoreState } from './types'

// 比赛/考试 adapter：rank/total 用服务端，进度 = 满分题数/总题数。
export function buildRoundBoard(
  kind: 'contest' | 'exam',
  round: ContestDetail,
  leaders: LeaderRow[],
  canInspect: boolean,
): ScoreBoardView {
  const problems: ScoreProblemView[] = (round.problems ?? []).map((cp, i) => ({
    pid: cp.id,
    label: `T${i + 1}`,
    title: cp.title,
    fullScore: cp.score > 0 ? cp.score : 100,
    route: `/${kind}s/${round.id}/problems/${cp.id}`,
  }))
  const rows: ScoreRowView[] = leaders.map((leader) => {
    const byPid = new Map(leader.problems.map((c) => [c.pid, c]))
    const cells: ScoreCellView[] = problems.map((p) => {
      const c = byPid.get(p.pid)
      const score = c?.score ?? 0
      const tries = c?.tries ?? 0
      return {
        pid: p.pid,
        score,
        errors: c?.errors ?? 0,
        fullScore: p.fullScore,
        tries,
        state: scoreState(score, p.fullScore, tries),
      }
    })
    const solved = cells.filter((c) => c.state === 'full').length
    return {
      uid: leader.uid,
      rank: leader.rank,
      name: leader.nickname || leader.username,
      level: leader.level,
      total: leader.total,
      solved,
      totalProblems: problems.length,
      progress: progressPct(solved, problems.length),
      cells,
    }
  })
  return { problems, rows, canInspect, showRank: true }
}

// 作业 adapter：total 用服务端（Σbest），rank 按 4A；进度 = solved/总题数。
export function buildAssignmentBoard(
  assignment: AssignmentDetail,
  progress: ProgressRow[],
  canInspect: boolean,
  showRank: boolean,
): ScoreBoardView {
  const problems: ScoreProblemView[] = assignment.problems.map((ap, i) => ({
    pid: ap.id,
    label: `T${i + 1}`,
    title: ap.title,
    fullScore: 100,
    route: assignmentProblemRoute(ap),
  }))
  const rows: ScoreRowView[] = progress.map((row) => {
    const byPid = new Map(row.problems.map((c) => [c.pid, c]))
    const cells: ScoreCellView[] = problems.map((p) => {
      const c = byPid.get(p.pid)
      const score = c?.best ?? 0
      const tries = c?.tries ?? 0
      return {
        pid: p.pid,
        score,
        errors: c?.errors ?? 0,
        fullScore: p.fullScore,
        tries,
        state: scoreState(score, p.fullScore, tries),
      }
    })
    return {
      uid: row.uid,
      rank: showRank ? row.rank : null,
      name: row.nickname || row.username,
      level: row.level,
      total: row.total,
      solved: row.solved,
      totalProblems: problems.length,
      progress: progressPct(row.solved, problems.length),
      cells,
    }
  })
  return { problems, rows, canInspect, showRank }
}

// 作业题跳转：比赛/考试来源直接进对应轮次的题目页；普通题进题库详情。
export function assignmentProblemRoute(ap: AssignmentProblem): string {
  if (ap.exam_id) return `/exams/${ap.exam_id}/problems/${ap.id}`
  if (ap.contest_id) return `/contests/${ap.contest_id}/problems/${ap.id}`
  return `/problems/${ap.id}`
}

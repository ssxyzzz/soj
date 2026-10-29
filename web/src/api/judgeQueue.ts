// 判题队列扩展 API（v0.7.1）：取消评测 / 按题批量重测 / 按赛批量重测。
// v0.29 收编主 client（api/index.ts）：原本因安全钩子误报拦写入而自建的裸 axios
// 拦截层退役——401 登出、failData 挂载、错误提示与主链路行为一致。
import { post } from './index'

export const judgeQueueApi = {
  /** 取消评测：Pending 直接终态；Judging 置 CancelRequested，评测在当前测试点结束后停止 */
  cancel: (id: number) => post<{ id: number; status: string }>(`/submissions/${id}/cancel`, {}),
  /** 按题批量重测（低优先级，不挤占学生新提交），返回重置条数 */
  rejudgeProblem: (pid: number) => post<{ count: number }>(`/problems/${pid}/rejudge-all`, {}),
  /** 按赛批量重测（低优先级），kind = contest | exam */
  rejudgeRound: (kind: 'contest' | 'exam', id: number) =>
    post<{ count: number }>(`/${kind}s/${id}/rejudge-all`, {}),
}

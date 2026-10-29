// 班级管理扩展 API（v0.7.2）：解散班级 / 转让班级 / 编辑班级 / 布置作业。
// v0.29 收编主 client（api/index.ts）：裸 axios 自建拦截层退役，401 登出等行为统一。
import { del, patch, post } from './index'

export async function dissolveClass(id: number): Promise<void> {
  await del(`/classes/${id}`)
}

/** 转让班级给指定成员（uid），对方成为新建班人、原建班人降为班级管理员 */
export async function transferClass(id: number, uid: number): Promise<void> {
  await post(`/classes/${id}/transfer`, { uid })
}

/** 编辑班级（v0.7.3）：名称 / 公告（Markdown）；v0.7.4 起可带 private（仅建班人 ∨ root，后端校验） */
export async function updateClass(
  id: number,
  body: { name?: string; intro?: string; private?: boolean },
): Promise<void> {
  await patch(`/classes/${id}`, body)
}

/** 布置作业（v0.7.3 创建即选题）：可选 problem_ids 一次带题 */
export async function createAssignment(
  classId: number,
  body: { title: string; intro?: string; problem_ids?: number[] },
): Promise<{ id: number; title: string }> {
  return post(`/classes/${classId}/assignments`, body)
}

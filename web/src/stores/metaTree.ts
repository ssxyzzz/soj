// 来源标签树（2026-10-25 双树）：地区/赛事/题库/年份/其他。与知识树（tagtree store）
// 同构同权限——题库筛选、详情来源区、建题表单来源打标共享同一份树与 ID→名字映射。
// v0.29 起复用 tagtree.ts 的 treeStoreOf 工厂（原两份逐行双胞胎退役）。
import { treeStoreOf } from './tagtree'

export const useMetaTreeStore = treeStoreOf('meta', 'metaTree')

// 年级档位（v0.34）：与后端 repo/grade.go 同源的一~十二年级 + 已毕业。
// API 层 grade 字段全程使用显示名字符串（空串 = 未设置），档位数值只在导入行里流转。

export const GRADE_NAMES = [
  '一年级',
  '二年级',
  '三年级',
  '四年级',
  '五年级',
  '六年级',
  '初一',
  '初二',
  '初三',
  '高一',
  '高二',
  '高三',
] as const

export const GRADE_GRADUATED = '已毕业'

/** 年级下拉选项（含「已毕业」；未设置 = 空串清空选择）。 */
export const GRADE_OPTIONS: string[] = [...GRADE_NAMES, GRADE_GRADUATED]

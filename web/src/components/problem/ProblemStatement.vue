<script setup lang="ts">
// 题面主卡（编辑器体验小批追加需求）：题目详情页主列与在线编程左栏共用一份渲染——
// #id 标题 + 时限/内存 + 可选文件 IO 提示 + 题面 Markdown，改一处两处生效。
// 标题行右侧扩展位走 #extra 插槽（详情页放「主题库隐藏」标签与隐藏开关）。
import MdView from '@/components/MdView.vue'
import { fmtMem } from '@/utils/format'
import type { ProblemDetail } from '@/types'

defineProps<{
  problem: ProblemDetail
  fileIo?: boolean // 比赛/考试启用文件 IO 时显示提示（做题页主列与在线编程左栏同款）
}>()
</script>

<template>
  <el-card>
    <div class="title-row">
      <span class="big-title">#{{ problem.id }} {{ problem.title }}</span>
      <slot name="extra" />
    </div>
    <div class="info-row">
      <el-tag type="success">时间限制 {{ problem.time_limit_ms }} ms</el-tag>
      <el-tag type="success">内存限制 {{ fmtMem(problem.memory_limit_kb) }}</el-tag>
    </div>
    <el-alert
      v-if="fileIo"
      type="warning"
      :closable="false"
      class="fileio-alert"
      show-icon
      title="本题请以文件 IO 方式提交"
      :description="`输入输出文件名是 ${problem.file_io_input || 'problem.in'} / ${problem.file_io_output || 'problem.out'}`"
    />
    <MdView :content="problem.description" />
  </el-card>
</template>

<style scoped>
.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.big-title {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.4;
}

.info-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 10px 0 14px;
}

.fileio-alert {
  margin-bottom: 16px;
}
</style>

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
    <!-- 内容列 860 居中：标题/时限信息/题面 Markdown 同列对齐——此前标题行铺满卡片、
       Markdown 单独 860 居中（卡片内容宽 ~916px 时左右各缩 28px），天然错位 -->
    <div class="statement">
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
    </div>
  </el-card>
</template>

<style scoped>
/* 内容列 860 居中（与 MdView/vditor 编辑端同宽：标题与正文同列，见模板注释）；
   在线编程左栏容器窄于 860 时此规则自然退化为全宽 */
.statement {
  max-width: 860px;
  margin: 0 auto;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 页头大标题标准档（22/600）：全站层级顶点，题面内 Markdown 标题一律低于它 */
.big-title {
  font-size: 22px;
  font-weight: 600;
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

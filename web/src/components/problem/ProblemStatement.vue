<script setup lang="ts">
// 题面主卡：详情页主列与在线编程左栏共用——#id 标题 + 时限/内存 + 可选文件 IO 提示 + 题面；
// 标题行右侧扩展位走 #extra 插槽。
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
    <!-- 内容列 860 居中：标题与正文同列对齐 -->
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
      <MdView :content="problem.description_html || problem.description" />
    </div>
  </el-card>
</template>

<style scoped>
/* 内容列 860 居中（窄容器自然退化全宽） */
.statement {
  max-width: 860px;
  margin: 0 auto;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 页头大标题 22/600：层级顶点 */
.big-title {
  font-size: var(--soj-fs-page-title);
  font-weight: var(--soj-fw-emphasis);
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

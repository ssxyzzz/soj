<script setup lang="ts">
// 题面主卡：详情页主列与在线编程左栏共用——#id 标题 + 右侧（隐藏标记/时限/内存）+ 可选
// 文件 IO 提示 + 题面；标题行右侧扩展位走 #extra 插槽（详情页放「主题库隐藏」标记，
// 顺序：隐藏 → 时间限制 → 空间限制）。
import MdView from '@/components/MdView.vue'
import { Timer, Cpu } from '@element-plus/icons-vue'
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
        <div class="title-extra">
          <slot name="extra" />
          <span class="limit" title="时间限制">
            <el-icon><Timer /></el-icon>{{ problem.time_limit_ms }} ms
          </span>
          <span class="limit" title="内存限制">
            <el-icon><Cpu /></el-icon>{{ fmtMem(problem.memory_limit_kb) }}
          </span>
        </div>
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
      <!-- 无测试数据空壳题（v0.6.1 验收反馈）：提示样式与文件 IO 提示同款，分角色文案 -->
      <el-alert
        v-if="problem.has_cases === false"
        type="warning"
        :closable="false"
        class="fileio-alert"
        show-icon
        :title="problem.can_edit_statement ? '本题暂无测试数据，暂不可提交' : '本题暂无测试数据，无法评测。请联系管理员上传数据'"
        :description="problem.can_edit_statement ? '请到「编辑题目」页上传测试数据 zip（整包替换），上传后题目即可正常提交评测。' : undefined"
      />
      <MdView :content="problem.description" />
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
  /* 下方提示（文件 IO/空壳题）与正文的间距 */
  margin-bottom: 14px;
}

/* 页头大标题 22/600：层级顶点 */
.big-title {
  font-size: var(--soj-fs-page-title);
  font-weight: var(--soj-fw-emphasis);
  line-height: 1.4;
}

/* 标题行右侧：隐藏标记 + 时限/空间（推到最右，窄屏时随内容换行） */
.title-extra {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-left: auto;
}

/* 限制项：图标 + 数字 + 单位 */
.limit {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-13);
  white-space: nowrap;
}

.limit .el-icon {
  font-size: 15px;
}

.fileio-alert {
  margin-bottom: 16px;
}
</style>

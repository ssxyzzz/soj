<script setup lang="ts">
import { computed } from 'vue'
import { fmtSec } from '@/utils/format'
// 题面主卡：详情页主列与在线编程左栏共用——#id 标题 + 右侧（隐藏标记/时限/内存）+ 可选
// 文件 IO 提示 + 题面；标题行右侧扩展位走 #extra 插槽（详情页放「主题库隐藏」标记，
// 顺序：隐藏 → 时间限制 → 空间限制）。
import MdView from '@/components/MdView.vue'
import { Timer, Cpu } from '@element-plus/icons-vue'
import { fmtMem } from '@/utils/format'
import type { ProblemDetail } from '@/types'

const props = defineProps<{
  problem: ProblemDetail
  fileIo?: boolean // 比赛/考试启用文件 IO 时显示提示（做题页主列与在线编程左栏同款）
}>()

// 客观题（v0.9.3）：整卷题型摘要徽章（全同型给具体类型，混合给「客观题」）
const isObjective = computed(() => props.problem.type === 'objective')
const objectiveLabel = computed(() => {
  const qs = props.problem.questions ?? []
  if (!qs.length) return '客观题'
  const kinds = new Set(qs.map((q) => q.kind))
  if (kinds.size > 1) return `客观题 · ${qs.length} 题`
  const map: Record<string, string> = { single: '单选题', multiple: '多选题', judge: '判断题', blank: '填空题' }
  return map[qs[0].kind] ?? '客观题'
})

// 函数题/交互题徽章（v0.9.1/v0.9.2）
const kindLabel = computed(() => {
  if (props.problem.type === 'function') return '函数题'
  if (props.problem.type === 'interactive') return '交互题'
  return ''
})
</script>

<template>
  <el-card>
    <!-- 题面内容撑满卡片宽度：与下方满宽卡片（作答卡等）左右缘对齐 -->
    <div class="statement">
      <div class="title-row">
        <span class="big-title">#{{ problem.id }} {{ problem.title }}</span>
        <el-tag v-if="problem.type === 'objective'" size="small" type="info" class="kind-tag">
          {{ objectiveLabel }}
        </el-tag>
        <el-tag v-else-if="kindLabel" size="small" type="info" class="kind-tag">{{ kindLabel }}</el-tag>
        <div class="title-extra">
          <slot name="extra" />
          <template v-if="!isObjective">
            <span class="limit" title="时间限制">
              <el-icon><Timer /></el-icon>{{ fmtSec(problem.time_limit_ms) }}
            </span>
            <span class="limit" title="内存限制">
              <el-icon><Cpu /></el-icon>{{ fmtMem(problem.memory_limit_kb) }}
            </span>
          </template>
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
      <!-- 交互题（v0.9.2）：交互方式说明（选手不读输入文件，通过 stdin/stdout 与交互器通信） -->
      <el-alert
        v-if="problem.type === 'interactive'"
        type="info"
        :closable="false"
        class="fileio-alert"
        show-icon
        title="本题为交互题：通过标准输入输出与评测器交互"
        description="评测器会实时读取你的输出并回应，按题面约定的协议交替读写；不要使用文件读写。仅支持 C / C++。"
      />
      <!-- 无测试数据空壳题（v0.6.1 验收反馈）：提示样式与文件 IO 提示同款，分角色文案 -->
      <el-alert
        v-if="!isObjective && problem.has_cases === false"
        type="warning"
        :closable="false"
        class="fileio-alert"
        show-icon
        title="本题暂无测试数据，不可提交"
      />
      <MdView :content="problem.description" />
    </div>
  </el-card>
</template>

<style scoped>
/* 不限宽：题面随主列撑满卡片（容器 1440 已封顶行宽），避免右侧大片留白 */
.statement {
  margin: 0;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  /* 下方提示（文件 IO/空壳题）与正文的间距 */
  margin-bottom: 14px;
}

.kind-tag {
  flex: 0 0 auto;
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

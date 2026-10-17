<script setup lang="ts">
// 评测详情弹窗（共享）：状态标签点击后查看该评测记录的代码与测试点明细。
// 数据来源二选一：submissionId（按需拉取 /submissions/:id，可见性由后端判定）或
// submission（列表已带代码与明细时直接传入，避免多余请求）。
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import { submissionApi } from '@/api'
import { fmtDate, fmtMem, statusDesc, statusStyle } from '@/utils/format'
import type { Submission } from '@/types'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    submissionId?: number | null
    submission?: Submission | null
    title?: string
  }>(),
  { submissionId: null, submission: null, title: '' },
)

const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const loading = ref(false)
const detail = ref<Submission | null>(null)

const sub = computed<Submission | null>(() => props.submission ?? detail.value)
const heading = computed(() => props.title || (sub.value ? `评测详情 #${sub.value.id}` : '评测详情'))

async function load() {
  if (!props.submissionId) return
  loading.value = true
  try {
    detail.value = await submissionApi.get(props.submissionId)
  } catch {
    // 403/404 等已由拦截器提示（满分解锁/隐藏期等），关闭弹窗
    emit('update:modelValue', false)
  } finally {
    loading.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      detail.value = null
      if (props.submissionId && !props.submission) load()
    } else {
      detail.value = null
    }
  },
)
</script>

<template>
  <el-dialog :model-value="modelValue" :title="heading" width="1020" top="4vh" @update:model-value="(v: boolean) => emit('update:modelValue', v)">
    <div v-loading="loading">
      <template v-if="sub">
        <el-descriptions :column="3" size="small" border class="detail-desc">
          <el-descriptions-item label="状态">
            <el-tooltip v-if="statusDesc(sub.status)" placement="top" effect="light">
              <template #content>
                <div class="status-tip">
                  <div>{{ statusDesc(sub.status)!.meaning }}</div>
                  <div v-if="statusDesc(sub.status)!.cause">常见原因：{{ statusDesc(sub.status)!.cause }}</div>
                </div>
              </template>
              <el-tag size="small" :type="statusStyle(sub.status).type" class="status-tag">{{ statusStyle(sub.status).label }}</el-tag>
            </el-tooltip>
            <el-tag v-else size="small" :type="statusStyle(sub.status).type">{{ statusStyle(sub.status).label }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="得分">{{ sub.score }}</el-descriptions-item>
          <el-descriptions-item label="语言">{{ sub.language }}</el-descriptions-item>
          <el-descriptions-item label="耗时">{{ sub.time_ms }} ms</el-descriptions-item>
          <el-descriptions-item label="内存">{{ fmtMem(sub.memory_kb) }}</el-descriptions-item>
          <el-descriptions-item label="提交时间">{{ fmtDate(sub.created_at) }}</el-descriptions-item>
        </el-descriptions>
        <template v-if="sub.error_info">
          <div class="detail-sub">错误信息</div>
          <pre class="mono sub-code">{{ sub.error_info }}</pre>
        </template>
        <div class="detail-sub">代码</div>
        <pre class="mono sub-code">{{ sub.code }}</pre>
        <template v-if="sub.detail?.length">
          <div class="detail-sub">测试点明细</div>
          <el-table :data="sub.detail" size="small" border>
            <el-table-column prop="index" label="测试点" width="90" />
            <el-table-column label="结果">
              <template #default="{ row }">
                <el-tag size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="耗时" width="120">
              <template #default="{ row }">{{ row.time_ms }} ms</template>
            </el-table-column>
            <el-table-column label="内存" width="120">
              <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
            </el-table-column>
          </el-table>
        </template>
      </template>
    </div>
  </el-dialog>
</template>

<style scoped>
.detail-desc {
  margin-bottom: 4px;
}

.detail-sub {
  font-weight: 600;
  margin: 14px 0 8px;
}

.sub-code {
  margin: 0;
  padding: 10px 14px;
  background: #f8f8f8;
  border-radius: 4px;
  max-height: 320px;
  overflow: auto;
}

.status-tag {
  cursor: help;
}

.status-tip {
  line-height: 1.6;
  max-width: 260px;
  white-space: normal;
}

.status-tip div + div {
  margin-top: 4px;
  color: #909399;
}
</style>

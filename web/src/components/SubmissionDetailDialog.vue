<script setup lang="ts">
// 评测详情弹窗（共享）：状态标签点击后查看该评测记录的代码与测试点明细。
// 数据来源二选一：submissionId（按需拉取 /submissions/:id，可见性由后端判定）或
// submission（列表已带代码与明细时直接传入，避免多余请求）。
// 教师级权限（submission.view.all）可重测：重置 Pending 交还判题池，轮询至终态。
// v0.7.1：队列态（排队/评测/取消中）可取消评测——本人 ∨ submission.view.all。
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { submissionApi } from '@/api'
import { judgeQueueApi } from '@/api/judgeQueue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, isQueueStatus, statusDesc } from '@/utils/format'
import { useUserStore } from '@/stores/user'
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

const userStore = useUserStore()
const loading = ref(false)
const detail = ref<Submission | null>(null)
const polled = ref<Submission | null>(null) // 重测/取消轮询拿到的最新数据（优先于传入的列表快照）
const rejudging = ref(false)
const cancelling = ref(false)
let pollToken = 0

const sub = computed<Submission | null>(() => polled.value ?? props.submission ?? detail.value)
const heading = computed(() => props.title || (sub.value ? `评测详情 #${sub.value.id}` : '评测详情'))
const canRejudge = computed(() => userStore.can('submission.view.all'))
const judging = computed(() => isQueueStatus(sub.value?.status))
// 取消评测：仅队列态（排队/评测/取消中）且 本人 ∨ 查看全部提交
const canCancel = computed(
  () =>
    judging.value &&
    !!sub.value &&
    (sub.value.user_id === userStore.uid || userStore.can('submission.view.all')),
)
// 测试点按题目数据顺序展示（防后端返回顺序波动）；系统级错误（沙箱失败等）并入表尾明细行
const sortedDetail = computed(() => {
  const rows = [...(sub.value?.detail ?? [])].sort((a, b) => a.index - b.index)
  const info = sub.value?.error_info
  // 「第 N 个测试点: …」形态的失败原因已由逐点明细行的 msg 覆盖，不重复并入；
  // 只有基础设施类错误（沙箱调用失败/编译错误等，无对应明细行）才补表尾行
  if (info && !/^第 \d+ 个测试点/.test(info)) rows.push({ msg: info } as never)
  return rows
})

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

async function onRejudge() {
  const id = sub.value?.id
  if (!id || rejudging.value) return
  rejudging.value = true
  try {
    await submissionApi.rejudge(id)
    ElMessage.success(`#${id} 已加入重测队列`)
    pollUntilDone(id, `#${id} 重测完成`)
  } finally {
    rejudging.value = false
  }
}

async function onCancel() {
  const s = sub.value
  if (!s || cancelling.value) return
  try {
    await ElMessageBox.confirm(
      `确定取消评测 #${s.id} 吗？取消后本次评测作废（得分 0），不可恢复。`,
      '取消评测',
      { type: 'warning', confirmButtonText: '取消评测', cancelButtonText: '再等等' },
    )
  } catch {
    return
  }
  cancelling.value = true
  try {
    const r = await judgeQueueApi.cancel(s.id)
    if (r.status === 'Cancelled') {
      ElMessage.success(`#${s.id} 已取消`)
      pollUntilDone(s.id, `#${s.id} 已取消`)
    } else {
      ElMessage.success(`#${s.id} 将在当前测试点结束后停止评测`)
      pollUntilDone(s.id, `#${s.id} 已取消`)
    }
  } finally {
    cancelling.value = false
  }
}

// 轮询：1.2s 一次拉详情，到终态停并提示（重测/取消共用）
function pollUntilDone(id: number, doneMsg: string) {
  const token = ++pollToken
  const tick = async () => {
    if (token !== pollToken || !props.modelValue) return
    try {
      const s = await submissionApi.get(id)
      polled.value = s
      if (isQueueStatus(s.status)) {
        window.setTimeout(tick, 1200)
      } else {
        ElMessage.success(doneMsg)
      }
    } catch {
      // 拉取失败已由拦截器提示，停止本轮轮询
    }
  }
  window.setTimeout(tick, 1200)
}

watch(
  () => props.modelValue,
  (open) => {
    pollToken++ // 关闭弹窗即停止重测轮询
    if (open) {
      detail.value = null
      polled.value = null
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
              <SubStatus :status="sub.status" :score="sub.score" />
            </el-tooltip>
            <SubStatus v-else :status="sub.status" :score="sub.score" />
            <el-button
              v-if="canRejudge"
              link
              type="primary"
              size="small"
              class="ml8"
              :loading="rejudging || judging"
              :disabled="judging"
              @click="onRejudge"
            >
              {{ judging ? '评测中' : '重测' }}
            </el-button>
            <el-button
              v-if="canCancel"
              link
              type="danger"
              size="small"
              class="ml8"
              :loading="cancelling"
              @click="onCancel"
            >
              取消评测
            </el-button>
          </el-descriptions-item>
          <el-descriptions-item label="语言">{{ sub.language }}</el-descriptions-item>
          <el-descriptions-item label="总耗时">{{ sub.time_ms }} ms</el-descriptions-item>
          <el-descriptions-item label="总内存">{{ fmtMem(sub.memory_kb) }}</el-descriptions-item>
          <el-descriptions-item label="提交时间">{{ fmtDate(sub.created_at) }}</el-descriptions-item>
        </el-descriptions>
        <div class="detail-sub">代码</div>
        <pre class="mono sub-code">{{ sub.code }}</pre>
        <template v-if="sub.detail?.length">
          <div class="detail-sub">测试点明细</div>
          <el-table stripe :data="sortedDetail" size="small" border>
            <el-table-column prop="index" label="测试点" width="90" />
            <el-table-column label="结果">
              <template #default="{ row }">
                <SubStatus v-if="row.status" :status="row.status" />
              </template>
            </el-table-column>
            <el-table-column label="明细" min-width="150">
              <template #default="{ row }">
                <span v-if="row.msg" class="case-err">{{ row.msg }}</span>
              </template>
            </el-table-column>
            <el-table-column label="耗时" width="90">
              <template #default="{ row }">{{ row.time_ms != null ? `${row.time_ms} ms` : '' }}</template>
            </el-table-column>
            <el-table-column label="内存" width="90">
              <template #default="{ row }">{{ row.memory_kb ? fmtMem(row.memory_kb) : '' }}</template>
            </el-table-column>
          </el-table>
        </template>
        <template v-else-if="sub.error_info">
          <div class="detail-sub">错误信息</div>
          <pre class="mono sub-code">{{ sub.error_info }}</pre>
        </template>
      </template>
    </div>
  </el-dialog>
</template>



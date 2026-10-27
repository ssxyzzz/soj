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
import CodePane from '@/components/CodePane.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, fmtSec, isQueueStatus, statusDesc } from '@/utils/format'
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
        <!-- v3 desc-grid：三列分格条（状态格带重测/取消链接），替 el-descriptions -->
        <div class="desc-grid">
          <div class="dcell">
            状态
            <b><SubStatus v-if="sub.status" :status="sub.status" :score="sub.score" /></b>
            <el-tooltip v-if="statusDesc(sub.status)" placement="top" effect="light">
              <template #content>
                <div class="status-tip">
                  <div>{{ statusDesc(sub.status)!.meaning }}</div>
                  <div v-if="statusDesc(sub.status)!.cause">常见原因：{{ statusDesc(sub.status)!.cause }}</div>
                </div>
              </template>
              <span class="lk">说明</span>
            </el-tooltip>
            <el-button
              v-if="canRejudge"
              link
              type="primary"
              size="small"
              class="lk-btn"
              :loading="rejudging || judging"
              :disabled="judging"
              @click="onRejudge"
            >
              {{ judging ? '评测中' : '重测' }}
            </el-button>
            <el-button
              v-if="canCancel"
              link
              size="small"
              class="lk-btn lk-danger"
              :loading="cancelling"
              @click="onCancel"
            >
              取消评测
            </el-button>
          </div>
          <div class="dcell">语言<b>{{ sub.language }}</b></div>
          <div class="dcell">总耗时<b class="mono">{{ fmtSec(sub.time_ms) }}</b></div>
          <div class="dcell">总内存<b class="mono">{{ fmtMem(sub.memory_kb) }}</b></div>
          <div class="dcell">提交时间<b class="mono">{{ fmtDate(sub.created_at) }}</b></div>
          <div class="dcell">选手<b>{{ sub.username }}</b></div>
        </div>
        <!-- 客观题作答（v0.9）：替代代码区展示所选答案 -->
        <template v-if="sub.is_objective">
          <div class="detail-sub">作答</div>
          <div class="obj-answer mono">
            <div v-for="(a, i) in sub.answers" :key="i">
              第 {{ i + 1 }} 题：
              <template v-if="a.blanks?.length">
                {{ a.blanks.map((b, k) => `第${k + 1}空 ${b || '（空）'}`).join('  ') }}
              </template>
              <template v-else-if="a.answer?.length">
                {{ a.answer.map((x) => String.fromCharCode(65 + x)).join('、') }}
              </template>
              <template v-else>（未作答）</template>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="detail-sub">代码</div>
          <CodePane :code="sub.code" :language="sub.language" />
        </template>
        <template v-if="sub.detail?.length">
          <div class="detail-sub">测试点明细</div>
          <el-table stripe :data="sortedDetail" size="small" border>
            <el-table-column prop="index" label="测试点" width="90" />
            <el-table-column label="结果" width="110">
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
              <template #default="{ row }">{{ fmtSec(row.time_ms) }}</template>
            </el-table-column>
            <el-table-column label="内存" width="90">
              <template #default="{ row }">{{ row.memory_kb ? fmtMem(row.memory_kb) : '' }}</template>
            </el-table-column>
          </el-table>
          <p class="foot-hint">常见原因：溢出、数组越界、未处理多组数据；悬停状态徽章可查看说明。</p>
        </template>
        <template v-else-if="sub.error_info">
          <div class="detail-sub">错误信息</div>
          <pre class="soj-codepane mono">{{ sub.error_info }}</pre>
        </template>
      </template>
    </div>
  </el-dialog>
</template>



<style scoped>
.obj-answer {
  padding: 10px 14px;
  background: var(--el-fill-color-lighter);
  border-radius: var(--el-border-radius-base);
  line-height: 1.8;
}

/* v3 .desc-grid：三列分格条（外框 + 内部 border 分隔） */
.desc-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  border: 1px solid var(--soj-line);
  border-radius: 14px;
  overflow: hidden;
  background: var(--el-fill-color-blank);
}

.desc-grid .dcell {
  padding: 11px 16px;
  border-left: 1px solid var(--soj-line);
  border-top: 1px solid var(--soj-line);
  font-size: 13.5px;
  color: var(--soj-ink-2);
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.desc-grid .dcell:nth-child(-n + 3) {
  border-top: 0;
}

.desc-grid .dcell:nth-child(3n + 1) {
  border-left: 0;
}

.desc-grid b {
  color: var(--soj-navy);
  font-weight: var(--soj-fw-bold);
  margin-left: 6px;
}

/* 状态格的「说明 / 重测 / 取消评测」小链接 */
.lk {
  color: var(--soj-blue);
  cursor: help;
  font-weight: var(--soj-fw-emphasis);
  font-size: var(--soj-fs-12);
  margin-left: 8px;
}

.lk-btn {
  margin-left: 8px;
}

.lk-danger {
  color: var(--soj-wa);
}

.foot-hint {
  margin: 12px 0 0;
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

@media (max-width: 768px) {
  .desc-grid {
    grid-template-columns: 1fr;
  }

  .desc-grid .dcell {
    border-left: 0;
    border-top: 1px solid var(--soj-line);
  }

  .desc-grid .dcell:first-child {
    border-top: 0;
  }
}
</style>

<script setup lang="ts">
// 评测详情弹窗（共享）：状态标签点击后查看该评测记录的代码与测试点明细。
// 数据来源二选一：submissionId（按需拉取 /submissions/:id，可见性由后端判定）或
// submission（列表已带代码与明细时直接传入，避免多余请求）。
// 重测（submission.rejudge）与取消成绩/恢复成绩（submission.score.cancel）各自成位
// （v0.97.29，默认仅 root 手动授予）；取消成绩后重测拒绝，恢复须显式「恢复成绩」。
// v0.7.1：队列态（排队/评测/取消中）可取消评测——本人 ∨ submission.view.all。
import { computed, ref, watch } from 'vue'
import { usePolling } from '@/composables/usePolling'
import { pushActive, useTopic } from '@/composables/useEventStream'
import { ElMessage, ElMessageBox } from 'element-plus'

import { submissionApi, aiTutorApi } from '@/api'
import { judgeQueueApi } from '@/api/judgeQueue'
import CaseLiveBar from '@/components/CaseLiveBar.vue'
import CodePane from '@/components/CodePane.vue'
import MdView from '@/components/MdView.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, fmtSec, isQueueStatus, statusDescFor } from '@/utils/format'
import { useUserStore } from '@/stores/user'
import type { Submission, SubPushEvent } from '@/types'

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
const cancellingScore = ref(false)
const restoringScore = ref(false)

const sub = computed<Submission | null>(() => polled.value ?? props.submission ?? detail.value)
const heading = computed(() => props.title || (sub.value ? `评测详情 #${sub.value.id}` : '评测详情'))
// 重测（v0.97.29 自 view.all 拆出独立位）与取消成绩/恢复成绩各自成位，默认仅 root 手动授予
const canRejudge = computed(() => userStore.can('submission.rejudge'))
const canScoreCancel = computed(() => userStore.can('submission.score.cancel'))
// 取消成绩只作用于已出终态且未处于取消成绩态的提交（队列态/自取消无成绩可作废）
const canCancelScore = computed(
  () =>
    canScoreCancel.value &&
    !!sub.value &&
    !isQueueStatus(sub.value.status) &&
    sub.value.status !== 'Cancelled' &&
    !sub.value.score_cancelled,
)
const canRestoreScore = computed(() => canScoreCancel.value && !!sub.value?.score_cancelled)

// AI 找 bug（v0.33-④）：非 AC 提交可分析（本人提交；服务端赛时封锁/计费/审计全把关）。
// 结果 MdView 渲染在弹窗尾部；失败自动退费（服务端），拦截器 toast。
const canAiDebug = computed(
  () => !!sub.value && sub.value.status !== 'Accepted' && !isQueueStatus(sub.value.status) && !!userStore.token,
)
const aiDebugging = ref(false)
const aiDebugContent = ref('')
const aiDebugPrice = ref(0)

async function onAiDebug() {
  if (!sub.value) return
  try {
    await ElMessageBox.confirm(
      `花 ${aiDebugPrice.value} 玉让 AI 分析这份代码的错误？（对照标程定位根因，附完整修正代码）`,
      'AI 找 bug',
      { confirmButtonText: `${aiDebugPrice.value} 玉分析`, cancelButtonText: '再想想', type: 'info' },
    )
  } catch {
    return
  }
  aiDebugging.value = true
  aiDebugContent.value = ''
  try {
    const r = await aiTutorApi.debug(sub.value.id)
    aiDebugContent.value = r.content
    userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
    ElMessage.success('分析完成')
  } catch {
    // 拦截器统一提示（余额不足/每日上限/赛时封锁/生成失败已退费）
  } finally {
    aiDebugging.value = false
  }
}

// 打开弹窗时拉找 bug 单价（题目难度计价；AI 未配置静默隐藏按钮）
const aiReady = ref(false)
watch(
  () => [props.modelValue, sub.value?.problem_id] as const,
  async ([open, pid]) => {
    if (!open || !pid || !userStore.token) return
    try {
      const r = await aiTutorApi.status(pid)
      aiReady.value = r.enabled
      aiDebugPrice.value = r.debug_price ?? 0
    } catch {
      aiReady.value = false
    }
  },
  { immediate: true },
)
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

// 取消成绩/恢复成绩（v0.97.29）：终态后原位刷新详情（轮询只管队列态，这里直接拉一次）；
// 列表态由各页面自身刷新机制跟进（提交记录页 5s 轮询/弹窗内重拉）。
async function refreshInPlace() {
  const id = sub.value?.id
  if (!id) return
  try {
    polled.value = await submissionApi.get(id)
  } catch {
    /* 拦截器已提示 */
  }
}

async function onCancelScore() {
  const s = sub.value
  if (!s || cancellingScore.value) return
  let reason = ''
  try {
    const { value } = await ElMessageBox.prompt(
      `确定取消 #${s.id} 的成绩吗？得分归零、状态「成绩已取消」，榜单/进度/经验同步回退；可随时点「恢复成绩」找回。`,
      '取消成绩',
      {
        confirmButtonText: '取消成绩',
        cancelButtonText: '再想想',
        type: 'warning',
        inputPlaceholder: '取消原因（可选，学生端可见），如：与题解代码雷同',
      },
    )
    reason = (value ?? '').trim()
  } catch {
    return
  }
  cancellingScore.value = true
  try {
    await submissionApi.scoreCancel(s.id, reason)
    ElMessage.success(`#${s.id} 成绩已取消`)
    await refreshInPlace()
  } finally {
    cancellingScore.value = false
  }
}

async function onRestoreScore() {
  const s = sub.value
  if (!s || restoringScore.value) return
  try {
    await ElMessageBox.confirm(`恢复 #${s.id} 的原始判题结果吗？榜单/进度/经验将同步回归。`, '恢复成绩', {
      confirmButtonText: '恢复成绩',
      cancelButtonText: '再想想',
      type: 'warning',
    })
  } catch {
    return
  }
  restoringScore.value = true
  try {
    await submissionApi.scoreRestore(s.id)
    ElMessage.success(`#${s.id} 成绩已恢复`)
    await refreshInPlace()
  } finally {
    restoringScore.value = false
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

// 判题等待（v0.98 双通道）：SSE sub 事件为主（progress 逐点信号 300ms 节流拉
// 详情、status 终态信号即拉），1.2s 轮询为兜底——pushActive 时轮询拍零网络空转
// （推送断连自动恢复真实轮询），终态口径统一 isQueueStatus、10min 上限防队列僵死。
let pollId = 0
let pollDoneMsg: string | null = null

function applyPolled(s: Submission) {
  polled.value = s
  if (isQueueStatus(s.status)) return // 队列/评测/取消中：继续等
  if (pollId === s.id) {
    if (pollDoneMsg) ElMessage.success(pollDoneMsg)
    pollDoneMsg = null
    pollId = 0 // 终态：轮询 fn 见 0 即停
  }
}

async function fetchPolled(id: number) {
  try {
    applyPolled(await submissionApi.get(id))
  } catch {
    /* 拦截器已提示；轮询续拍自愈，事件等下一拍 */
  }
}

const judgePoll = usePolling(
  async () => {
    if (pushActive.value) return // 推送在线：轮询拍空转让位（断连自动恢复）
    if (!pollId) return false
    await fetchPolled(pollId)
  },
  1200,
  {
    maxDurationMs: 600_000,
    onExpire: () => {
      pollDoneMsg = null
      pollId = 0
      ElMessage.warning('评测等待时间过长，已停止自动刷新')
    },
  },
)

function pollUntilDone(id: number, doneMsg: string) {
  pollId = id
  pollDoneMsg = doneMsg
  if (!pushActive.value) judgePoll.restart()
  else void fetchPolled(id) // 推送在线先拉一拍拿队列态（事件随后接管）
}

// 推送断连兜底：等待中（pollId 未清）且弹窗仍开 → 恢复真实轮询
watch(pushActive, (on) => {
  if (!on && pollId && props.modelValue) judgePoll.restart()
})

// SSE sub 订阅：等待中的 pollId 或弹窗打开着的队列态提交都跟随（后者是纯增益——
// 原实现开弹窗看不轮询，现在逐点进度也实时点亮）
let progressTimer: ReturnType<typeof setTimeout> | null = null
useTopic('sub', (raw) => {
  const ev = raw as unknown as SubPushEvent
  const watching = pollId || (props.modelValue && judging.value ? sub.value?.id : 0)
  if (ev.sid !== watching) return
  if (ev.kind === 'progress') {
    // 逐点信号 300ms 节流（含尾拍）：点速快于节流窗时合并拉取
    if (progressTimer != null) return
    progressTimer = setTimeout(() => {
      progressTimer = null
      void fetchPolled(ev.sid)
    }, 300)
    return
  }
  void fetchPolled(ev.sid)
})

watch(
  () => props.modelValue,
  (open) => {
    judgePoll.stop() // 关闭弹窗即停止重测轮询
    pollId = 0
    if (open) {
      detail.value = null
      polled.value = null
      if (props.submissionId && !props.submission) load()
    } else {
      detail.value = null
    }
  },
)

// 打开即跟随（v0.98）：递交后直接打开本弹窗的场景——弹窗里的提交仍在队列态
//（Pending/Judging/取消中）时自动接管跟踪（adopt pollId，SSE 事件与兜底轮询随
// 即驱动 Pending → 编译中… → 判题中 k/N → 终态回落全程）；终态不提示 toast
//（页面级完成提示由各提交入口自己负责，弹窗只如实展示状态）。
watch(
  () => [props.modelValue, sub.value?.id, sub.value?.status] as const,
  ([open, sid, status]) => {
    if (!open || !sid || pollId === sid) return
    if (!isQueueStatus(status ?? '')) return
    pollId = sid
    if (!pushActive.value) judgePoll.restart()
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
            <b>
              <!-- 状态说明直接悬停状态徽章触发（与我的提交/全部代码弹窗同口径），不再单设「说明」链接 -->
              <el-tooltip v-if="sub.status && statusDescFor(sub)" placement="top" effect="light">
                <template #content>
                  <div class="status-tip">
                    <div>{{ statusDescFor(sub)!.meaning }}</div>
                    <div v-if="!sub.score_cancelled && statusDescFor(sub)!.cause">常见原因：{{ statusDescFor(sub)!.cause }}</div>
                    <div v-if="sub.score_cancelled && sub.score_cancelled_reason">取消原因：{{ sub.score_cancelled_reason }}</div>
                  </div>
                </template>
                <SubStatus :status="sub.status" :score="sub.score" />
              </el-tooltip>
              <SubStatus v-else-if="sub.status" :status="sub.status" :score="sub.score" />
            </b>
            <el-tag v-if="sub.score_cancelled" type="danger" size="small" effect="light" class="sc-tag">成绩已取消</el-tag>
            <el-button
              v-if="canRejudge && !judging && !sub.score_cancelled"
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
              v-if="canCancelScore"
              link
              type="danger"
              size="small"
              class="lk-btn"
              :loading="cancellingScore"
              @click="onCancelScore"
            >
              取消成绩
            </el-button>
            <el-button
              v-if="canRestoreScore"
              link
              type="success"
              size="small"
              class="lk-btn"
              :loading="restoringScore"
              @click="onRestoreScore"
            >
              恢复成绩
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
            <el-button
              v-if="canAiDebug && aiReady"
              link
              type="warning"
              size="small"
              class="lk-btn"
              :loading="aiDebugging"
              @click="onAiDebug"
            >
              AI 找 bug（{{ aiDebugPrice }} 玉）
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
        <!-- 逐点实时进度（v0.98）：判题中色块逐个点亮（SSE progress 信号驱动），
             终态后回落下方明细表；case_total 由 judge 逐点回写下发 -->
        <template v-if="judging && sub.case_total">
          <div class="detail-sub">评测进度</div>
          <CaseLiveBar :sub="sub" />
        </template>
        <!-- 明细表终态回落（v0.98）：判题中部分 detail 随逐点回写流式可见，但展示
             归色块条独占（上方 case-live）——表格只在终态出现，避免双视图叠映 -->
        <template v-if="!judging && sub.detail?.length">
          <div class="detail-sub">测试点明细<TipLabel tip="悬停状态徽章可查看该结果说明与常见原因（溢出、数组越界、未处理多组数据等）" /></div>
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
        </template>
        <template v-else-if="sub.error_info">
          <div class="detail-sub">错误信息</div>
          <pre class="soj-codepane mono">{{ sub.error_info }}</pre>
        </template>

        <!-- AI 找 bug 结果（v0.33-④）：错误定位 → 原因 → 最小修复建议 -->
        <template v-if="aiDebugContent">
          <div class="detail-sub ai-debug-sub">✦ AI 找 bug</div>
          <MdView :html="aiDebugContent" variant="compact" auto-height />
        </template>
      </template>
    </div>
  </el-dialog>
</template>



<style scoped>
.obj-answer {
  padding: var(--soj-sp-10) var(--soj-sp-14);
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
  padding: var(--soj-sp-12) var(--soj-sp-16);
  border-left: 1px solid var(--soj-line);
  border-top: 1px solid var(--soj-line);
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  display: flex;
  align-items: center;
  gap: var(--soj-sp-4);
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
  margin-left: var(--soj-sp-6);
}

/* 状态格的「重测 / 取消成绩 / 恢复成绩 / 取消评测」小链接（状态说明已并入状态徽章悬停） */
.sc-tag {
  margin-left: var(--soj-sp-8);
}

.lk-btn {
  margin-left: var(--soj-sp-8);
}

.lk-danger {
  color: var(--soj-wa);
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

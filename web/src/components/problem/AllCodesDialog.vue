<script setup lang="ts">
// 题目榜单弹窗（v0.23 Tab 化，收编原「所有代码」）：挑战榜 / 用时榜 / 内存榜 / 全部代码。
//   - 挑战榜登录即可见（榜单只含用时与名字，不泄题）；用时/内存榜维持满分门槛，
//     未解锁时 Tab 内提示（can_view_boards 由服务端判定）；
//   - 全部代码 = 原有列表 + 详情交互（排序三档/语言/提交者筛选，点行进详情）；
//   - 榜单渲染做不可见字符过滤（TopsCoding 用户名 RTL 控制符伪装翻车教训）。
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { problemApi, submissionApi, userApi } from '@/api'
import CaseLiveBar from '@/components/CaseLiveBar.vue'
import CodePane from '@/components/CodePane.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, fmtRel, fmtSec, isQueueStatus, statusDescFor } from '@/utils/format'
import { settleCasePlayback } from '@/utils/casePlayback'
import { usePolling } from '@/composables/usePolling'
import { pushActive, useTopic } from '@/composables/useEventStream'
import { useCasePlayback } from '@/composables/useCasePlayback'
import { useUserStore } from '@/stores/user'
import { languageOptions } from '@/utils/code'
import type { BoardsResult, LookupUser, Submission, SubPushEvent } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problemId: number
  problemTitle: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const PAGE_SIZE = 10
type BoardTab = 'challenge' | 'time' | 'mem' | 'codes'
const tab = ref<BoardTab>('challenge')
const boards = ref<BoardsResult | null>(null)
const boardsLoading = ref(false)

// ---------- 全部代码（原所有代码） ----------
const others = ref<Submission[]>([])
const othersTotal = ref(0)
const othersPage = ref(1)
const othersSort = ref<'recent' | 'time' | 'mem'>('recent')
const othersLang = ref('')
// 提交者筛选（2026-10-25 用户需求「选定账号查找」）：远程模糊搜账号，选中按 user_id 过滤
const othersUser = ref<number | undefined>()
const userOptions = ref<LookupUser[]>([])
const userSearching = ref(false)
const othersView = ref<'list' | 'detail'>('list')
// othersSnap = 最新真实快照（取数/动作准入看它）；othersDetail = 详情视图渲染用的展示快照
// （v0.99.1 逐点回放：判题中色块 ≥100ms/点 逐个点亮，回放走完前仍是判题中）
const othersSnap = ref<Submission | null>(null)
const othersDetail = useCasePlayback(
  othersSnap,
  () => props.modelValue && tab.value === 'codes' && othersView.value === 'detail',
)
// 测试点按题目数据顺序展示（防后端返回顺序波动）；系统级错误（沙箱失败等）并入表尾明细行
const sortedDetail = computed(() => {
  const rows = [...(othersDetail.value?.detail ?? [])].sort((a, b) => a.index - b.index)
  const info = othersDetail.value?.error_info
  if (info && !/^第 \d+ 个测试点/.test(info)) rows.push({ msg: info } as never)
  return rows
})

// 不可见字符过滤（v0.23）：RTL 覆盖符/零宽字符等控制符从展示名中剔除
const RTL_OVERRIDES = /[\u200e\u200f\u202a-\u202e\u2066-\u2069\u200b-\u200d\u2060\ufeff]/g
function safeName(row: { uid?: number; nickname?: string; username?: string }): string {
  const clean = (s?: string) => (s ?? '').replace(RTL_OVERRIDES, '')
  return clean(row.nickname) || clean(row.username) || `#${row.uid ?? ''}`
}

async function searchUsers(q: string) {
  if (!q.trim()) return // 保留候选，防已选中项显示成 uid 数字
  userSearching.value = true
  try {
    userOptions.value = await userApi.lookup(q.trim())
  } catch {
    userOptions.value = []
  } finally {
    userSearching.value = false
  }
}

// 最后一页不足 PAGE_SIZE 时补空白行，让表格保持整页高度
const displayOthers = computed(() => {
  const fill = Math.max(0, PAGE_SIZE - others.value.length)
  return [...others.value, ...Array.from({ length: fill }, () => ({}) as Submission)]
})

async function loadOthers(p = 1) {
  try {
    const r = await problemApi.submissions(
      props.problemId,
      p,
      PAGE_SIZE,
      othersSort.value,
      othersLang.value || undefined,
      othersUser.value || undefined,
    )
    others.value = r.list
    othersTotal.value = r.total
    othersPage.value = p
  } catch {
    others.value = []
  }
}

async function loadBoards() {
  boardsLoading.value = true
  try {
    boards.value = await problemApi.boards(props.problemId)
  } catch {
    boards.value = null
  } finally {
    boardsLoading.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      tab.value = 'challenge' // 默认挑战榜：低门槛即参与钩子
      othersUser.value = undefined // 换题打开重置提交者筛选
      othersView.value = 'list'
      othersSnap.value = null
      loadBoards()
      loadOthers()
    } else {
      othersPoll.stop() // 关闭弹窗即停止重测轮询
    }
  },
)

function openOthersDetail(s: Submission) {
  if (!s.id) return // 补位的空白行不可点
  othersSnap.value = s
  othersView.value = 'detail'
}

// ---- 重测 / 取消成绩 / 恢复成绩（v0.97.29）----
// 权限位可见（默认仅 root 手动授予），口径与 SubmissionDetailDialog 一致：
// 取消成绩只作用于已出终态的提交；取消后重测拒绝，恢复须显式「恢复成绩」。
const userStore = useUserStore()
const canRejudge = computed(() => userStore.can('submission.rejudge'))
const canScoreCancel = computed(() => userStore.can('submission.score.cancel'))
const rejudging = ref(false)
const scoreCancelling = ref(false)
const scoreRestoring = ref(false)
const canRejudgeDetail = computed(() => {
  const s = othersDetail.value
  return canRejudge.value && !!s && !isQueueStatus(s.status) && !s.score_cancelled
})
const canCancelScore = computed(() => {
  const s = othersDetail.value
  return canScoreCancel.value && !!s && !isQueueStatus(s.status) && s.status !== 'Cancelled' && !s.score_cancelled
})
const canRestoreScore = computed(() => canScoreCancel.value && !!othersDetail.value?.score_cancelled)

// 原位刷新详情（详情可能来自列表行快照，操作后按 id 拉最新）+ 列表回拉
async function refreshOthersDetail() {
  const id = othersDetail.value?.id
  if (id) {
    try {
      othersSnap.value = await submissionApi.get(id)
    } catch {
      /* 拦截器已提示 */
    }
  }
  loadOthers(othersPage.value)
}

// 重测等待（v0.98 双通道，口径同 SubmissionDetailDialog）：SSE sub 事件为主、
// 1.2s 轮询兜底（pushActive 时空转，断连自动恢复）；progress 信号 300ms 节流拉
// 详情喂逐点进度，终态停并提示
let othersPollId = 0
let othersProgressTimer: ReturnType<typeof setTimeout> | null = null

async function othersFetch(id: number) {
  try {
    const s = await submissionApi.get(id)
    if (othersSnap.value?.id === s.id) othersSnap.value = s
    if (isQueueStatus(s.status)) return
    othersPollId = 0
    // 完成提示等色块回放走完（与详情视图同步揭晓）；详情不在屏时立即放行
    await settleCasePlayback(s.id)
    ElMessage.success(`#${s.id} 重测完成`)
    loadOthers(othersPage.value)
  } catch {
    /* 拦截器已提示；轮询续拍自愈 */
  }
}

const othersPoll = usePolling(
  async () => {
    if (pushActive.value) return // 推送在线：轮询拍空转让位
    if (!othersPollId) return false
    await othersFetch(othersPollId)
  },
  1200,
  {
    maxDurationMs: 600_000,
    onExpire: () => ElMessage.warning('评测等待时间过长，已停止自动刷新'),
  },
)

// 同一个处理器两条流都订：全部代码列表里都是别人的提交，重测的进度与终态事件只走
// 全站流 suball（sub 只发给提交者本人）；服务端按权限过滤 suball，普通用户收不到
function onSubEvent(raw: Record<string, unknown>) {
  const ev = raw as unknown as SubPushEvent
  if (ev.sid !== othersPollId) return
  if (ev.kind === 'progress') {
    if (othersProgressTimer != null) return
    othersProgressTimer = setTimeout(() => {
      othersProgressTimer = null
      if (othersPollId) void othersFetch(othersPollId)
    }, 300)
  } else {
    void othersFetch(ev.sid)
  }
}
useTopic('sub', onSubEvent)
useTopic('suball', onSubEvent)

watch(pushActive, (on) => {
  if (!on && othersPollId) othersPoll.restart()
})

async function onOthersRejudge() {
  const s = othersDetail.value
  if (!s || rejudging.value) return
  rejudging.value = true
  try {
    await submissionApi.rejudge(s.id)
    ElMessage.success(`#${s.id} 已加入重测队列`)
    othersPollId = s.id
    if (!pushActive.value) othersPoll.restart()
    else void othersFetch(s.id)
  } finally {
    rejudging.value = false
  }
}

async function onOthersCancelScore() {
  const s = othersDetail.value
  if (!s || scoreCancelling.value) return
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
  scoreCancelling.value = true
  try {
    await submissionApi.scoreCancel(s.id, reason)
    ElMessage.success(`#${s.id} 成绩已取消`)
    await refreshOthersDetail()
  } finally {
    scoreCancelling.value = false
  }
}

async function onOthersRestoreScore() {
  const s = othersDetail.value
  if (!s || scoreRestoring.value) return
  try {
    await ElMessageBox.confirm(`恢复 #${s.id} 的原始判题结果吗？榜单/进度/经验将同步回归。`, '恢复成绩', {
      confirmButtonText: '恢复成绩',
      cancelButtonText: '再想想',
      type: 'warning',
    })
  } catch {
    return
  }
  scoreRestoring.value = true
  try {
    await submissionApi.scoreRestore(s.id)
    ElMessage.success(`#${s.id} 成绩已恢复`)
    await refreshOthersDetail()
  } finally {
    scoreRestoring.value = false
  }
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="题目榜单"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
    @closed="othersView = 'list'"
  >
    <el-tabs v-model="tab" class="board-tabs">
      <!-- 挑战榜 -->
      <el-tab-pane label="挑战榜" name="challenge" lazy>
        <el-table v-loading="boardsLoading" stripe :data="boards?.challenge ?? []" border>
          <el-table-column prop="rank" label="名次" width="70">
            <template #default="{ row }">
              <span class="rk" :class="`rk-${row.rank <= 3 ? row.rank : 'n'}`">{{ row.rank }}</span>
            </template>
          </el-table-column>
          <el-table-column label="挑战者" min-width="150">
            <template #default="{ row }">
              <router-link :to="`/users/${row.uid}`" class="board-user">{{ safeName(row) }}</router-link>
            </template>
          </el-table-column>
          <el-table-column label="用时" width="110">
            <template #default="{ row }">
              <span class="mono">{{ fmtSec(row.duration_ms) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="码字速度" width="110">
            <template #default="{ row }">
              <span class="mono">{{ row.chars_per_sec ? row.chars_per_sec.toFixed(1) : '—' }}</span>
              <span class="dim"> 字/秒</span>
            </template>
          </el-table-column>
          <el-table-column prop="lang" label="语言" width="110" />
          <el-table-column label="达成时间">
            <template #default="{ row }">{{ fmtRel(row.created_at) }}</template>
          </el-table-column>
        </el-table>
        <el-empty
          v-if="boards && !boards.challenge?.length"
          description="还没有挑战记录——点「⚡ 挑战模式」抢首杀"
          :image-size="48"
        />
      </el-tab-pane>

      <!-- 用时榜 -->
      <el-tab-pane label="用时榜" name="time" lazy>
        <div v-if="boards && !boards.can_view_boards" class="board-locked">
          该题拿到满分后可查看用时榜与内存榜（挑战榜不受限）
        </div>
        <template v-else>
          <el-table v-loading="boardsLoading" stripe :data="boards?.time ?? []" border>
            <el-table-column prop="rank" label="名次" width="70">
              <template #default="{ row }">
                <span class="rk" :class="`rk-${row.rank <= 3 ? row.rank : 'n'}`">{{ row.rank }}</span>
              </template>
            </el-table-column>
            <el-table-column label="选手" min-width="150">
              <template #default="{ row }">
                <router-link :to="`/users/${row.uid}`" class="board-user">{{ safeName(row) }}</router-link>
              </template>
            </el-table-column>
            <el-table-column label="最短用时" width="110">
              <template #default="{ row }">
                <span class="mono">{{ fmtSec(row.value) }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="lang" label="语言" width="110" />
            <el-table-column label="达成时间">
              <template #default="{ row }">{{ fmtRel(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="boards && !boards.time?.length" description="还没有可计入的通过提交" :image-size="48" />
        </template>
      </el-tab-pane>

      <!-- 内存榜 -->
      <el-tab-pane label="内存榜" name="mem" lazy>
        <div v-if="boards && !boards.can_view_boards" class="board-locked">
          该题拿到满分后可查看用时榜与内存榜（挑战榜不受限）
        </div>
        <template v-else>
          <el-table v-loading="boardsLoading" stripe :data="boards?.mem ?? []" border>
            <el-table-column prop="rank" label="名次" width="70">
              <template #default="{ row }">
                <span class="rk" :class="`rk-${row.rank <= 3 ? row.rank : 'n'}`">{{ row.rank }}</span>
              </template>
            </el-table-column>
            <el-table-column label="选手" min-width="150">
              <template #default="{ row }">
                <router-link :to="`/users/${row.uid}`" class="board-user">{{ safeName(row) }}</router-link>
              </template>
            </el-table-column>
            <el-table-column label="最小内存" width="110">
              <template #default="{ row }">
                <span class="mono">{{ fmtMem(row.value) }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="lang" label="语言" width="110" />
            <el-table-column label="达成时间">
              <template #default="{ row }">{{ fmtRel(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="boards && !boards.mem?.length" description="还没有可计入的通过提交" :image-size="48" />
        </template>
      </el-tab-pane>

      <!-- 全部代码（原「所有代码」） -->
      <el-tab-pane label="全部代码" name="codes" lazy>
        <template v-if="othersView === 'list'">
          <div class="dlg-toolbar">
            <el-radio-group v-model="othersSort" @change="loadOthers(1)">
              <el-radio-button value="recent">最近提交</el-radio-button>
              <el-radio-button value="time">时间优先</el-radio-button>
              <el-radio-button value="mem">空间优先</el-radio-button>
            </el-radio-group>
            <el-select
              v-model="othersLang"
              clearable
              placeholder="全部语言"
              style="width: 160px"
              @change="loadOthers(1)"
            >
              <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
            </el-select>
            <!-- 提交者筛选（2026-10-25 用户需求「选定账号查找」）：远程模糊搜账号 -->
            <el-select
              v-model="othersUser"
              class="user-sel"
              filterable
              remote
              clearable
              reserve-keyword
              placeholder="全部提交者"
              :remote-method="searchUsers"
              :loading="userSearching"
              @change="loadOthers(1)"
            >
              <el-option v-for="u in userOptions" :key="u.id" :value="u.id" :label="u.nickname || u.username" />
            </el-select>
            <span class="dim">共 {{ othersTotal }} 份</span>
          </div>
          <el-table
            stripe
            :data="displayOthers"
            size="small"
            border
            style="cursor: pointer"
            @row-click="(row: Submission) => openOthersDetail(row as Submission)"
          >
            <el-table-column label="状态" width="140">
              <template #default="{ row }">
                <template v-if="row.id">
                  <el-tooltip v-if="statusDescFor(row)" placement="top" effect="light">
                    <template #content>
                      <div class="status-tip">
                        <div>{{ statusDescFor(row)!.meaning }}</div>
                        <div v-if="!row.score_cancelled && statusDescFor(row)!.cause">常见原因：{{ statusDescFor(row)!.cause }}</div>
                        <div v-if="row.score_cancelled && row.score_cancelled_reason">取消原因：{{ row.score_cancelled_reason }}</div>
                      </div>
                    </template>
                    <SubStatus :status="row.status" :score="row.score" />
                  </el-tooltip>
                  <SubStatus v-else :status="row.status" :score="row.score" />
                </template>
              </template>
            </el-table-column>
            <el-table-column label="提交者" min-width="110">
              <template #default="{ row }">
                <!-- v0.75 昵称优先展示（与榜单列 safeName / 提交记录页同口径），缺省回退用户名 -->
                {{ row.nickname || row.username }}
              </template>
            </el-table-column>
            <el-table-column prop="language" label="语言" width="100" />
            <el-table-column label="总耗时" width="100">
              <template #default="{ row }">
                <template v-if="row.id">{{ fmtSec(row.time_ms) }}</template>
              </template>
            </el-table-column>
            <el-table-column label="总内存" width="110">
              <template #default="{ row }">
                <template v-if="row.id">{{ fmtMem(row.memory_kb) }}</template>
              </template>
            </el-table-column>
            <el-table-column label="提交时间">
              <template #default="{ row }">
                <template v-if="row.id">{{ fmtRel(row.created_at) }}</template>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!others.length" description="还没有人提交过" :image-size="48" />
          <div v-if="othersTotal > 0" class="side-pager">
            <el-pagination
              background
              layout="prev, pager, next"
              :total="othersTotal"
              :page-size="PAGE_SIZE"
              :current-page="othersPage"
              @current-change="(p: number) => loadOthers(p)"
            />
          </div>
        </template>
        <template v-else-if="othersDetail">
          <!-- 详情头行：返回列表 + 管理操作居左（重测/取消成绩/恢复成绩，v0.97.29 各自权限位可见） -->
          <div class="detail-top">
            <div class="detail-top-l">
              <el-button @click="othersView = 'list'; othersPoll.stop()">← 返回列表</el-button>
              <el-button v-if="canRejudgeDetail" link type="primary" :loading="rejudging" @click="onOthersRejudge">
                重测
              </el-button>
              <el-tag v-if="othersDetail.score_cancelled" type="danger" size="small" effect="light">成绩已取消</el-tag>
              <el-button v-if="canCancelScore" link type="danger" :loading="scoreCancelling" @click="onOthersCancelScore">
                取消成绩
              </el-button>
              <el-button v-if="canRestoreScore" link type="success" :loading="scoreRestoring" @click="onOthersRestoreScore">
                恢复成绩
              </el-button>
            </div>
          </div>
          <el-descriptions :column="3" border class="detail-desc">
            <el-descriptions-item label="状态">
              <el-tooltip v-if="statusDescFor(othersDetail)" placement="top" effect="light">
                <template #content>
                  <div class="status-tip">
                    <div>{{ statusDescFor(othersDetail)!.meaning }}</div>
                    <div v-if="!othersDetail.score_cancelled && statusDescFor(othersDetail)!.cause">常见原因：{{ statusDescFor(othersDetail)!.cause }}</div>
                    <div v-if="othersDetail.score_cancelled && othersDetail.score_cancelled_reason">取消原因：{{ othersDetail.score_cancelled_reason }}</div>
                  </div>
                </template>
                <SubStatus :status="othersDetail.status" :score="othersDetail.score" />
              </el-tooltip>
              <SubStatus v-else :status="othersDetail.status" :score="othersDetail.score" />
            </el-descriptions-item>
            <el-descriptions-item label="提交者">{{ othersDetail.username }}</el-descriptions-item>
            <el-descriptions-item label="语言">{{ othersDetail.language }}</el-descriptions-item>
            <el-descriptions-item label="总耗时">{{ fmtSec(othersDetail.time_ms) }}</el-descriptions-item>
            <el-descriptions-item label="总内存">{{ fmtMem(othersDetail.memory_kb) }}</el-descriptions-item>
            <el-descriptions-item label="提交时间">{{ fmtDate(othersDetail.created_at) }}</el-descriptions-item>
          </el-descriptions>
          <!-- 有测试点明细时错误原因已在明细列展示（表尾 err 行），仅无明细时独立成块（如编译错误） -->
          <template v-if="othersDetail.error_info && !sortedDetail.length">
            <div class="detail-sub">错误信息</div>
            <pre class="mono sub-code">{{ othersDetail.error_info }}</pre>
          </template>
          <div class="detail-sub">代码</div>
          <CodePane :code="othersDetail.code" :language="othersDetail.language" />
          <!-- 逐点实时进度（v0.98）：判题中色块逐个点亮，终态回落下方明细表；
               othersDetail 是回放展示快照（v0.99.1），快题按 ≥100ms/点 逐个点亮 -->
          <template v-if="isQueueStatus(othersDetail.status) && othersDetail.case_total">
            <div class="detail-sub">评测进度</div>
            <CaseLiveBar :sub="othersDetail" />
          </template>
          <!-- 明细表终态回落（v0.98）：判题中展示归色块条独占，表格只在终态出现 -->
          <template v-if="!isQueueStatus(othersDetail.status) && othersDetail.detail?.length">
            <div class="detail-sub">测试点明细</div>
            <el-table stripe :data="sortedDetail" border>
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
                <template #default="{ row }">{{ fmtSec(row.time_ms) }}</template>
              </el-table-column>
              <el-table-column label="内存" width="90">
                <template #default="{ row }">{{ row.memory_kb ? fmtMem(row.memory_kb) : '' }}</template>
              </el-table-column>
            </el-table>
          </template>
          <template v-else-if="othersDetail.error_info">
            <div class="detail-sub">错误信息</div>
            <pre class="mono sub-code">{{ othersDetail.error_info }}</pre>
          </template>
        </template>
      </el-tab-pane>
    </el-tabs>
  </el-dialog>
</template>

<style scoped>
.dlg-toolbar {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  margin-bottom: var(--soj-sp-12);
}

/* 提交者筛选下拉 */
.user-sel {
  width: 170px;
}

.board-tabs :deep(.el-tabs__header) {
  margin-bottom: var(--soj-sp-14);
}

.board-user {
  color: var(--soj-ink);
  text-decoration: none;
  font-weight: var(--soj-fw-emphasis);
}
.board-user:hover {
  color: var(--soj-blue);
}

/* 三甲名次配色 */
.rk {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 24px;
  height: 24px;
  padding: 0 var(--soj-sp-6);
  border-radius: 8px;
  font-weight: var(--soj-fw-heavy);
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  background: var(--soj-surface-2);
}
.rk-1 {
  color: var(--soj-warning-deep);
  background: color-mix(in srgb, var(--soj-warning) 22%, var(--soj-white));
}
.rk-2 {
  color: var(--soj-ink-2);
  background: color-mix(in srgb, var(--soj-mut) 26%, var(--soj-white));
}
.rk-3 {
  color: var(--soj-warning-deep);
  background: color-mix(in srgb, var(--soj-warning-deep) 16%, var(--soj-white));
}

.board-locked {
  padding: var(--soj-sp-48) 0;
  text-align: center;
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-14);
}

.dim {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* 详情头行左侧操作组：返回列表 + 重测/取消成绩/恢复成绩/成绩已取消标记 */
.detail-top-l {
  display: flex;
  align-items: center;
}

.detail-top-l .el-tag {
  margin-left: 12px;
}
</style>

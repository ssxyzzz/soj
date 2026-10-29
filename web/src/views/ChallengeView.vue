<script setup lang="ts">
// 挑战模式（v0.23）：限时手速 PK 全屏页。
//   - 计时权威在服务端：deadline 由 start/resume 下发，本地倒计时纯展示；
//   - 编辑器空白起跑：不恢复在线编程草稿；自动保存仅限本会话（刷新恢复 running 会话用）；
//   - 防复制粘贴只拦不罚：paste/拖放/右键/选中文本四件套，undo/redo 放开；
//   - 打字流水：每秒相对快照（增删字符数）随递交上传，仅作服务端事后审计留存；
//   - 自测可用且计时照走（策略张力）。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import CodeEditor from '@/components/CodeEditor.vue'
import ProblemStatement from '@/components/problem/ProblemStatement.vue'
import SelfTestPanel from '@/components/SelfTestPanel.vue'
import { challengeApi, problemApi, submissionApi } from '@/api'
import { isQueueStatus } from '@/utils/format'
import type { ProblemDetail } from '@/types'

const route = useRoute()
const router = useRouter()
const pid = Number(route.params.id)

// ---------- 页面状态机：loading / idle / countdown / running / success / fail ----------
type Phase = 'loading' | 'idle' | 'countdown' | 'running' | 'success' | 'fail'
const phase = ref<Phase>('loading')
const problem = ref<ProblemDetail | null>(null)
const challengeSec = ref(0)
const challengeId = ref(0)
const startMs = ref(0)
const deadlineMs = ref(0)
const nowTick = ref(Date.now())
const lastFailMsg = ref('')

// ---------- 编辑器 ----------
const language = ref('cpp')
const code = ref('')

// ---------- 递交与轮询 ----------
const submitting = ref(false)
const judging = ref(false)
let pollTimer: number | null = null

// ---------- 自测（共用 SelfTestPanel；挑战中可用且计时照走） ----------
const selfTestOpen = ref(false)

const LANGS = [
  { value: 'cpp', label: 'C++ 14' },
  { value: 'c', label: 'C (gcc)' },
  { value: 'python3', label: 'Python 3' },
]

// ---------- 倒计时 ----------
const remainingMs = computed(() => Math.max(0, deadlineMs.value - nowTick.value))
const remainText = computed(() => {
  const total = Math.ceil(remainingMs.value / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s < 10 ? '0' : ''}${s}`
})
const urgent = computed(() => remainingMs.value <= 10_000 && phase.value === 'running')

let tickTimer: number | null = null
function startTick() {
  stopTick()
  tickTimer = window.setInterval(() => {
    nowTick.value = Date.now()
    if (phase.value === 'running' && remainingMs.value <= 0) {
      // 本地先落败者相（服务端懒清扫为权威终态）
      finishFail('时间到！手速再快一点，下次挑战一定行。')
    }
  }, 250)
}
function stopTick() {
  if (tickTimer != null) {
    window.clearInterval(tickTimer)
    tickTimer = null
  }
}

// ---------- 打字流水（审计） ----------
const changelog = ref<{ ti: number; dc: string }[]>([])
let lastSnap = ''
let snapTimer: number | null = null
function startSnapshots() {
  stopSnapshots()
  lastSnap = code.value
  changelog.value = []
  snapTimer = window.setInterval(() => {
    if (phase.value !== 'running') return
    const cur = code.value
    if (cur === lastSnap) return
    // 公共前缀/后长度 → 单步增删字符数（紧凑审计口径：总量守恒可对账）
    let pre = 0
    const maxPre = Math.min(cur.length, lastSnap.length)
    while (pre < maxPre && cur[pre] === lastSnap[pre]) pre++
    let suf = 0
    const maxSuf = Math.min(cur.length, lastSnap.length) - pre
    while (suf < maxSuf && cur[cur.length - 1 - suf] === lastSnap[lastSnap.length - 1 - suf]) suf++
    const added = cur.length - pre - suf
    const removed = lastSnap.length - pre - suf
    changelog.value.push({ ti: Date.now() - startMs.value, dc: `+${added}/-${removed}` })
    lastSnap = cur
  }, 1000)
}
function stopSnapshots() {
  if (snapTimer != null) {
    window.clearInterval(snapTimer)
    snapTimer = null
  }
}

// ---------- 自动保存（仅本会话） ----------
const draftKey = computed(() => `soj-challenge-${pid}`)
let saveTimer: number | null = null
watch(code, () => {
  if (phase.value !== 'running') return
  if (saveTimer != null) window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => {
    try {
      localStorage.setItem(draftKey.value, code.value)
    } catch {
      /* 隐私模式等写入失败静默 */
    }
  }, 800)
})
function restoreDraft() {
  try {
    const d = localStorage.getItem(draftKey.value)
    if (d) code.value = d
  } catch {
    /* ignore */
  }
}
function clearDraft() {
  try {
    localStorage.removeItem(draftKey.value)
  } catch {
    /* ignore */
  }
}

// ---------- 流程 ----------
onMounted(async () => {
  try {
    const [p, st] = await Promise.all([problemApi.detail(pid), challengeApi.status(pid)])
    problem.value = p
    challengeSec.value = p.challenge?.sec ?? 0
    const active = st.active as { id?: number; remaining_ms?: number; deadline_at?: number }
    if (active && active.id && (active.remaining_ms ?? 0) > 0) {
      // 恢复进行中的会话：跳过开场倒数，恢复草稿与流水起点
      challengeId.value = active.id
      deadlineMs.value = Date.now() + (active.remaining_ms ?? 0)
      startMs.value = Date.now() - (challengeSec.value * 1000 - (active.remaining_ms ?? 0))
      restoreDraft()
      phase.value = 'running'
      startTick()
      startSnapshots()
      ElMessage.warning('你有正在进行的挑战，已恢复倒计时')
    } else {
      phase.value = 'idle'
      if (st.last && (st.last.status === 'timeout' || st.last.status === 'quit')) {
        lastFailMsg.value = st.last.status === 'timeout' ? '上一次挑战超时未通过' : '上一次挑战已退出'
      }
    }
  } catch {
    ElMessage.error('题目加载失败')
    router.replace(`/problems/${pid}`)
  }
  startTick()
})

const countdownNum = ref(0)
let cdTimer: number | null = null
// v0.27 卸载守卫：3-2-1 倒计时窗口内离页时，beginChallenge 的续段不得在已卸载组件上
// 启动快照 interval（曾致永无人清理的 1s 定时器钉住整份组件闭包）
let alive = true

onBeforeUnmount(() => {
  alive = false
  stopTick()
  stopSnapshots()
  if (pollTimer != null) window.clearTimeout(pollTimer)
  if (saveTimer != null) window.clearTimeout(saveTimer)
  if (cdTimer != null) {
    window.clearInterval(cdTimer)
    cdTimer = null
  }
})

async function beginChallenge() {
  let startedAt = 0
  try {
    const r = await challengeApi.start(pid)
    challengeId.value = r.id
    challengeSec.value = r.sec
    // 服务端权威时刻：started_at 已含 3s 前摇、deadline_at = started_at + 时长
    startedAt = r.started_at
    deadlineMs.value = r.deadline_at
  } catch {
    return // 拦截器已 toast
  }
  // 3-2-1-GO 全屏倒数（与后端 challengePrerollMS 对齐，GO 落地才可写）
  phase.value = 'countdown'
  code.value = ''
  clearDraft()
  countdownNum.value = 3
  let n = 3
  await new Promise<void>((resolve) => {
    cdTimer = window.setInterval(() => {
      n -= 1
      countdownNum.value = n
      if (n <= 0) {
        if (cdTimer != null) window.clearInterval(cdTimer)
        cdTimer = null
        resolve()
      }
    }, 1000)
  })
  if (!alive) return // 倒计时中被卸载：会话留给服务端懒清扫（timeout/quit 口径）
  startMs.value = startedAt
  phase.value = 'running'
  startSnapshots()
  ElMessage.success('挑战开始！')
}

function finishFail(msg: string) {
  if (phase.value !== 'running') return
  phase.value = 'fail'
  lastFailMsg.value = msg
  stopSnapshots()
  clearDraft()
  // 不主动 quit：服务端懒清扫会把到期会话落为 timeout（DNF 口径），新开挑战时旧会话才会被顶为 quit
}

async function quitChallenge() {
  try {
    await ElMessageBox.confirm('确定退出本次挑战吗？当前会话将作废，不进榜单。', '退出挑战', {
      confirmButtonText: '退出',
      cancelButtonText: '继续挑战',
      type: 'warning',
    })
  } catch {
    return
  }
  stopTick()
  stopSnapshots()
  clearDraft()
  await challengeApi.quit(pid).catch(() => {})
  router.push(`/problems/${pid}`)
}

function backToProblem() {
  router.push(`/problems/${pid}`)
}

// ---------- 递交（走正常判题管道，带挑战会话） ----------
const verdict = ref('')
async function doSubmit() {
  if (phase.value !== 'running' || submitting.value || judging.value) return
  if (!code.value.trim()) {
    ElMessage.warning('先写下你的代码再递交')
    return
  }
  submitting.value = true
  verdict.value = ''
  try {
    const { id } = await submissionApi.create(pid, language.value, code.value, {
      challengeId: challengeId.value,
      changelog: changelog.value,
    })
    judging.value = true
    pollVerdict(id)
  } catch {
    /* 拦截器已 toast */
  } finally {
    submitting.value = false
  }
}

function pollVerdict(sid: number) {
  if (pollTimer != null) window.clearTimeout(pollTimer)
  pollTimer = window.setTimeout(async () => {
    try {
      const s = await submissionApi.get(sid)
      // 终态统一 isQueueStatus 口径（含 CancelRequested——自写 ['Pending','Judging']
      // 会把取消收尾期当终态，v0.29 根修分叉）
      const terminal = !isQueueStatus(s.status)
      if (!terminal) {
        pollVerdict(sid)
        return
      }
      judging.value = false
      verdict.value = s.status
      if (s.status === 'Accepted') {
        // 挑战成功：服务端已在 AC 回写时终判（duration 以提交创建时刻起算）
        phase.value = 'success'
        stopSnapshots()
        clearDraft()
      } else {
        ElMessage.error(`评测结果：${s.status}，倒计时继续，加油！`)
      }
    } catch {
      judging.value = false
    }
  }, 1200)
}

// 用时 = 总时长 - 剩余（结果横幅展示口径）
const usedText = computed(() => {
  const total = Math.ceil((challengeSec.value * 1000 - remainingMs.value) / 1000)
  const m = Math.floor(total / 60)
  const s = Math.max(0, total % 60)
  return `${m}:${s < 10 ? '0' : ''}${s}`
})

// 防复制粘贴四件套（只拦不罚）：绑定在编辑器包裹层捕获阶段
function swallow(e: Event) {
  e.preventDefault()
}
</script>

<template>
  <div class="chal-page">
    <!-- 顶栏 -->
    <header class="chal-head">
      <div class="chal-head-left">
        <span class="chal-brand">挑战模式</span>
        <h1 class="chal-title">{{ problem?.title ?? '…' }}</h1>
        <span v-if="challengeSec" class="chal-limit">限时 {{ Math.round(challengeSec / 60) }} 分钟</span>
      </div>
      <div class="chal-head-right">
        <div class="chal-timer" :class="{ urgent }" v-if="phase === 'running' || phase === 'countdown'">
          <span class="chal-timer-icon">⏱</span>
          <span class="chal-timer-text">{{ remainText }}</span>
        </div>
        <el-select v-model="language" class="chal-lang" :disabled="phase !== 'running'">
          <el-option v-for="l in LANGS" :key="l.value" :label="l.label" :value="l.value" />
        </el-select>
        <el-button :disabled="phase !== 'running'" @click="selfTestOpen = !selfTestOpen">
          {{ selfTestOpen ? '收起自测' : '自测' }}
        </el-button>
        <el-button type="danger" plain :disabled="phase !== 'running'" @click="quitChallenge">退出挑战</el-button>
      </div>
    </header>

    <!-- 主体：左题面右编辑器 -->
    <div class="chal-body" v-if="problem">
      <aside class="chal-statement">
        <ProblemStatement :problem="problem" />
      </aside>
      <main class="chal-editor-col">
        <!-- 自测面板（共用 SelfTestPanel） -->
        <SelfTestPanel
          v-if="selfTestOpen"
          class="chal-selftest"
          :problem-id="pid"
          :language="language"
          :code="code"
        />
        <!-- 编辑器：防粘贴四件套挂捕获层 -->
        <div
          class="chal-editor-wrap"
          @paste.capture="swallow"
          @dragenter.capture="swallow"
          @dragover.capture="swallow"
          @drop.capture="swallow"
          @contextmenu.capture="swallow"
          @mouseup.capture="swallow"
        >
          <CodeEditor v-model="code" :language="language" height="100%" :tab-indent="true" />
        </div>
        <div class="chal-foot">
          <span class="chal-verdict" v-if="verdict">上次递交：{{ verdict }}</span>
          <span class="chal-hint">挑战中禁止粘贴、拖入与选中文本；撤销/重做不受限</span>
          <el-button
            type="primary"
            class="chal-submit"
            :loading="submitting || judging"
            :disabled="phase !== 'running'"
            @click="doSubmit"
          >
            {{ judging ? '评测中…' : '递交评测' }}
          </el-button>
        </div>
      </main>
    </div>

    <!-- idle 遮罩：开场说明 + 开始挑战 -->
    <div class="chal-overlay" v-if="phase === 'idle' || phase === 'loading'">
      <div class="chal-overlay-card" v-if="phase === 'idle'">
        <div class="chal-kicker">CHALLENGE</div>
        <h2 class="chal-overlay-title">准备好了吗？</h2>
        <p class="chal-overlay-desc">
          点击开始后进入 <b>{{ Math.round(challengeSec / 60) }}</b> 分钟倒计时，
          在时限内 AC 即挑战成功，用时进入本题挑战榜。
        </p>
        <ul class="chal-rules">
          <li>编辑器空白起跑，禁止粘贴 / 拖入 / 选中文本</li>
          <li>撤销与重做不受限，自测可用且计时照走</li>
          <li>可反复挑战，榜单取每人最短用时</li>
        </ul>
        <p class="chal-last" v-if="lastFailMsg">{{ lastFailMsg }}</p>
        <el-button type="primary" size="large" class="chal-begin" @click="beginChallenge">
          ⚡ 开始挑战
        </el-button>
        <el-button text @click="backToProblem">先不做，返回题目</el-button>
      </div>
      <div v-else class="chal-overlay-loading">加载中…</div>
    </div>

    <!-- 3-2-1-GO 倒数 -->
    <div class="chal-count" v-if="phase === 'countdown'">
      <span :key="countdownNum" class="chal-count-num">{{
        countdownNum > 0 ? countdownNum : 'GO!'
      }}</span>
    </div>

    <!-- 成功 / 失败全屏横幅 -->
    <div class="chal-overlay" v-if="phase === 'success'">
      <div class="chal-overlay-card success">
        <div class="chal-result-emoji">🎉</div>
        <h2 class="chal-overlay-title">挑战成功！</h2>
        <p class="chal-overlay-desc">
          用时 <b>{{ usedText }}</b> 已计入本题挑战榜
        </p>
        <el-button type="primary" @click="backToProblem">查看挑战榜</el-button>
      </div>
    </div>
    <div class="chal-overlay" v-if="phase === 'fail'">
      <div class="chal-overlay-card fail">
        <div class="chal-result-emoji">💪</div>
        <h2 class="chal-overlay-title">本次挑战未完成</h2>
        <p class="chal-overlay-desc">{{ lastFailMsg }}</p>
        <p class="chal-overlay-desc sub">别灰心，欲速则不达——理清思路，再战一次！</p>
        <el-button type="primary" @click="backToProblem">返回题目</el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chal-page {
  position: fixed;
  inset: 0;
  z-index: 50;
  display: flex;
  flex-direction: column;
  background: var(--soj-canvas, #f6f8fb);
}

.chal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  height: 60px;
  padding: 0 20px;
  background: #fff;
  border-bottom: 1px solid var(--soj-line, #e6eaf2);
  flex: none;
}
.chal-head-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.chal-brand {
  padding: 3px 12px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--soj-blue, #007bff) 12%, #fff);
  color: var(--soj-blue, #007bff);
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}
.chal-title {
  margin: 0;
  font-size: 18px;
  font-weight: 800;
  color: var(--soj-ink, #0b1f45);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chal-limit {
  font-size: 13px;
  color: var(--soj-ink-3, #64748b);
  white-space: nowrap;
}
.chal-head-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: none;
}
.chal-lang {
  width: 120px;
}
.chal-timer {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 40px;
  padding: 0 16px;
  border-radius: 999px;
  background: var(--soj-blue, #007bff);
  color: #fff;
  font-weight: 800;
}
.chal-timer.urgent {
  animation: chal-pulse 1s ease-in-out infinite;
  background: var(--soj-danger, #ff6b6b);
}
@keyframes chal-pulse {
  50% {
    opacity: 0.72;
  }
}
.chal-timer-text {
  font-variant-numeric: tabular-nums;
  font-size: 16px;
}

.chal-body {
  display: flex;
  flex: 1;
  min-height: 0;
}
.chal-statement {
  width: 44%;
  min-width: 380px;
  overflow-y: auto;
  padding: 16px 20px;
  background: #fff;
  border-right: 1px solid var(--soj-line, #e6eaf2);
}
.chal-editor-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 12px 14px 10px;
}
.chal-editor-wrap {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border-radius: 12px;
  overflow: hidden;
}

.chal-selftest {
  flex: none;
  margin-bottom: 10px;
  max-height: 300px;
  overflow: auto;
}

.chal-foot {
  flex: none;
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 10px;
}
.chal-verdict {
  font-size: 13px;
  font-weight: 700;
  color: var(--soj-danger, #ff6b6b);
}
.chal-hint {
  flex: 1;
  text-align: right;
  font-size: 12px;
  color: var(--soj-ink-3, #64748b);
}
.chal-submit {
  min-width: 132px;
  font-weight: 800;
}

/* 全屏遮罩（开场 / 结果） */
.chal-overlay {
  position: absolute;
  inset: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--soj-canvas, #f6f8fb) 86%, #0b1f45 14%);
  backdrop-filter: blur(3px);
}
.chal-overlay-card {
  width: 460px;
  max-width: calc(100vw - 48px);
  padding: 32px 36px;
  border-radius: 20px;
  background: #fff;
  box-shadow: 0 18px 50px rgb(11 31 69 / 14%);
  text-align: center;
}
.chal-kicker {
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0.2em;
  color: var(--soj-blue, #007bff);
}
.chal-overlay-title {
  margin: 8px 0 12px;
  font-size: 26px;
  font-weight: 800;
  color: var(--soj-ink, #0b1f45);
}
.chal-overlay-desc {
  margin: 0 0 6px;
  font-size: 14.5px;
  color: var(--soj-ink-2, #22334d);
}
.chal-overlay-desc.sub {
  color: var(--soj-ink-3, #64748b);
  font-size: 13.5px;
}
.chal-rules {
  margin: 14px 0;
  padding: 12px 16px 12px 30px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--soj-blue, #007bff) 6%, #fff);
  text-align: left;
  font-size: 13px;
  color: var(--soj-ink-2, #22334d);
}
.chal-rules li {
  margin: 3px 0;
}
.chal-last {
  font-size: 13px;
  color: var(--soj-ink-3, #64748b);
}
.chal-begin {
  width: 100%;
  height: 46px;
  font-size: 16px;
  font-weight: 800;
  margin: 14px 0 8px;
}
.chal-result-emoji {
  font-size: 44px;
}

/* 3-2-1-GO */
.chal-count {
  position: absolute;
  inset: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(11 31 69 / 88%);
  pointer-events: none;
}
.chal-count-num {
  font-size: 120px;
  font-weight: 900;
  color: #fff;
  animation: chal-count-in 1s ease-out;
}
@keyframes chal-count-in {
  0% {
    transform: scale(1.6);
    opacity: 0;
  }
  30% {
    transform: scale(1);
    opacity: 1;
  }
}
@media (prefers-reduced-motion: reduce) {
  .chal-count-num {
    animation: none;
  }
  .chal-timer.urgent {
    animation: none;
  }
}
</style>

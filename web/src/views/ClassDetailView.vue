<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { classApi, examApi } from '@/api'
import { createAssignment as apiCreateAssignment, dissolveClass, transferClass, updateClass } from '@/api/classManage'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import PageCrumbs from '@/components/PageCrumbs.vue'
import LazySelectCell from '@/components/LazySelectCell.vue'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import ProblemPicker from '@/components/ProblemPicker.vue'
import CopyFromListButton from '@/components/CopyFromListButton.vue'
import UserSuggestTextarea from '@/components/UserSuggestTextarea.vue'
import SwitchRow from '@/components/SwitchRow.vue'
import { fmtDate, fmtDay, fmtSec } from '@/utils/format'
import { playAchievement, playSSR } from '@/utils/gameSounds'
import { MAX_CONTENT_BYTES, contentBytes } from '@/utils/md'
import { useUserStore } from '@/stores/user'
import { usePolling } from '@/composables/usePolling'
import type { AssignmentListItem, ClassDetail, ClassMember, ClassRaceView, PickedProblem } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const cl = ref<ClassDetail | null>(null)
// 成员身份编辑选项（v0.63 惰性格）：admin=1 / member=2 数值映射（LazySelectCell 数值型）
const memberRoleOptions = [
  { id: 1, label: '班级管理员' },
  { id: 2, label: '普通成员' },
]
const loading = ref(true)
const section = ref<'assignments' | 'exams' | 'pk' | 'members'>('assignments')

// 作业拖拽排序（仅班级管理员）：六点手柄武装 → 卡片可拖 → 悬停即时换位 →
// 落下/收尾一次性保存全序（服务端逐个写 sort = 下标，列表 sort 升序 + 新者在前兜底）
const armedA = ref<number | null>(null)
const dragA = ref<number | null>(null)
function armA(a: AssignmentListItem) {
  if (cl.value?.manageable) armedA.value = a.id
}
function disarmA() {
  armedA.value = null
}
function onADragstart(a: AssignmentListItem, e: DragEvent) {
  if (armedA.value !== a.id || !cl.value?.manageable) {
    e.preventDefault()
    return
  }
  dragA.value = a.id
  e.dataTransfer?.setData('text/plain', String(a.id))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}
function onADragover(a: AssignmentListItem, e: DragEvent) {
  if (dragA.value == null || a.id === dragA.value) return
  const from = assignments.value.findIndex((x) => x.id === dragA.value)
  const to = assignments.value.findIndex((x) => x.id === a.id)
  if (from < 0 || to < 0) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  if (from === to) return
  assignments.value.splice(to, 0, assignments.value.splice(from, 1)[0])
}
async function commitADrag() {
  if (dragA.value == null || !cl.value) return
  const ids = assignments.value.map((x) => x.id)
  dragA.value = null
  disarmA()
  try {
    await classApi.reorderAssignments(cl.value.id, ids)
  } catch {
    await loadAssignments() // 保存失败回滚为服务端现状（拦截器已 toast）
  }
}
const assignments = ref<AssignmentListItem[]>([])
const assignmentsVisible = ref(false)

const joinCode = ref('')
const joining = ref(false)
const showCreateAssignment = ref(false)
const creatingAssignment = ref(false)
const assignmentForm = reactive({ title: '', intro: '', picked: [] as PickedProblem[] })
const importText = ref('')
const importing = ref(false)

// 从题单复制（v0.18.3 拍板 7）：拍平合并进已选（按题号去重），隐藏题提示跳过数量
function mergeFromList(items: { id: number; title: string }[], skippedHidden: number) {
  const have = new Set(assignmentForm.picked.map((p) => p.id))
  let added = 0
  for (const it of items) {
    if (!have.has(it.id)) {
      assignmentForm.picked.push({ id: it.id, title: it.title })
      added++
    }
  }
  ElMessage.success(`已复制 ${added} 题${skippedHidden ? `，跳过隐藏/重复 ${skippedHidden} 题` : ''}`)
}

// 发起考试
const exams = ref<{ id: number; title: string; status: string; start: string; end: string; problem_count: number }[]>([])
const examsVisible = ref(false)
const showExam = ref(false)
const creatingExam = ref(false)
const examForm = ref({
  title: '',
  intro: '',
  range: [] as string[],
  hiddenDays: 0,
  picked: [] as PickedProblem[],
  fileIO: false,
  scoreboardOff: true, // 「关闭成绩表」开关（默认开 = 成绩表关闭）；提交时反转为 scoreboard
  hideDuring: true,
})

async function loadExams() {
  if (!cl.value) return
  try {
    const r = await examApi.listByClass(cl.value.id)
    examsVisible.value = r.visible
    exams.value = r.list
  } catch {
    exams.value = []
  }
}

function openExam(row: { id: number }) {
  router.push(`/exams/${row.id}`)
}

async function createExam() {
  if (!cl.value) return
  if (!examForm.value.title.trim() || examForm.value.range.length !== 2) {
    ElMessage.warning('请填写考试名称与起止时间')
    return
  }
  if (!examForm.value.picked.length) {
    ElMessage.warning('请从选题器选择至少一道题目')
    return
  }
  creatingExam.value = true
  try {
    const r = await examApi.create(cl.value.id, {
      title: examForm.value.title.trim(),
      intro: examForm.value.intro.trim(),
      start: new Date(examForm.value.range[0]).toISOString(),
      end: new Date(examForm.value.range[1]).toISOString(),
      hidden_days: examForm.value.hiddenDays,
      problem_ids: examForm.value.picked.map((p) => p.id),
      file_io: examForm.value.fileIO,
      scoreboard: !examForm.value.scoreboardOff,
      hide_during: examForm.value.hideDuring,
    })
    ElMessage.success('考试创建成功')
    showExam.value = false
    router.push(`/exams/${r.id}`)
  } finally {
    creatingExam.value = false
  }
}

async function load() {
  loading.value = true
  try {
    cl.value = await classApi.detail(Number(route.params.id))
    // v0.37 作业/考试并行拉（原串行两连，高延迟链路省一跳 ~100ms；两者互不依赖）；
    // v0.42 课堂 PK 现状一并拉（running 时经 watch 带起轮询与秒针）
    await Promise.allSettled([loadAssignments(), loadExams(), loadRace()])
  } catch {
    router.replace('/classes')
  } finally {
    loading.value = false
  }
}

async function loadAssignments() {
  if (!cl.value) return
  try {
    const r = await classApi.listAssignments(cl.value.id)
    assignmentsVisible.value = r.visible
    assignments.value = r.list
  } catch {
    assignments.value = []
  }
}

// ---------- 课堂 PK（v0.42）：全班同题手速挑战 ----------
// 教师发起（manageable），学生从挑战链路参赛（StartChallenge 的 Lv3 闸 + staff
// 豁免自动生效）；榜单 3s 轮询，倒计时秒针本地走字、绝对时限锚在服务端下发值上。
const raceState = ref<{ race?: ClassRaceView; last?: ClassRaceView }>({})
const race = computed(() => raceState.value.race)
const lastRace = computed(() => raceState.value.last)
const raceView = computed(() => race.value ?? lastRace.value)
// raceCardVisible 退役（v0.91 tab 化）：PK 卡落独立 tab 恒渲染，空态由卡内 hint 承载

const showRace = ref(false)
const creatingRace = ref(false)
const cancellingRace = ref(false)
const raceForm = reactive({ picked: [] as PickedProblem[], bonus: true, countdown: 0 })

function openRaceDialog() {
  raceForm.picked = []
  raceForm.bonus = true
  raceForm.countdown = 0
  showRace.value = true
}

async function createRace() {
  if (!cl.value) return
  if (raceForm.picked.length !== 1) {
    ElMessage.warning('请选择恰好一道 PK 题目')
    return
  }
  creatingRace.value = true
  try {
    await classApi.createRace(cl.value.id, {
      pid: raceForm.picked[0].id,
      bonus: raceForm.bonus,
      countdown: raceForm.countdown,
    })
    ElMessage.success('课堂 PK 已发起，参赛入口已通知全班')
    showRace.value = false
    raceForm.picked = []
    await loadRace()
  } finally {
    creatingRace.value = false
  }
}

async function cancelRace() {
  if (!cl.value) return
  try {
    await ElMessageBox.confirm('取消后本场 PK 作废（已 AC 记录保留，不再计名次加成）？', '取消课堂 PK', {
      type: 'warning',
      confirmButtonText: '取消 PK',
      cancelButtonText: '继续比赛',
    })
  } catch {
    return
  }
  cancellingRace.value = true
  try {
    await classApi.cancelRace(cl.value.id)
    ElMessage.success('已取消本场课堂 PK')
    await loadRace()
  } finally {
    cancellingRace.value = false
  }
}

function joinRace() {
  const r = race.value
  if (!r) return
  router.push(`/problems/${r.pid}/challenge?race=${r.id}`)
}

// 本地秒针：倒计时锚在服务端下发的剩余值上（客户端时钟偏移不影响走字）
const nowTick = ref(Date.now())
const raceAnchor = ref<{ at: number; remain: number; cd: number } | null>(null)
let tickTimer: ReturnType<typeof setInterval> | null = null
function ensureTick() {
  if (!tickTimer) tickTimer = setInterval(() => (nowTick.value = Date.now()), 1000)
}
function stopTick() {
  if (tickTimer) {
    clearInterval(tickTimer)
    tickTimer = null
  }
}
onUnmounted(stopTick)

const raceRemainMs = computed(() =>
  raceAnchor.value ? Math.max(0, raceAnchor.value.remain - (nowTick.value - raceAnchor.value.at)) : 0,
)
const raceCdMs = computed(() =>
  raceAnchor.value ? Math.max(0, raceAnchor.value.cd - (nowTick.value - raceAnchor.value.at)) : 0,
)

function fmtClock(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}

// 结算庆祝（running→settled 轮询撞见）：我在榜内 → 评级音效 + 卡片闪光
const raceCelebrate = ref(false)
let celebrateTimer: ReturnType<typeof setTimeout> | null = null

async function loadRace() {
  if (!cl.value || (!cl.value.joined && !cl.value.manageable)) return
  try {
    const r = await classApi.race(cl.value.id)
    const prev = raceState.value.race
    raceState.value = r
    // 锚定 nowTick 自身时钟（非 Date.now()）：nowTick 从 setup 起表，比请求返回
    // 时刻早一个时延 δ——锚 Date.now() 会让 nowTick−at 恒为 −δ，cd=0 的场次被
    // 撑出「开赛 00:01」的幽灵倒计时（实弹抓过）
    raceAnchor.value = r.race
      ? { at: nowTick.value, remain: r.race.remaining_ms ?? 0, cd: r.race.countdown_ms ?? 0 }
      : null
    const next = r.race ?? r.last
    if (
      prev?.status === 'running' &&
      next &&
      next.id === prev.id &&
      next.status === 'settled' &&
      next.my_rank > 0 &&
      !raceCelebrate.value
    ) {
      raceCelebrate.value = true
      if (next.my_rank === 1) playSSR()
      else playAchievement()
      if (celebrateTimer) clearTimeout(celebrateTimer)
      celebrateTimer = setTimeout(() => (raceCelebrate.value = false), 4000)
    }
  } catch {
    raceState.value = {}
    raceAnchor.value = null
  }
}

// 实时榜 3s 轮询：race 消失（结算/取消）即自动停轮；秒针随 race 存在起停
const racePoller = usePolling(async () => {
  await loadRace()
  return !!raceState.value.race
}, 3000)
watch(
  () => !!raceState.value.race,
  (on) => {
    if (on) {
      ensureTick()
      racePoller.restart()
    } else {
      stopTick()
    }
  },
)

// 榜行：前 5 名 + 榜外自己的名次行
const raceRows = computed(() => (raceView.value?.board ?? []).slice(0, 5))
const myOutsideRow = computed(() => {
  const r = raceView.value
  if (!r || r.my_rank <= 0 || r.my_rank <= raceRows.value.length) return null
  return { rank: r.my_rank, uid: -1, username: '我', duration_ms: r.my_ms, at: 0 }
})

async function join() {
  if (!cl.value) return
  joining.value = true
  try {
    await classApi.join(cl.value.id, joinCode.value)
    ElMessage.success('加入成功')
    await load()
  } finally {
    joining.value = false
  }
}

const leaving = ref(false)
async function leave() {
  if (!cl.value || leaving.value) return
  leaving.value = true
  try {
    await classApi.leave(cl.value.id)
    ElMessage.success('已退出班级')
    await load()
  } finally {
    leaving.value = false
  }
}

// 转让班级（v0.7.2，建班人 ∨ root；v0.7.4 收进编辑班级弹窗）：
// 从成员中选受让人，二次确认后换主；受让人成为新建班人，原建班人降为班级管理员
const transferring = ref(false)
const transferUid = ref<number | null>(null)
const transferCandidates = computed(() =>
  (cl.value?.members ?? []).filter((m) => !m.is_owner),
)
async function doTransfer() {
  if (!cl.value || !transferUid.value || transferring.value) return
  const target = transferCandidates.value.find((m) => m.uid === transferUid.value)
  try {
    await ElMessageBox.confirm(
      `确定把班级「${cl.value.name}」转让给「${target?.username ?? '#' + transferUid.value}」吗？转让后对方将成为建班人，你将变为班级管理员。`,
      '转让班级',
      { type: 'warning', confirmButtonText: '确认转让', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  transferring.value = true
  try {
    await transferClass(cl.value.id, transferUid.value)
    ElMessage.success('班级已转让')
    showEditClass.value = false
    await load()
  } finally {
    transferring.value = false
  }
}

// 解散班级（v0.7.2，建班人 ∨ root；v0.7.4 收进编辑班级弹窗）：
// 班级仍挂有考试/作业时后端拒绝并提示
const dissolving = ref(false)
async function dissolve() {
  if (!cl.value || dissolving.value) return
  try {
    await ElMessageBox.confirm(
      `确定解散班级「${cl.value.name}」吗？班内全部考试与作业将一并删除、所有成员将被移出，删除后不可恢复！`,
      '⚠️ 危险操作：确认解散',
      { type: 'error', confirmButtonText: '确认解散', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  dissolving.value = true
  try {
    await dissolveClass(cl.value.id)
    ElMessage.success('班级已解散')
    showEditClass.value = false
    router.replace('/classes')
  } finally {
    dissolving.value = false
  }
}

async function regenInvite() {
  if (!cl.value) return
  const r = await classApi.regenInvite(cl.value.id)
  if (cl.value) cl.value.invite_code = r.invite_code
  ElMessage.success('已重置邀请码')
}

function copyInvite() {
  if (cl.value?.invite_code) {
    navigator.clipboard?.writeText(cl.value.invite_code)
    ElMessage.success('邀请码已复制')
  }
}

async function changeMemberRole(row: ClassMember, role: string) {
  if (!cl.value) return
  await classApi.setMemberRole(cl.value.id, row.uid, role)
  ElMessage.success(`已把 ${row.username} 设为${role === 'admin' ? '班级管理员' : '普通成员'}`)
  await load()
}

async function importMembers() {
  if (!cl.value || !importText.value.trim()) {
    ElMessage.warning('请填写用户名列表')
    return
  }
  importing.value = true
  try {
    const r = await classApi.importMembers(cl.value.id, importText.value)
    ElMessage.success(`导入完成：${r.added}/${r.total}`)
    importText.value = ''
    await load()
  } finally {
    importing.value = false
  }
}

async function createAssignment() {
  if (!cl.value) return
  if (!assignmentForm.title.trim()) {
    ElMessage.warning('请填写作业名称')
    return
  }
  creatingAssignment.value = true
  try {
    const r = await apiCreateAssignment(cl.value.id, {
      title: assignmentForm.title.trim(),
      intro: assignmentForm.intro.trim(),
      problem_ids: assignmentForm.picked.map((p) => p.id),
    })
    ElMessage.success('作业创建成功')
    showCreateAssignment.value = false
    assignmentForm.title = ''
    assignmentForm.intro = ''
    assignmentForm.picked = []
    router.push(`/assignments/${r.id}`)
  } finally {
    creatingAssignment.value = false
  }
}

// ---- 编辑班级（v0.7.3 名称/公告；v0.7.4 私密开关 + 转让 + 解散收进同一弹窗） ----
const showEditClass = ref(false)
const savingClass = ref(false)
const editClassForm = reactive({ name: '', intro: '', private: false })
// 可见性/转让/解散仅建班人 ∨ root（与后端 SetClassPrivate/Transfer/Delete 同门槛）
const isClassOwner = computed(
  () => !!cl.value && (cl.value.owner_id === userStore.uid || userStore.role === 'root'),
)

function openEditClass() {
  if (!cl.value) return
  editClassForm.name = cl.value.name
  editClassForm.intro = cl.value.intro || ''
  editClassForm.private = !!cl.value.private
  transferUid.value = null
  showEditClass.value = true
}

async function saveEditClass() {
  if (!cl.value) return
  if (!editClassForm.name.trim()) {
    ElMessage.warning('班级名称不能为空')
    return
  }
  if (contentBytes(editClassForm.intro) > MAX_CONTENT_BYTES) {
    ElMessage.error('班级公告过长（上限 256KB）')
    return
  }
  savingClass.value = true
  try {
    await updateClass(cl.value.id, {
      name: editClassForm.name.trim(),
      intro: editClassForm.intro,
      private: isClassOwner.value ? editClassForm.private : undefined,
    })
    ElMessage.success('班级已保存')
    showEditClass.value = false
    await load()
  } finally {
    savingClass.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <PageShell v-if="cl">
        <el-card>
          <div class="title-row">
            <!-- v0.77 统一面包屑：班级页单段即名称（末段样式承担原 h3 视觉，无链接） -->
            <PageCrumbs :items="[{ label: cl.name }]" />
            <el-tag v-if="cl.private" size="small" type="warning">私密班</el-tag>
            <el-tag v-if="cl.joined" size="small" type="success">已加入</el-tag>
          </div>
          <div v-if="cl.intro.trim()" class="intro"><MdView :html="cl.intro_html ?? ''" /></div>
          <!-- 创建时间在右栏「班级信息」卡展示，名称卡不再重复（v0.7.2 验收反馈） -->
          <div v-if="!cl.joined && !cl.manageable" class="join-bar">
            <el-input
              v-model="joinCode"
              placeholder="输入邀请码加入班级"
              style="width: 240px"
              maxlength="4"
              @keyup.enter="join"
            />
            <el-button type="primary" :loading="joining" @click="join">加入班级</el-button>
          </div>
        </el-card>

        <div class="section-switch">
          <div class="soj-seg">
            <button type="button" :class="{ on: section === 'assignments' }" @click="section = 'assignments'">作业</button>
            <button type="button" :class="{ on: section === 'exams' }" @click="section = 'exams'">考试</button>
            <button type="button" :class="{ on: section === 'pk' }" @click="section = 'pk'">课堂 PK</button>
            <button v-if="cl.members" type="button" :class="{ on: section === 'members' }" @click="section = 'members'">学生</button>
          </div>
        </div>

        <!-- 课堂 PK tab（v0.42 → v0.91 tab 化：原横幅卡常驻作业列表上方，拍板挪进
             tab 行「考试/学生」之间；学生无 PK 也见空态卡，入口不再隐形） -->
        <template v-if="section === 'pk'">
          <div class="card pk-card" :class="{ celebrating: raceCelebrate }">
            <div class="pk-top">
              <span class="pk-badge">⚔️ 课堂 PK</span>
              <span
                class="pk-state"
                :class="race ? (raceCdMs > 0 ? 'soon' : 'live') : lastRace?.status === 'settled' ? 'done' : 'off'"
              >
                {{ race ? (raceCdMs > 0 ? '即将开始' : '进行中') : lastRace?.status === 'settled' ? '已结算' : '已取消' }}
              </span>
              <span v-if="race && raceCdMs > 0" class="pk-clock mono">开赛 {{ fmtClock(raceCdMs) }}</span>
              <span v-else-if="race" class="pk-clock mono">{{ fmtClock(raceRemainMs) }}</span>
              <span class="pk-grow"></span>
              <el-button v-if="cl?.manageable && !race" size="small" type="primary" plain @click="openRaceDialog">
                发起 PK
              </el-button>
              <el-button
                v-if="cl?.manageable && race"
                size="small"
                type="danger"
                plain
                :loading="cancellingRace"
                @click="cancelRace"
              >
                取消
              </el-button>
            </div>

            <template v-if="raceView">
              <div class="pk-body">
                <RouterLink class="pk-title" :to="`/problems/${raceView.pid}`">{{ raceView.ptitle }}</RouterLink>
                <span class="pk-meta">
                  {{ raceView.host_name }} 发起 ·
                  {{ Math.max(1, Math.round((raceView.deadline_at - raceView.started_at) / 60000)) }} 分钟
                  {{ raceView.bonus ? '· 前 3 名掉落加成' : '· 纯娱乐局' }}
                </span>
              </div>
              <div v-if="raceView.board.length" class="pk-board">
                <div
                  v-for="row in raceRows"
                  :key="row.uid"
                  class="pk-row"
                  :class="{ me: row.uid === userStore.uid }"
                >
                  <span class="pk-medal">{{ ['🥇', '🥈', '🥉'][row.rank - 1] ?? `#${row.rank}` }}</span>
                  <span class="pk-name">{{ row.username }}</span>
                  <span class="pk-ms mono">{{ fmtSec(row.duration_ms) }}</span>
                </div>
                <div v-if="myOutsideRow" class="pk-row me">
                  <span class="pk-medal">#{{ myOutsideRow.rank }}</span>
                  <span class="pk-name">我</span>
                  <span class="pk-ms mono">{{ fmtSec(myOutsideRow.duration_ms) }}</span>
                </div>
              </div>
              <div v-else class="pk-hint">
                {{ race && raceCdMs > 0 ? '倒计时结束后开赛，AC 即上榜' : '还没有人 AC——第一个交的就是榜首' }}
              </div>

              <div class="pk-actions">
                <template v-if="race">
                  <el-button v-if="userStore.canChallenge" type="primary" :disabled="raceCdMs > 0" @click="joinRace">
                    {{ raceCdMs > 0 ? '即将开始…' : raceView.my_rank > 0 ? `继续挑战（当前 #${raceView.my_rank}）` : '参赛' }}
                  </el-button>
                  <el-tooltip v-else content="Lv.3 解锁挑战模式后可参赛（教职工豁免）" placement="top">
                    <el-button disabled>Lv.3 解锁后可参赛</el-button>
                  </el-tooltip>
                </template>
                <span v-else-if="raceView.status === 'settled'" class="pk-winner">
                  {{ raceView.winner_name ? `🏆 冠军：${raceView.winner_name}` : '本场无人 AC' }}
                </span>
              </div>
            </template>
            <div v-else class="pk-hint">没有进行中的课堂 PK——老师发起后全班同题限时开战</div>
          </div>
        </template>

        <template v-if="section === 'assignments'">
          <el-alert
            v-if="!assignmentsVisible"
            title="加入班级后才能查看和完成作业"
            type="info"
            :closable="false"
          />
          <template v-else>
            <!-- 作业长条卡直接落在页面上（2026-10-26 拍板：去外层大卡）；
                 左侧日期色块（年月日），管理员可从六点手柄拖动排序（最新默认在最上） -->
            <div v-if="assignments.length" class="strip-list">
              <div
                v-for="a in assignments"
                :key="a.id"
                class="card strip-card a-card"
                :class="{ dragging: dragA === a.id }"
                :draggable="armedA === a.id"
                @dragstart="onADragstart(a, $event)"
                @dragover="onADragover(a, $event)"
                @drop="commitADrag"
                @dragend="commitADrag"
                @click="router.push(`/assignments/${a.id}`)"
              >
                <div class="strip-main">
                  <div class="strip-title-row">
                    <h3>{{ a.title }}</h3>
                  </div>
                  <!-- 草图结构（2026-10-27 拍板）：日期大字｜竖线｜说明/题数纵排 -->
                  <div class="a-body">
                    <span class="a-date">{{ fmtDay(a.created_at) }}</span>
                    <i class="a-divider"></i>
                    <div class="a-info">
                      <p class="strip-intro">{{ a.intro || '暂无说明' }}</p>
                      <div class="strip-meta">{{ a.problem_count }} 题</div>
                    </div>
                    <!-- 完成进度（对齐题单章节条）：满分通过数 / 作业题数 -->
                    <div class="a-pct">
                      <div class="a-pct-bar">
                        <div
                          class="a-pct-in"
                          :style="{ width: `${a.problem_count ? Math.round((a.solved / a.problem_count) * 100) : 0}%` }"
                        ></div>
                      </div>
                      <span class="a-pct-txt mono">{{ a.solved }}/{{ a.problem_count }}</span>
                    </div>
                  </div>
                </div>
                <span
                  v-if="cl.manageable"
                  class="drag-handle"
                  title="拖动排序"
                  @mousedown="armA(a)"
                  @mouseup="disarmA"
                  @click.stop
                ></span>
              </div>
            </div>
            <el-empty v-else description="还没有作业" :image-size="60" />
          </template>
        </template>

        <template v-else-if="section === 'exams'">
          <template v-if="examsVisible">
            <!-- 考试长条卡：色块随状态（进行中绿 / 未开始橙 / 已结束灰），状态标签随标题 -->
            <div v-if="exams.length" class="strip-list">
              <div v-for="e in exams" :key="e.id" class="card strip-card" @click="openExam(e)">
                <span class="strip-av" :class="`st-${e.status}`">{{ (e.title || '?').slice(0, 1) }}</span>
                <div class="strip-main">
                  <div class="strip-title-row">
                    <h3>{{ e.title }}</h3>
                    <el-tag size="small" :type="e.status === 'running' ? 'success' : e.status === 'scheduled' ? 'warning' : 'info'">
                      {{ e.status === 'running' ? '进行中' : e.status === 'scheduled' ? '未开始' : '已结束' }}
                    </el-tag>
                  </div>
                  <div class="strip-meta">
                    {{ new Date(e.start).toLocaleString('zh-CN', { hour12: false }) }} ~
                    {{ new Date(e.end).toLocaleString('zh-CN', { hour12: false }) }} · {{ e.problem_count }} 题
                  </div>
                </div>
              </div>
            </div>
            <el-empty v-else description="还没有考试" :image-size="60" />
          </template>
          <el-alert v-else title="加入班级后才能查看考试" type="info" :closable="false" />
        </template>

        <el-card v-else-if="section === 'members'">
          <el-table stripe :data="cl.members">
              <el-table-column prop="uid" label="ID" width="100" />
              <el-table-column prop="username" label="用户名" min-width="140" />
              <el-table-column label="昵称" min-width="120">
                <template #default="{ row }">
                  <!-- 昵称可空（注册未填）：横杠不兜底用户名——用户名列就在旁边 -->
                  <span v-if="row.nickname">{{ row.nickname }}</span>
                  <span v-else class="dim">—</span>
                </template>
              </el-table-column>
              <el-table-column label="身份" width="200">
                <template #default="{ row }">
                  <el-tag v-if="row.is_owner" size="small" type="warning">建班人</el-tag>
                  <!-- v0.63 惰性单元格编辑：成员全量下发，行内常驻 el-select 在百人班
                       是冻结源——点格才挂（admin=1/member=2 数值映射，选中即改身份） -->
                  <LazySelectCell
                    v-else-if="cl.manageable"
                    :model-value="row.role === 'admin' ? 1 : 2"
                    :options="memberRoleOptions"
                    @update:model-value="(v: number | number[]) => changeMemberRole(row as ClassMember, v === 1 ? 'admin' : 'member')"
                  />
                  <el-tag v-else size="small" :type="row.role === 'admin' ? 'success' : 'info'">
                    {{ row.role === 'admin' ? '班级管理员' : '成员' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="加入时间" width="200">
                <template #default="{ row }">{{ fmtDate(row.joined_at) }}</template>
              </el-table-column>
            </el-table>
            <div v-if="cl.manageable" class="import">
              <UserSuggestTextarea
                v-model="importText"
                class="import-input"
                :rows="2"
                placeholder="输入已注册用户名，空格 / 逗号 / 换行分隔"
              />
              <el-button type="primary" :loading="importing" @click="importMembers">导入成员</el-button>
            </div>
        </el-card>

      <template #aside>
        <!-- 管理卡在信息卡上方（v0.7.2 验收调整）；v0.7.4 可见性/转让/解散收进编辑班级弹窗 -->
        <RailCard v-if="cl.manageable" ov="管理" title="班级管理" class="aside-card">
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" @click="showCreateAssignment = true">布置作业</el-button>
            <el-button style="width: 100%" @click="showExam = true">发起考试</el-button>
            <el-button style="width: 100%" @click="openEditClass">编辑班级</el-button>
            <!-- 邀请码胶囊即复制按钮：体量对齐 entry-actions 动作按钮（2026-10-26 与复制按钮合一） -->
            <button
              type="button"
              class="soj-inv-chip is-clickable"
              title="点击复制邀请码"
              @click="copyInvite"
            >
              复制邀请码：<span class="inv-code">{{ cl.invite_code }}</span>
            </button>
            <el-button style="width: 100%" @click="regenInvite">重置邀请码</el-button>
            <el-button
              v-if="cl.leaveable"
              :disabled="leaving"
              type="danger"
              style="width: 100%"
              @click="leave"
            >
              退出班级
            </el-button>
          </div>
        </RailCard>

        <RailCard v-else-if="cl.joined && cl.leaveable" ov="管理" title="班级管理" class="aside-card">
          <el-button type="danger" style="width: 100%" @click="leave">退出班级</el-button>
        </RailCard>

        <RailCard ov="信息" title="班级信息" class="aside-card">
          <div class="soj-kv">
            <div class="k"><b>{{ cl.members?.length ?? '—' }}</b><span>成员</span></div>
            <div class="k"><b>{{ assignmentsVisible ? assignments.length : '—' }}</b><span>作业</span></div>
            <div class="k"><b>{{ examsVisible ? exams.length : '—' }}</b><span>考试</span></div>
            <div class="k"><b class="kv-date">{{ fmtDay(cl.created_at) }}</b><span>创建</span></div>
          </div>
        </RailCard>

        <RailCard ov="说明" title="可见性" class="aside-card">
          <p class="hint">私密班仅成员可见；作业与考试加入班级后才能查看和完成。</p>
        </RailCard>
      </template>
    </PageShell>

    <el-dialog v-model="showCreateAssignment" title="布置作业" width="960" top="5vh">
      <el-form label-position="top">
        <el-form-item label="作业名称" required>
          <el-input v-model="assignmentForm.title" maxlength="64" placeholder="如：第三章 循环练习" />
        </el-form-item>
        <el-form-item label="作业说明">
          <el-input v-model="assignmentForm.intro" type="textarea" :rows="2" placeholder="选填" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <TipLabel label="作业题目" tip="可先创建后在作业页调整；支持从题单复制，或按题号导入（粘贴一串题号一键加入）" />
          </template>
          <ProblemPicker v-model="assignmentForm.picked">
            <template #actions>
              <CopyFromListButton @merge="mergeFromList" />
            </template>
          </ProblemPicker>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateAssignment = false">取消</el-button>
        <el-button type="primary" :loading="creatingAssignment" @click="createAssignment">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showExam" title="发起班级考试" width="960" top="5vh">
      <el-form label-position="top">
        <el-form-item label="考试名称" required>
          <el-input v-model="examForm.title" maxlength="64" />
        </el-form-item>
        <el-form-item label="考试说明">
          <el-input v-model="examForm.intro" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="起止时间（最长 7 天）" required>
          <el-date-picker
            v-model="examForm.range"
            type="datetimerange"
            start-placeholder="开始"
            end-placeholder="结束"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item>
          <template #label>
            <TipLabel label="赛后隐藏期（天）" tip="0 为不隐藏；隐藏期内榜单与提交记录不公开，期满自动放开" />
          </template>
          <el-input-number v-model="examForm.hiddenDays" :min="0" :max="7" />
        </el-form-item>
        <el-form-item label="考试题目" required>
          <ProblemPicker v-model="examForm.picked" />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="examForm.fileIO"
            label="本场启用文件 IO"
            tip="场内所有题按各题文件名对以文件读写判题，题面下会展示文件 IO 说明；创建后可在考试详情管理卡随时开关，改动只影响之后的提交"
          />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="examForm.scoreboardOff"
            label="关闭成绩表"
            tip="默认开启：参与者看不到成绩表（组织者不受影响）；关闭此开关则本场向参与者开放成绩表"
          />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="examForm.hideDuring"
            label="赛时隐藏成绩"
            tip="隐藏状态/标签/通过率，考中只显示「已提交」；关闭后考中即可查看结果"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showExam = false">取消</el-button>
        <el-button type="primary" :loading="creatingExam" @click="createExam">创建</el-button>
      </template>
    </el-dialog>

    <!-- 发起课堂 PK（v0.42）：全班同题限时手速挑战，时长取题目挑战时长 -->
    <el-dialog v-model="showRace" title="发起课堂 PK" width="960" top="5vh">
      <el-form label-position="top">
        <el-form-item required>
          <template #label>
            <TipLabel label="PK 题目" tip="公开、支持挑战且已有测试数据的题；时长取该题挑战时长" />
          </template>
          <ProblemPicker v-model="raceForm.picked" />
        </el-form-item>
        <el-form-item label="发起时机">
          <el-radio-group v-model="raceForm.countdown">
            <el-radio-button :value="0">立即开始</el-radio-button>
            <el-radio-button :value="30">30 秒后</el-radio-button>
            <el-radio-button :value="60">1 分钟后</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="raceForm.bonus"
            label="前 3 名掉落加成（×2 / ×1.5 / ×1.2）"
            tip="该题首 AC 玉与掉落照常发放，前三名 AC 时刻的即时名次额外加成；关闭则为纯娱乐局"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showRace = false">取消</el-button>
        <el-button type="primary" :loading="creatingRace" @click="createRace">发起 PK</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showEditClass" title="编辑班级" width="960" top="5vh">
      <el-form label-position="top">
        <el-form-item label="班级名称" required>
          <el-input v-model="editClassForm.name" maxlength="64" />
        </el-form-item>
        <el-form-item label="班级公告">
          <MdEditor v-model="editClassForm.intro" height="150px" variant="compact" />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="editClassForm.private"
            :disabled="!isClassOwner"
            label="私密班"
            tip="公开班所有人可见；私密班仅成员与 root/admin 可见；仅建班人与 root 可切换，随保存生效"
          />
        </el-form-item>
      </el-form>

      <!-- 建班人 ∨ root：转让 / 解散收进编辑弹窗（v0.7.4），各自保留二次确认 -->
      <template v-if="isClassOwner">
        <el-divider />
        <div class="danger-row">
          <div class="danger-info">
            <div>转让班级</div>
            <div class="dim">选择一位成员成为新建班人，你将变为班级管理员</div>
          </div>
          <div class="danger-act">
            <el-select
              v-model="transferUid"
              filterable
              size="small"
              placeholder="选择受让人"
              style="width: 180px"
            >
              <el-option
                v-for="m in transferCandidates"
                :key="m.uid"
                :value="m.uid"
                :label="`#${m.uid} ${m.username}`"
              />
            </el-select>
            <el-button type="danger" :loading="transferring" :disabled="!transferUid" @click="doTransfer">
              转让
            </el-button>
          </div>
        </div>
        <div class="danger-row">
          <div class="danger-info">
            <div>解散班级</div>
            <div class="dim">班内全部考试与作业一并删除、所有成员移出，不可恢复</div>
          </div>
          <el-button type="danger" :loading="dissolving" @click="dissolve">解散</el-button>
        </div>
      </template>

      <template #footer>
        <el-button @click="showEditClass = false">取消</el-button>
        <el-button type="primary" :loading="savingClass" @click="saveEditClass">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.title-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  flex-wrap: wrap;
}

/* v3-full 实体大标题：藏蓝 28/850（本地 800，班级名） */
.title-row h3 {
  font-size: var(--soj-fs-entity-detail);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: -0.4px;
  color: var(--soj-navy);
}

.intro {
  margin: var(--soj-sp-10) 0 var(--soj-sp-4);
  color: var(--el-text-color-regular);
}

.join-bar {
  display: flex;
  gap: var(--soj-sp-12);
  margin-top: var(--soj-sp-16);
}

.section-switch {
  margin: var(--soj-sp-16) 0;
}

.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: var(--soj-sp-8) 0 0;
}

.hint.center {
  text-align: center;
}

.kv-date {
  font-size: var(--soj-fs-16);
}

.info-line {
  display: flex;
  justify-content: space-between;
  padding: var(--soj-sp-4) 0;
  color: var(--el-text-color-regular);
}

.tip {
  margin-top: var(--soj-sp-8);
}

/* 编辑班级弹窗危险区：转让/解散行，左说明右动作 */
.danger-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-12);
  padding: var(--soj-sp-10) 0;
}

.danger-row + .danger-row {
  border-top: 1px dashed var(--el-border-color-lighter);
}

.danger-info > div:first-child {
  font-weight: var(--soj-fw-emphasis);
}

.danger-act {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
}

.import {
  margin-top: var(--soj-sp-16);
  display: flex;
  align-items: flex-end;
  gap: var(--soj-sp-12);
}

/* 文本域吃满剩余宽、导入成员钮钉右侧末行——联想弹层向下展开不再盖钮 */
.import-input {
  flex: 1;
}

/* ---- 课堂 PK 卡（v0.42）：与作业长条卡同族白底卡（soj-line + 双层影）---- */
.pk-card {
  padding: var(--soj-sp-12) var(--soj-sp-20);
  margin-bottom: var(--soj-sp-12);
}

.pk-card.celebrating {
  animation: pk-flash 0.9s ease-in-out 4;
}

@keyframes pk-flash {
  0%,
  100% {
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--soj-warning) 0%, transparent);
  }
  40% {
    box-shadow: 0 0 0 4px color-mix(in srgb, var(--soj-warning) 35%, transparent);
  }
}

.pk-top {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  min-width: 0;
}

.pk-badge {
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
  white-space: nowrap;
}

.pk-state {
  padding: var(--soj-sp-2) var(--soj-sp-10);
  border-radius: 999px;
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-emphasis);
  white-space: nowrap;
}

.pk-state.live {
  color: var(--soj-success-deep);
  background: color-mix(in srgb, var(--soj-success) 12%, transparent);
}

.pk-state.soon {
  color: var(--soj-warning-deep);
  background: color-mix(in srgb, var(--soj-warning) 14%, transparent);
}

.pk-state.done {
  color: var(--soj-mut);
  background: color-mix(in srgb, var(--soj-mut) 10%, transparent);
}

.pk-state.off {
  color: var(--soj-mut);
  background: color-mix(in srgb, var(--soj-mut) 6%, transparent);
}

.pk-clock {
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  font-variant-numeric: tabular-nums;
}

.pk-grow {
  flex: 1;
}

.pk-body {
  display: flex;
  align-items: baseline;
  gap: var(--soj-sp-10);
  margin-top: var(--soj-sp-8);
  min-width: 0;
  flex-wrap: wrap;
}

.pk-title {
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  text-decoration: none;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.pk-title:hover {
  color: var(--soj-blue);
}

.pk-meta {
  font-size: var(--soj-fs-12);
  color: var(--soj-mut);
  white-space: nowrap;
}

.pk-board {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-2);
  margin-top: var(--soj-sp-8);
}

.pk-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  padding: var(--soj-sp-4) var(--soj-sp-8);
  border-radius: 8px;
  font-size: var(--soj-fs-13);
  min-width: 0;
}

.pk-row.me {
  background: color-mix(in srgb, var(--soj-blue) 8%, transparent);
}

.pk-medal {
  width: 34px;
  flex: none;
  text-align: center;
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-mut);
}

.pk-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--soj-ink);
}

.pk-ms {
  flex: none;
  font-size: var(--soj-fs-13);
  color: var(--soj-mut);
  font-variant-numeric: tabular-nums;
}

.pk-hint {
  margin-top: var(--soj-sp-8);
  font-size: var(--soj-fs-13);
  color: var(--soj-mut);
}

.pk-actions {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  margin-top: var(--soj-sp-10);
}

.pk-winner {
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-warning-deep);
}

@media (prefers-reduced-motion: reduce) {
  .pk-card.celebrating {
    animation: none;
  }
}

/* ---- 作业/考试长条卡（2026-10-26 拍板：替代表格，对齐题单页横条卡范式）---- */
.strip-list {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-12);
}

.strip-card {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-14);
  padding: var(--soj-sp-16) var(--soj-sp-20);
  cursor: pointer;
  transition:
    transform 0.15s,
    box-shadow 0.15s;
}

/* 作业卡更紧凑（2026-10-28 反馈）：压上下内边距，日期字号同步收小 */
.strip-card.a-card {
  padding: var(--soj-sp-10) var(--soj-sp-20);
}

.strip-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--soj-shadow-card-hover);
}

/* 首字色块：作业按 id 轮换色；考试随状态（进行中绿 / 未开始橙 / 已结束灰） */
.strip-av {
  flex: none;
  width: 44px;
  height: 44px;
  border-radius: 12px;
  display: grid;
  place-items: center;
  font-size: var(--soj-fs-20);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-white);
  background: var(--soj-d2);
}

.strip-av.st-running {
  background: var(--soj-d5);
}

.strip-av.st-scheduled {
  background: var(--soj-d3);
}

/* 作业卡日期行（2026-10-27 拍板草图）：日期大字｜竖线｜说明/题数纵排 */
.a-body {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-16);
  margin-top: var(--soj-sp-4);
}

.a-date {
  flex: none;
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.02em;
  white-space: nowrap;
}

.a-divider {
  flex: none;
  width: 2px;
  align-self: stretch;
  min-height: 30px;
  border-radius: 1px;
  background: var(--el-border-color);
}

.a-info {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-2);
  line-height: 1.35;
}

.a-info .strip-intro {
  margin: 0;
}

/* 完成进度条（样式对齐题单章节条/lpct：渐变胶囊 + mono 计数） */
.a-pct {
  flex: none;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  margin-left: auto;
}

.a-pct-bar {
  width: 150px;
  height: 7px;
  border-radius: 999px;
  background: var(--soj-surface-2);
  overflow: hidden;
}

.a-pct-in {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--soj-blue-hi), var(--soj-teal));
  transition: width 0.3s;
}

.a-pct-txt {
  flex: none;
  min-width: 34px;
  text-align: right;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

/* 六点拖拽手柄（仅班级管理员可见）：grab 光标，hover 主蓝 */
.strip-card > .drag-handle {
  flex: none;
  width: 10px;
  height: 18px;
  cursor: grab;
  background-image: radial-gradient(circle, var(--soj-ink-3) 1.2px, transparent 1.3px);
  background-size: 5px 6px;
}

.strip-card > .drag-handle:hover {
  background-image: radial-gradient(circle, var(--soj-blue) 1.2px, transparent 1.3px);
}

.strip-card.dragging {
  opacity: 0.55;
}

.strip-main {
  flex: 1 1 auto;
  min-width: 0;
}

.strip-title-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  min-width: 0;
}

.strip-title-row h3 {
  margin: 0;
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.strip-intro {
  margin: var(--soj-sp-4) 0 0;
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.strip-meta {
  margin-top: var(--soj-sp-2);
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}
</style>

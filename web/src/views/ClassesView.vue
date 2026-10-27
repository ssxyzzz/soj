<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { classApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import { useUserStore } from '@/stores/user'
import { fmtDay } from '@/utils/format'
import type { ClassCard } from '@/types'

const router = useRouter()
const userStore = useUserStore()

const list = ref<ClassCard[]>([])
const loading = ref(false)
const showCreate = ref(false)
const creating = ref(false)
const createForm = reactive({ name: '', intro: '', private: false })
const joinCode = ref('')
const joining = ref(false)

// 置顶与拖拽排序（2026-10-26 v0.22）：个人视角 marks（POST /me/classes/marks 持久化）——
// 仅已入班的卡可置顶/拖动；置顶优先（置顶组内按 pins 序），置顶卡不可拖出置顶组、
// 非置顶卡不可拖入置顶组，组内互相拖动随意；拖完（drop/dragend）整序落库。
const pins = ref<number[]>([])
const dragId = ref<number | null>(null)
const armedId = ref<number | null>(null)

const isPinned = (id: number) => pins.value.includes(id)

function saveMarks() {
  const order = list.value.filter((c) => c.joined).map((c) => c.id)
  const p = list.value.filter((c) => c.joined && isPinned(c.id)).map((c) => c.id)
  classApi.saveMarks(p, order).catch(() => {})
}

function togglePin(c: ClassCard) {
  const i = pins.value.indexOf(c.id)
  if (i >= 0) pins.value.splice(i, 1)
  else pins.value.unshift(c.id) // 新置顶排置顶组最前
  resort()
  saveMarks()
}

// 置顶组按 pins 序在前；非置顶保持原相对顺序（稳定排序）
function resort() {
  const idx = new Map(pins.value.map((id, i) => [id, i] as const))
  list.value = [...list.value].sort((a, b) => {
    const pa = idx.has(a.id) ? 0 : 1
    const pb = idx.has(b.id) ? 0 : 1
    if (pa !== pb) return pa - pb
    if (pa === 0) return (idx.get(a.id) ?? 0) - (idx.get(b.id) ?? 0)
    return 0
  })
}

// 拖拽仅从六点手柄发起（避免整卡误拖/文本不可选）：手柄 mousedown 挂 armed，卡片才 draggable
function armDrag(c: ClassCard) {
  if (c.joined) armedId.value = c.id
}
function disarm() {
  armedId.value = null
}

function onDragstart(c: ClassCard, e: DragEvent) {
  if (armedId.value !== c.id || !c.joined) {
    e.preventDefault()
    return
  }
  dragId.value = c.id
  e.dataTransfer?.setData('text/plain', String(c.id))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}

function onDragover(c: ClassCard, e: DragEvent) {
  if (dragId.value == null || c.id === dragId.value) return
  const from = list.value.findIndex((x) => x.id === dragId.value)
  const to = list.value.findIndex((x) => x.id === c.id)
  if (from < 0 || to < 0) return
  // 置顶优先：置顶卡拖不进非置顶区、非置顶卡拖不进置顶区（跨组直接忽略）
  if (isPinned(list.value[from].id) !== isPinned(c.id)) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  if (from === to) return
  list.value.splice(to, 0, list.value.splice(from, 1)[0])
}

// drop 与 dragend 都会到这：只提交一次
function commitDrag() {
  if (dragId.value == null) return
  dragId.value = null
  disarm()
  saveMarks()
}

const canCreate = () => userStore.token && userStore.can('class.create')

async function load() {
  loading.value = true
  try {
    list.value = await classApi.list()
    pins.value = list.value.filter((c) => c.pinned).map((c) => c.id)
  } finally {
    loading.value = false
  }
}

async function create() {
  if (!createForm.name.trim()) {
    ElMessage.warning('请输入班级名称')
    return
  }
  creating.value = true
  try {
    const r = await classApi.create(createForm.name.trim(), createForm.intro.trim(), createForm.private)
    ElMessage.success(`班级创建成功，邀请码 ${r.invite_code}`)
    showCreate.value = false
    createForm.name = ''
    createForm.intro = ''
    createForm.private = false
    await load()
    router.push(`/classes/${r.id}`)
  } finally {
    creating.value = false
  }
}

async function joinByCode() {
  if (!joinCode.value.trim()) {
    ElMessage.warning('请输入邀请码')
    return
  }
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: '/classes' } })
    return
  }
  joining.value = true
  try {
    const r = await classApi.joinByCode(joinCode.value.trim())
    ElMessage.success(`已加入「${r.name}」`)
    joinCode.value = ''
    router.push(`/classes/${r.class_id}`)
  } finally {
    joining.value = false
  }
}

const joinTarget = ref<ClassCard | null>(null)
const showJoin = ref(false)
const joinTargetCode = ref('')
const joiningTarget = ref(false)

function openJoin(c: ClassCard) {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: '/classes' } })
    return
  }
  joinTarget.value = c
  joinTargetCode.value = ''
  showJoin.value = true
}

async function joinTargetSubmit() {
  if (!joinTarget.value) return
  joiningTarget.value = true
  try {
    await classApi.join(joinTarget.value.id, joinTargetCode.value)
    ElMessage.success(`已加入「${joinTarget.value.name}」`)
    showJoin.value = false
    await load()
  } finally {
    joiningTarget.value = false
  }
}

onMounted(load)
</script>

<template>
  <PageShell>
      <div class="page-head">
        <div class="kicker">CLASSES</div>
        <h1 class="page-title">班级<small>邀请码入班 · 作业与考试按班组织</small></h1>
      </div>
      <el-empty v-if="!loading && !list.length" description="还没有你能看到的班级，用右侧邀请码加入吧" />
      <el-row v-loading="loading" :gutter="16">
        <el-col v-for="c in list" :key="c.id" :xs="24" :sm="12">
          <el-card
            class="card clcard"
            :class="{ dragging: dragId === c.id }"
            shadow="hover"
            :draggable="armedId === c.id"
            @dragstart="onDragstart(c, $event)"
            @dragover="onDragover(c, $event)"
            @drop="commitDrag"
            @dragend="commitDrag"
            @click="router.push(`/classes/${c.id}`)"
          >
            <div class="name-row">
              <h3 class="name">{{ c.name }}</h3>
              <span v-if="c.private" class="cl-chip lock">🔒 私密</span>
              <!-- 建班人视角显示「xxx的班」；其余成员已加入，非成员给加入入口。
                   私密徽章与身份徽章互相独立（2026-10-25：私密班的建班人不再丢「xxx的班」） -->
              <span v-if="c.owner_id === userStore.uid" class="cl-chip joined">{{ c.owner_name }}的班</span>
              <span v-else-if="c.joined" class="cl-chip joined">✓ 已加入</span>
              <el-button v-else size="small" type="primary" link @click.stop="openJoin(c)">加入</el-button>
              <!-- 置顶钮：锚名称行右缘垂直居中（2026-10-26 拍板：与首行平齐、与六点同列） -->
              <button
                v-if="c.joined"
                type="button"
                class="pin-btn"
                :class="{ on: isPinned(c.id) }"
                :title="isPinned(c.id) ? '取消置顶' : '置顶'"
                @click.stop="togglePin(c)"
              >
                📌
              </button>
            </div>
            <p class="intro">{{ c.intro.trim() || '暂无简介' }}</p>
            <div class="meta">
              <span>{{ c.member_count }} 名成员</span>·<span>{{ c.assignment_count }} 个作业</span>·<span>创建于 {{ fmtDay(c.created_at) }}</span>
              <!-- 六点拖拽手柄（仅已入班）：锚信息行右缘垂直居中，与置顶钮同列；
                   拖拽仅从手柄发起（避免整卡误拖/文本不可选），置顶优先、组内互拖、不跨组 -->
              <span
                v-if="c.joined"
                class="drag-handle"
                title="拖动排序"
                @mousedown="armDrag(c)"
                @mouseup="disarm"
                @click.stop
              ></span>
            </div>
          </el-card>
        </el-col>
      </el-row>

    <template #aside>
      <RailCard v-if="canCreate()" ov="管理" title="班级管理" class="aside-card">
        <el-button type="primary" style="width: 100%" @click="showCreate = true">创建班级</el-button>
        <p class="hint">创建后生成 4 位邀请码，学生凭码入班；公开班级全站可见，私密班级仅成员可见。</p>
      </RailCard>

      <RailCard ov="入口" title="邀请码加入" class="aside-card">
        <div class="entry-actions">
          <el-input
            v-model="joinCode"
            placeholder="请向老师索取邀请码"
            maxlength="4"
            @keyup.enter="joinByCode"
          />
          <el-button :loading="joining" style="width: 100%" @click="joinByCode">加入班级</el-button>
        </div>
      </RailCard>

      <RailCard ov="辅助" title="班级内能做什么" class="aside-card">
        <p class="hint">老师按周发布作业，题目从题库挑选；作业页统一查看完成情况，客观题自动判分，主观题人工评分。</p>
      </RailCard>

      <RailCard ov="说明" title="可见性" class="aside-card">
        <p class="hint">私密班级不出现在全站列表，仅通过邀请码加入；退出班级后历史提交保留。</p>
      </RailCard>
    </template>
    <el-dialog
      v-model="showJoin"
      :title="`加入 ${joinTarget?.name ?? ''}`"
      width="460"
      @closed="joinTarget = null"
    >
      <el-input v-model="joinTargetCode" placeholder="请向老师索取邀请码" maxlength="4" @keyup.enter="joinTargetSubmit" />
      <template #footer>
        <el-button @click="showJoin = false">取消</el-button>
        <el-button type="primary" :loading="joiningTarget" @click="joinTargetSubmit">加入</el-button>
      </template>
    </el-dialog>
  </PageShell>

  <el-dialog v-model="showCreate" title="创建班级" width="560">
    <el-form label-position="top">
      <el-form-item label="班级名称" required>
        <el-input v-model="createForm.name" maxlength="64" placeholder="如：初二 3 班信息组" />
      </el-form-item>
      <el-form-item label="班级简介">
        <el-input v-model="createForm.intro" type="textarea" :rows="3" placeholder="选填" />
      </el-form-item>
      <el-form-item label="可见性">
        <el-radio-group v-model="createForm.private">
          <el-radio-button :value="false">公开（所有人可见）</el-radio-button>
          <el-radio-button :value="true">私密（仅成员可见）</el-radio-button>
        </el-radio-group>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showCreate = false">取消</el-button>
      <el-button type="primary" :loading="creating" @click="create">创建</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.card {
  margin-bottom: 16px;
}

.name-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* 置顶钮与六点手柄（2026-10-26 拍板）：各自锚定所在行右缘垂直居中——置顶对齐
   名称行、六点对齐信息行；同宽盒子（20px）同 right 锚点保证竖对齐同列。
   行右侧预留 26px，长名称/长信息不钻到图标下面 */
.name-row,
.meta {
  position: relative;
  padding-right: 26px;
}

/* 置顶钮（仅已入班）：置顶态全彩，未置顶灰度 */
.pin-btn {
  position: absolute;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 0;
  background: transparent;
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
  opacity: 0.35;
  filter: grayscale(1);
  transition: opacity 0.15s;
  padding: 0;
}

.pin-btn:hover {
  opacity: 0.75;
}

.pin-btn.on {
  opacity: 1;
  filter: none;
}

/* 六点拖拽手柄：grab 光标，hover 主蓝；盒 10×16 右偏 5px 使中心与 20px 置顶钮同列 */
.drag-handle {
  position: absolute;
  right: 5px;
  top: 50%;
  transform: translateY(-50%);
  width: 10px;
  height: 16px;
  cursor: grab;
  background-image: radial-gradient(circle, var(--soj-ink-3) 1.2px, transparent 1.3px);
  background-size: 5px 5.34px;
}

.drag-handle:hover {
  background-image: radial-gradient(circle, var(--soj-blue) 1.2px, transparent 1.3px);
}

:deep(.clcard.dragging) {
  opacity: 0.55;
}

/* 手柄 absolute 锚点（卡片自身 relative；global .card 无定位）；
   全卡可点进班级（2026-10-26 拍板：对齐题单卡交互）——指针 + hover 上浮 */
:deep(.clcard) {
  position: relative;
  cursor: pointer;
}

:deep(.clcard:hover) {
  transform: translateY(-2px);
}

/* v3 clcard：班级名藏蓝 17/800（2026-10-25 用户反馈关键信息偏小上调），hover 主蓝 */
.name {
  margin: 0;
  cursor: pointer;
  color: var(--soj-navy);
  font-size: 17px;
  font-weight: var(--soj-fw-heavy);
}

.name:hover {
  color: var(--soj-blue);
}

/* v3 .joined / .lock 胶囊 */
.cl-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  padding: 1px 9px;
  border-radius: 999px;
  flex: none;
}

.cl-chip.joined {
  color: var(--soj-ac);
  background: var(--soj-ac-bg);
}

.cl-chip.lock {
  color: var(--soj-tle);
  background: var(--soj-tle-bg);
}

.intro {
  color: var(--soj-ink-2);
  margin: 6px 0 8px;
  font-size: var(--soj-fs-14);
  line-height: 1.7;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.meta {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  margin-top: 2px;
}

.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: 10px 0 0;
}

.top-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
  flex-shrink: 0;
}
</style>

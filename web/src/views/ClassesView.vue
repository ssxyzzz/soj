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

const canCreate = () => userStore.token && userStore.can('class.create')

async function load() {
  loading.value = true
  try {
    list.value = await classApi.list()
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
          <el-card class="card clcard" shadow="hover">
            <div class="name-row">
              <h3 class="name" @click="router.push(`/classes/${c.id}`)">{{ c.name }}</h3>
              <span v-if="c.private" class="cl-chip lock">🔒 私密</span>
              <!-- 建班人视角显示「xxx的班」；其余成员已加入，非成员给加入入口。
                   私密徽章与身份徽章互相独立（2026-10-25：私密班的建班人不再丢「xxx的班」） -->
              <span v-if="c.owner_id === userStore.uid" class="cl-chip joined">{{ c.owner_name }}的班</span>
              <span v-else-if="c.joined" class="cl-chip joined">✓ 已加入</span>
              <el-button v-else size="small" type="primary" link @click="openJoin(c)">加入</el-button>
            </div>
            <p class="intro">{{ c.intro || '暂无简介' }}</p>
            <div class="meta">
              <span>{{ c.member_count }} 名成员</span>·<span>{{ c.assignment_count }} 个作业</span>·<span>创建于 {{ fmtDay(c.created_at) }}</span>
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

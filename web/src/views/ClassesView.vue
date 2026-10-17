<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { classApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import { useUserStore } from '@/stores/user'
import { fmtDate } from '@/utils/format'
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
      <el-empty v-if="!loading && !list.length" description="还没有你能看到的班级，用右侧邀请码加入吧" />
      <el-row v-loading="loading" :gutter="16">
        <el-col v-for="c in list" :key="c.id" :xs="24" :sm="12">
          <el-card class="card" shadow="hover">
            <div class="name-row">
              <h4 class="name" @click="router.push(`/classes/${c.id}`)">{{ c.name }}</h4>
              <el-tag v-if="c.private" size="small" type="warning">私密</el-tag>
              <el-tag v-if="c.joined" size="small" type="success">已加入</el-tag>
            </div>
            <p class="intro">{{ c.intro || '暂无简介' }}</p>
            <div class="meta">
              <el-tag size="small" type="info">{{ c.member_count }} 名成员</el-tag>
              <el-tag size="small" type="info">{{ c.assignment_count }} 个作业</el-tag>
              <span class="dim">创建于 {{ fmtDate(c.created_at) }}</span>
              <el-button
                v-if="!c.joined"
                size="small"
                type="primary"
                link
                class="join-btn"
                @click="openJoin(c)"
              >
                加入
              </el-button>
            </div>
          </el-card>
        </el-col>
      </el-row>

    <template #aside>
      <el-card v-if="canCreate()" class="aside-card">
        <template #header><span>管理</span></template>
        <el-button type="primary" style="width: 100%" @click="showCreate = true">创建班级</el-button>
      </el-card>

      <el-card class="aside-card">
        <template #header><span>邀请码加入</span></template>
        <div class="entry-actions">
          <el-input
            v-model="joinCode"
            placeholder="请向老师索取邀请码"
            maxlength="8"
            @keyup.enter="joinByCode"
          />
          <el-button type="primary" :loading="joining" style="width: 100%" @click="joinByCode">加入班级</el-button>
        </div>
      </el-card>
    </template>
    <el-dialog
      v-model="showJoin"
      :title="`加入 ${joinTarget?.name ?? ''}`"
      width="460"
      @closed="joinTarget = null"
    >
      <el-input v-model="joinTargetCode" placeholder="请向老师索取邀请码" maxlength="8" @keyup.enter="joinTargetSubmit" />
      <template #footer>
        <el-button @click="showJoin = false">取消</el-button>
        <el-button type="primary" :loading="joiningTarget" @click="joinTargetSubmit">加入</el-button>
      </template>
    </el-dialog>
  </PageShell>

  <el-dialog v-model="showCreate" title="创建班级" width="500">
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

.name {
  margin: 0;
  font-size: 16px;
  cursor: pointer;
  color: #409eff;
  font-weight: 600;
}

.intro {
  color: #909399;
  font-size: 15px;
  min-height: 36px;
  margin: 6px 0 8px;
}

.meta {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  font-size: 15px;
}

.join-btn {
  margin-left: auto;
}

.aside-card + .aside-card {
  margin-top: 16px;
}

.entry-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
}
</style>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { classApi } from '@/api'
import { useUserStore } from '@/stores/user'
import type { ClassCard } from '@/types'

const router = useRouter()
const userStore = useUserStore()

const list = ref<ClassCard[]>([])
const loading = ref(false)
const showCreate = ref(false)
const creating = ref(false)
const createForm = reactive({ name: '', intro: '' })
const joinTarget = ref<ClassCard | null>(null)
const showJoin = ref(false)
const joinCode = ref('')
const joining = ref(false)

const canCreate = () =>
  userStore.token && (userStore.role === 'admin' || userStore.role === 'root')

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
    const r = await classApi.create(createForm.name.trim(), createForm.intro.trim())
    ElMessage.success(`班级创建成功，邀请码 ${r.invite_code}`)
    showCreate.value = false
    createForm.name = ''
    createForm.intro = ''
    await load()
    router.push(`/classes/${r.id}`)
  } finally {
    creating.value = false
  }
}

function openJoin(c: ClassCard) {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: '/classes' } })
    return
  }
  joinTarget.value = c
  joinCode.value = ''
  showJoin.value = true
}

async function join() {
  if (!joinTarget.value) return
  joining.value = true
  try {
    await classApi.join(joinTarget.value.id, joinCode.value)
    ElMessage.success('加入成功')
    showJoin.value = false
    router.push(`/classes/${joinTarget.value.id}`)
  } finally {
    joining.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <el-card>
      <template #header>
        <div class="head">
          <span>全部班级</span>
          <el-button v-if="canCreate()" type="primary" size="small" @click="showCreate = true">
            创建班级
          </el-button>
        </div>
      </template>
      <el-empty v-if="!loading && !list.length" description="还没有班级，等一位老师来创建吧" />
      <el-row v-loading="loading" :gutter="16">
        <el-col v-for="c in list" :key="c.id" :xs="24" :sm="12" :md="8">
          <el-card class="card" shadow="hover">
            <h4 class="name" @click="router.push(`/classes/${c.id}`)">{{ c.name }}</h4>
            <p class="intro">{{ c.intro || '暂无简介' }}</p>
            <div class="meta">
              <el-tag size="small" type="info">{{ c.member_count }} 名成员</el-tag>
              <el-tag size="small" type="info">{{ c.problem_count }} 道题</el-tag>
              <el-button size="small" type="primary" link @click="openJoin(c)">加入</el-button>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </el-card>

    <el-dialog v-model="showCreate" title="创建班级" width="420">
      <el-form label-position="top">
        <el-form-item label="班级名称" required>
          <el-input v-model="createForm.name" maxlength="64" placeholder="如：初二 3 班信息组" />
        </el-form-item>
        <el-form-item label="班级简介">
          <el-input v-model="createForm.intro" type="textarea" :rows="3" placeholder="选填" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="showJoin"
      :title="`加入 ${joinTarget?.name ?? ''}`"
      width="380"
      @closed="joinTarget = null"
    >
      <el-input v-model="joinCode" placeholder="请向老师索取邀请码" maxlength="8" />
      <template #footer>
        <el-button @click="showJoin = false">取消</el-button>
        <el-button type="primary" :loading="joining" @click="join">加入</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card {
  margin-bottom: 16px;
}

.name {
  margin: 0 0 6px;
  font-size: 16px;
  cursor: pointer;
  color: #409eff;
}

.intro {
  color: #909399;
  font-size: 13px;
  min-height: 36px;
}

.meta {
  display: flex;
  gap: 8px;
  align-items: center;
}
</style>

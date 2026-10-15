<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { contestApi } from '@/api'
import { useUserStore } from '@/stores/user'
import type { ContestListItem } from '@/types'

const router = useRouter()
const userStore = useUserStore()

const list = ref<ContestListItem[]>([])
const loading = ref(false)
const showCreate = ref(false)
const creating = ref(false)
const form = ref({
  title: '',
  intro: '',
  range: [] as string[],
  hiddenDays: 0,
  pids: '' as string,
})

const statusMap: Record<string, { label: string; type: 'success' | 'warning' | 'info' }> = {
  running: { label: '进行中', type: 'success' },
  scheduled: { label: '未开始', type: 'warning' },
  ended: { label: '已结束', type: 'info' },
}

async function load() {
  loading.value = true
  try {
    list.value = await contestApi.list()
  } finally {
    loading.value = false
  }
}

async function create() {
  if (!form.value.title.trim() || form.value.range.length !== 2) {
    ElMessage.warning('请填写比赛名称与起止时间')
    return
  }
  const problemIds = form.value.pids
    .split(/[\s,，、]+/)
    .map((s) => Number(s))
    .filter((n) => Number.isInteger(n) && n > 0)
  if (!problemIds.length) {
    ElMessage.warning('请填写题目 ID（数字，可多个）')
    return
  }
  creating.value = true
  try {
    const r = await contestApi.create({
      title: form.value.title.trim(),
      intro: form.value.intro.trim(),
      start: new Date(form.value.range[0]).toISOString(),
      end: new Date(form.value.range[1]).toISOString(),
      hidden_days: form.value.hiddenDays,
      problem_ids: problemIds,
    })
    ElMessage.success('比赛创建成功')
    showCreate.value = false
    router.push(`/contests/${r.id}`)
  } finally {
    creating.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <el-card>
      <template #header>
        <div class="head">
          <span>比赛</span>
          <el-button
            v-if="userStore.role === 'root'"
            type="primary"
            size="small"
            @click="showCreate = true"
          >
            创建主题库比赛
          </el-button>
        </div>
      </template>
      <el-table v-loading="loading" :data="list" style="cursor: pointer" @row-click="(row: ContestListItem) => router.push(`/contests/${row.id}`)">
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusMap[row.status]?.type ?? 'info'">
              {{ statusMap[row.status]?.label ?? row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="title" label="比赛" min-width="220" />
        <el-table-column label="范围" width="140">
          <template #default>
            <el-tag size="small">主题库 · 全员</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="时间" min-width="280">
          <template #default="{ row }">{{ new Date(row.start).toLocaleString('zh-CN', { hour12: false }) }} ~ {{ new Date(row.end).toLocaleString('zh-CN', { hour12: false }) }}</template>
        </el-table-column>
        <el-table-column prop="problem_count" label="题数" width="80" />
      </el-table>
      <el-empty v-if="!loading && !list.length" description="还没有比赛" />
    </el-card>

    <el-dialog v-model="showCreate" title="创建主题库比赛" width="560">
      <el-form label-position="top">
        <el-form-item label="比赛名称" required>
          <el-input v-model="form.title" maxlength="64" />
        </el-form-item>
        <el-form-item label="简介">
          <el-input v-model="form.intro" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="起止时间（最长 7 天）" required>
          <el-date-picker
            v-model="form.range"
            type="datetimerange"
            start-placeholder="开始"
            end-placeholder="结束"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="赛后隐藏期（天，0 为不隐藏；隐藏期内榜单与提交记录不可见）">
          <el-input-number v-model="form.hiddenDays" :min="0" :max="7" />
        </el-form-item>
        <el-form-item label="题目 ID（从题库取，用空格/逗号分隔）" required>
          <el-input v-model="form.pids" placeholder="如：3 4 5" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
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
</style>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'

import { submissionApi } from '@/api'
import { fmtDate, fmtMem, statusStyle } from '@/utils/format'
import type { Submission } from '@/types'

const router = useRouter()
const list = ref<Submission[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

async function load() {
  loading.value = true
  try {
    const r = await submissionApi.list({ page: page.value, page_size: pageSize })
    list.value = r.list
    total.value = r.total
  } finally {
    loading.value = false
  }
}

function onPageChange(p: number) {
  page.value = p
  load()
}

// 页面打开期间每 5 秒自动刷新一次判题状态
onMounted(() => {
  load()
  timer = setInterval(load, 5000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="page">
    <el-card>
      <template #header>
        <div class="head">
          <span>提交记录（共 {{ total }} 条）</span>
          <el-button :icon="Refresh" size="small" @click="load">刷新</el-button>
        </div>
      </template>
      <el-table v-loading="loading" :data="list">
        <el-table-column prop="id" label="ID" width="90" />
        <el-table-column label="结果" width="130">
          <template #default="{ row }">
            <el-tag :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="题目" min-width="200">
          <template #default="{ row }">
            <el-link type="primary" @click="router.push(`/problems/${row.problem_id}`)">
              #{{ row.problem_id }} {{ row.problem_title }}
            </el-link>
          </template>
        </el-table-column>
        <el-table-column prop="username" label="用户" width="140" />
        <el-table-column prop="language" label="语言" width="100" />
        <el-table-column label="耗时" width="90">
          <template #default="{ row }">{{ row.time_ms }} ms</template>
        </el-table-column>
        <el-table-column label="内存" width="100">
          <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
        </el-table-column>
        <el-table-column label="提交时间" width="180">
          <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          background
          layout="prev, pager, next, total"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          @current-change="onPageChange"
        />
      </div>
    </el-card>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>

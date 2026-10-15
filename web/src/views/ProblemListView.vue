<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { problemApi } from '@/api'
import type { ProblemListItem } from '@/types'

const router = useRouter()
const list = ref<ProblemListItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const r = await problemApi.list(page.value, pageSize)
    list.value = r.list
    total.value = r.total
  } finally {
    loading.value = false
  }
}

function problemRoute(row: { id?: number; locked?: boolean; lock_kind?: string; contest_id?: number; exam_id?: number }) {
  if (row.locked) {
    router.push(row.lock_kind === 'exam' && row.exam_id ? `/exams/${row.exam_id}` : `/contests/${row.contest_id}`)
    return
  }
  router.push(`/problems/${row.id}`)
}

function onPageChange(p: number) {
  page.value = p
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <el-card>
      <template #header>
        <span>题库（共 {{ total }} 题）</span>
      </template>
      <el-table
        v-loading="loading"
        :data="list"
        style="cursor: pointer"
        @row-click="(row: ProblemListItem) => problemRoute(row)"
      >
        <el-table-column prop="id" label="ID" width="100" />
        <el-table-column label="题目" min-width="320">
          <template #default="{ row }">
            {{ row.title }}
            <el-tag v-if="row.hidden" size="small" type="danger" class="ml8">隐藏</el-tag>
            <el-tag v-if="row.locked" size="small" type="warning" class="ml8">
              {{ row.lock_kind === 'exam' ? '考试中' : '比赛中' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="140">
          <template #default="{ row }">
            <el-link v-if="row.locked" type="warning" @click.stop="problemRoute(row)">
              前往{{ row.lock_kind === 'exam' ? '考试' : '比赛' }}
            </el-link>
            <el-link v-else type="primary" @click.stop="router.push(`/problems/${row.id}`)">进入题目</el-link>
          </template>
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
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.ml8 {
  margin-left: 8px;
}
</style>

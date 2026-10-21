<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'

import { submissionApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import SubmissionDetailDialog from '@/components/SubmissionDetailDialog.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, fmtSec, statusDesc } from '@/utils/format'
import type { Submission } from '@/types'

const router = useRouter()
const list = ref<Submission[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

// 状态标签点击 → 该评测记录的代码与测试点明细（本页需「查看全部提交」权限，代码可见）
const showDetail = ref(false)
const detailId = ref<number | null>(null)

function openDetail(id: number) {
  detailId.value = id
  showDetail.value = true
}

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
  <PageShell>
    <el-card>
      <template #header>
        <div class="head">
          <span>提交记录（共 {{ total }} 条）</span>
          <el-button :icon="Refresh" size="small" @click="load">刷新</el-button>
        </div>
      </template>
      <el-table stripe v-loading="loading" :data="list">
        <el-table-column label="状态" width="140">
          <template #default="{ row }">
            <el-tooltip v-if="statusDesc(row.status)" placement="top" effect="light">
              <template #content>
                <div class="status-tip">
                  <div>{{ statusDesc(row.status)!.meaning }}</div>
                  <div v-if="statusDesc(row.status)!.cause">常见原因：{{ statusDesc(row.status)!.cause }}</div>
                </div>
              </template>
              <span class="status-tag clickable" @click.stop="openDetail(row.id)">
                <SubStatus :status="row.status" :score="row.score" />
              </span>
            </el-tooltip>
            <span v-else class="status-tag clickable" @click.stop="openDetail(row.id)">
              <SubStatus :status="row.status" :score="row.score" />
            </span>
          </template>
        </el-table-column>
        <el-table-column label="题目" min-width="200">
          <template #default="{ row }">
            <el-link type="primary" @click="router.push(`/problems/${row.problem_id}`)">
              #{{ row.problem_id }} {{ row.problem_title }}
            </el-link>
            <el-tag v-if="row.contest_id" size="small" type="success" class="ml8">
              {{ row.contest_title || `比赛#${row.contest_id}` }}
            </el-tag>
            <el-tag v-if="row.exam_id" size="small" type="success" class="ml8">
              {{ row.exam_title || `考试#${row.exam_id}` }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="username" label="用户" width="140" />
        <el-table-column prop="language" label="语言" width="100" />
        <el-table-column label="总耗时" width="90">
          <template #default="{ row }">{{ fmtSec(row.time_ms) }}</template>
        </el-table-column>
        <el-table-column label="总内存" width="100">
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

    <SubmissionDetailDialog v-model="showDetail" :submission-id="detailId" />
  </PageShell>
</template>

<style scoped>
/* 状态标签可点击查看代码与测试点明细（悬停说明样式走全局 .status-tag/.status-tip） */
.status-tag.clickable {
  cursor: pointer;
}
</style>

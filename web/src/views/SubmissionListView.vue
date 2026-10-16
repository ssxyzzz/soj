<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'

import { submissionApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
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

const statusDocs: { status: string; meaning: string; cause?: string }[] = [
  { status: 'Pending', meaning: '排队中，等待评测' },
  { status: 'Judging', meaning: '正在评测' },
  { status: 'Accepted', meaning: '通过，拿到满分' },
  { status: 'Wrong Answer', meaning: '答案错误', cause: '输出与期望不一致：检查输出格式、多余空格/换行、边界情况' },
  { status: 'Time Limit Exceeded', meaning: '超出时间限制', cause: '算法复杂度过高或死循环：优化算法，确认循环能终止' },
  { status: 'Memory Limit Exceeded', meaning: '超出内存限制', cause: '数组/递归开太大：减小规模或改用更省内存的写法' },
  { status: 'Runtime Error', meaning: '运行时错误', cause: '常见原因：数组越界、除零、空指针、栈溢出（递归过深）' },
  { status: 'Compile Error', meaning: '编译错误', cause: '代码无法编译：查看提交详情里的编译器报错信息定位行号' },
  { status: 'System Error', meaning: '系统错误', cause: '评测机内部异常（不是你的问题），可重新提交' },
]
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

    <template #aside>
      <el-card class="aside-card">
        <template #header><span>评测状态说明</span></template>
        <div v-for="d in statusDocs" :key="d.status" class="doc-row">
          <div class="doc-head">
            <el-tag size="small" :type="statusStyle(d.status).type">{{ statusStyle(d.status).label }}</el-tag>
          </div>
          <div class="doc-meaning">{{ d.meaning }}</div>
          <div v-if="d.cause" class="doc-cause">常见原因：{{ d.cause }}</div>
        </div>
      </el-card>
    </template>
  </PageShell>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.ml8 {
  margin-left: 8px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.aside-card {
  position: static;
}

.doc-row {
  padding: 8px 4px;
  border-bottom: 1px dashed #ebeef5;
}

.doc-head {
  margin-bottom: 4px;
}

.doc-meaning {
  font-size: 13px;
  color: #606266;
}

.doc-cause {
  font-size: 12px;
  color: #909399;
  margin-top: 2px;
  line-height: 1.5;
}
</style>

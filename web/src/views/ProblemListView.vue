<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { authApi, problemApi, rankApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import { useUserStore } from '@/stores/user'
import type { ProblemListItem } from '@/types'

const router = useRouter()
const userStore = useUserStore()

const list = ref<ProblemListItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const keyword = ref('')

const solvedCount = ref<number | null>(null)
const solvedRank = ref<number | null>(null)

async function load() {
  loading.value = true
  try {
    const r = await problemApi.list(page.value, pageSize, keyword.value.trim() || undefined)
    list.value = r.list
    total.value = r.total
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

function resetSearch() {
  keyword.value = ''
  page.value = 1
  load()
}

async function loadOverview() {
  if (!userStore.token) return
  try {
    const [me, rank] = await Promise.all([authApi.me(), rankApi.top()])
    const row = rank.find((r) => r.user_id === me.id)
    solvedCount.value = row?.solved ?? 0
    solvedRank.value = row?.rank ?? null
  } catch {
    solvedCount.value = null
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

function comingSoon() {
  ElMessage.info('题目创建 / 导入将在 v0.5b 提供，当前请使用命令行导入')
}

onMounted(() => {
  load()
  loadOverview()
})
</script>

<template>
  <PageShell>
    <el-card>
        <div class="search-row">
          <el-input
            v-model="keyword"
            placeholder="搜索题目（标题关键词或题号）"
            clearable
            style="width: 320px"
            @keyup.enter="search"
            @clear="resetSearch"
          />
          <el-button type="primary" @click="search">搜索</el-button>
          <el-button v-if="keyword" @click="resetSearch">重置</el-button>
          <span class="dim">共 {{ total }} 题</span>
        </div>
        <div class="tag-row">
          <span class="dim">标签筛选</span>
          <el-tag size="small" type="info" class="ml8">即将上线</el-tag>
        </div>
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

    <template #aside>
      <el-card v-if="userStore.role === 'admin' || userStore.role === 'root'" class="aside-card">
        <template #header><span>题目管理</span></template>
        <div class="aside-actions">
          <el-button type="primary" plain style="width: 100%" @click="comingSoon">新增题目</el-button>
          <el-button plain style="width: 100%" @click="comingSoon">导入题目</el-button>
        </div>
        <div class="dim tip">网页出题（含测试数据 zip 上传）在 v0.5b 上线</div>
      </el-card>

      <el-card class="aside-card">
        <template #header><span>我的概览</span></template>
        <template v-if="userStore.token">
          <div class="stat-big">{{ solvedCount ?? '—' }}</div>
          <div class="dim">已解决题数{{ solvedRank ? ` · 当前排名第 ${solvedRank}` : '' }}</div>
          <div class="dim tip">按标签 / 难度分组统计将随标签功能上线</div>
        </template>
        <template v-else>
          <div class="dim">登录后查看你的做题概览</div>
          <el-button type="primary" plain style="width: 100%; margin-top: 8px" @click="router.push('/login')">
            去登录
          </el-button>
        </template>
      </el-card>
    </template>
  </PageShell>
</template>

<style scoped>
.search-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.tag-row {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  background: #f5f7fa;
  border-radius: 4px;
  margin-bottom: 12px;
  font-size: 13px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.ml8 {
  margin-left: 8px;
}

.dim {
  color: #909399;
  font-size: 13px;
}

.aside-actions {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.tip {
  margin-top: 8px;
}

.stat-big {
  font-size: 32px;
  font-weight: 700;
  color: #409eff;
  line-height: 1.2;
}
</style>

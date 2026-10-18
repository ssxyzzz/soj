<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { authApi, problemApi, rankApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import TagFilter from '@/components/problem/TagFilter.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { useUserStore } from '@/stores/user'
import type { ProblemListItem } from '@/types'

const router = useRouter()
const userStore = useUserStore()
const tagStore = useTagTreeStore()

const list = ref<ProblemListItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const keyword = ref('')
const tagSel = ref<number[]>([])

const solvedCount = ref<number | null>(null)
const solvedRank = ref<number | null>(null)

async function load() {
  loading.value = true
  try {
    const r = await problemApi.list(page.value, pageSize, keyword.value.trim() || undefined, tagSel.value)
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
  tagSel.value = []
  page.value = 1
  load()
}

function onTagChange(ids: number[]) {
  tagSel.value = ids
  page.value = 1
  load()
}

// 表格标签列：前 3 个直接显示，其余收进 +N 悬浮
function shownTags(row: ProblemListItem): number[] {
  return (row.tags ?? []).slice(0, 3)
}

function restTagNames(row: ProblemListItem): string {
  return (row.tags ?? [])
    .slice(3)
    .map((id) => tagStore.nameOf(id))
    .join('、')
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

// 通过率 = AC 提交 / 总提交（百分比，整数省小数）
function acceptRate(row: { submit_count?: number; accepted_count?: number }): string {
  if (!row.submit_count) return '—'
  const pct = ((row.accepted_count ?? 0) / row.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
}

function onPageChange(p: number) {
  page.value = p
  load()
}

function comingSoon() {
  ElMessage.info('批量题包导入尚未上线；单题可直接点「新增题目」上传 zip，批量仍用命令行导入')
}

onMounted(() => {
  load()
  loadOverview()
  tagStore.ensure() // 标签树异步就绪后筛选区自动出现（v-if 响应 store）
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
          <el-pagination
            class="search-pager"
            background
            layout="total, prev, pager, next"
            :total="total"
            :page-size="pageSize"
            :current-page="page"
            @current-change="onPageChange"
          />
        </div>
        <div v-if="tagStore.nodes.length || tagSel.length" class="tag-row">
          <TagFilter :selected="tagSel" @change="onTagChange" />
        </div>
        <el-table
stripe
          v-loading="loading"
          :data="list"
          style="cursor: pointer"
          @row-click="(row: ProblemListItem) => problemRoute(row)"
        >
          <el-table-column prop="id" label="ID" width="100" />
          <el-table-column label="题目" min-width="320">
            <template #default="{ row }">
              <span class="title">{{ row.title }}</span>
              <el-tag v-if="row.hidden" size="small" type="danger" class="ml8">隐藏</el-tag>
              <el-tag v-if="row.locked" size="small" type="warning" class="ml8">
                {{ row.lock_kind === 'exam' ? '考试中' : '比赛中' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="标签" min-width="170">
            <template #default="{ row }">
              <template v-if="row.tags?.length">
                <el-tag v-for="id in shownTags(row as ProblemListItem)" :key="id" size="small" type="info" class="cell-tag">
                  {{ tagStore.nameOf(id) }}
                </el-tag>
                <el-tooltip v-if="(row.tags?.length ?? 0) > 3" placement="top" effect="light">
                  <template #content>
                    <!-- 标签列插槽与 tooltip 内容插槽的 row 是宽松类型（DefaultRow），收窄回列表项类型 -->
                    <div style="max-width: 320px">{{ restTagNames(row as ProblemListItem) }}</div>
                  </template>
                  <el-tag size="small" type="info">+{{ (row.tags?.length ?? 0) - 3 }}</el-tag>
                </el-tooltip>
              </template>
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
          <el-table-column label="通过率" width="110">
            <template #default="{ row }">
              <el-tooltip
                v-if="row.submit_count"
                :content="`AC ${row.accepted_count ?? 0} / 提交 ${row.submit_count}`"
                placement="top"
              >
                <span>{{ acceptRate(row) }}</span>
              </el-tooltip>
              <span v-else class="dim">—</span>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

    <template #aside>
      <el-card v-if="userStore.can('problem.create')" class="aside-card">
        <template #header><span>题目管理</span></template>
        <div class="entry-actions">
          <el-button type="primary" style="width: 100%" @click="router.push('/problems/create')">新增题目</el-button>
          <el-button style="width: 100%" @click="comingSoon">导入题目</el-button>
        </div>
        <div class="dim tip">网页出题（含测试数据 zip 上传）在 v0.5b 上线</div>
      </el-card>

      <el-card class="aside-card">
        <template #header><span>我的概览</span></template>
        <template v-if="userStore.token">
          <div class="stat-big">{{ solvedCount ?? '—' }}</div>
          <div class="dim">已解决题数{{ solvedRank ? ` · 当前排名第 ${solvedRank}` : '' }}</div>
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
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.search-pager {
  margin-left: auto;
}

.tag-row {
  display: flex;
  align-items: flex-start;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: var(--el-border-radius-base);
  margin-bottom: 12px;
}

.cell-tag {
  margin: 0 6px 4px 0;
}

/* 实体名（题名）主色，整行可点击 */
.title {
  color: var(--el-color-primary);
}

.tip {
  margin-top: 8px;
}

/* 统计大数字（32/600，强调档） */
.stat-big {
  font-size: 32px;
  font-weight: 600;
  color: var(--el-color-primary);
  line-height: 1.2;
}
</style>

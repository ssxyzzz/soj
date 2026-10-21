<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { authApi, problemApi, rankApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import FitTags from '@/components/problem/FitTags.vue'
import SubStatus from '@/components/SubStatus.vue'
import TagFilter from '@/components/problem/TagFilter.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { useUserStore } from '@/stores/user'
import type { ProblemListItem } from '@/types'

const router = useRouter()

// 客观题小类徽章（v0.9）：带「题」字，与详情页题型标签口径一致
function kindBadge(kind?: string) {
  const map: Record<string, string> = { single: '单选题', multiple: '多选题', judge: '判断题', blank: '填空题' }
  return map[kind ?? ''] ?? '客观题'
}

// 题型徽章文案：客观题按小类细分；函数/交互题给固定名；传统题不显示
function typeBadge(row: { type?: string; kind?: string }): string {
  if (row.type === 'objective') return kindBadge(row.kind)
  if (row.type === 'function') return '函数题'
  if (row.type === 'interactive') return '交互题'
  return ''
}
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
          <el-table-column v-if="userStore.token" label="状态" width="150">
            <template #default="{ row }">
              <SubStatus v-if="row.status" :status="row.status" :score="row.my_score" />
              <span v-else class="dim">未做</span>
            </template>
          </el-table-column>
          <el-table-column label="题目" min-width="320">
            <template #default="{ row }">
              <span class="title">{{ row.title }}</span>
              <el-tag v-if="typeBadge(row)" size="small" type="info" class="ml8">
                {{ typeBadge(row) }}
              </el-tag>
              <el-tag v-if="row.hidden" size="small" type="danger" class="ml8">隐藏题</el-tag>
              <!-- 客观题没有测试数据是正常形态，不提示「无数据」 -->
              <el-tag v-if="row.has_cases === false && row.type !== 'objective'" size="small" type="warning" class="ml8">无数据</el-tag>
              <el-tag v-if="row.locked" size="small" type="success" class="ml8">
                {{ row.lock_kind === 'exam' ? '考试中' : '比赛中' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="标签" min-width="170">
            <template #default="{ row }">
              <FitTags v-if="row.tags?.length" :ids="row.tags" />
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
        <div class="dim tip">网页出题（含测试数据 zip 上传）在 v0.5.2 上线</div>
      </el-card>

      <el-card class="aside-card">
        <template #header><span>我的概览</span></template>
        <template v-if="userStore.token">
          <div class="stat-row">
            <div class="stat">
              <div class="stat-big">{{ solvedCount ?? '—' }}</div>
              <div class="dim">已解决题数</div>
            </div>
            <div class="stat">
              <div class="stat-big">{{ solvedRank ?? '—' }}</div>
              <div class="dim">当前排名</div>
            </div>
          </div>
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

/* 实体名（题名）主色，整行可点击 */
.title {
  color: var(--el-color-primary);
}

.tip {
  margin-top: 8px;
}

/* 统计大数字（32/600，强调档） */
/* 两个统计（已解决题数/当前排名）并排一行，数字同规格 */
.stat-row {
  display: flex;
  align-items: flex-end;
  gap: 40px;
}

.stat-big {
  font-size: var(--soj-fs-stat);
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-color-primary);
  line-height: 1.2;
}
</style>

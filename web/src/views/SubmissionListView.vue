<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowDown } from '@element-plus/icons-vue'

import { submissionApi, userApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import SubmissionDetailDialog from '@/components/SubmissionDetailDialog.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtMem, fmtRel, fmtSec, statusDesc, statusMap } from '@/utils/format'
import type { LookupUser, Submission } from '@/types'

const router = useRouter()
const list = ref<Submission[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

// v3 筛选 chips（后端 v0.19 扩参）：关键词 / 状态 / 语言 / 选手 / 仅看自己
const kw = ref('')
const statusFilter = ref('')
const langFilter = ref('')
const mineOnly = ref(false)
const statusOpen = ref(false)
const langOpen = ref(false)

// 选手筛选（2026-10-25 用户需求「选定账号查找」）：远程模糊搜账号，选中按 user_id 过滤
const userFilter = ref<number | undefined>()
const userOptions = ref<LookupUser[]>([])
const userSearching = ref(false)

async function searchUsers(q: string) {
  if (!q.trim()) return // 保留已有候选，防已选中项失参后显示成 uid 数字
  userSearching.value = true
  try {
    userOptions.value = await userApi.lookup(q.trim())
  } catch {
    userOptions.value = []
  } finally {
    userSearching.value = false
  }
}

// 与「仅看自己」互斥：语义都是限定提交者，后端 mine 会覆盖 user_id
function onUserFilterChange() {
  if (userFilter.value) mineOnly.value = false
  page.value = 1
  load()
}

// 状态候选（判题终态 + 队列态，取全站状态表）
const statusOptions = computed(() =>
  Object.entries(statusMap).map(([value, v]) => ({ value, label: (v as { label: string }).label })),
)

// 语言候选：从当前页数据归纳（后端无语言字典接口；常见语言固定补齐）
const langOptions = computed(() => {
  const seen = new Set(list.value.map((s) => s.language))
  for (const l of ['cpp', 'c', 'python3']) seen.add(l)
  return [...seen]
})

const activeCount = computed(
  () =>
    (kw.value ? 1 : 0) +
    (statusFilter.value ? 1 : 0) +
    (langFilter.value ? 1 : 0) +
    (userFilter.value ? 1 : 0) +
    (mineOnly.value ? 1 : 0),
)

function resetFilters() {
  kw.value = ''
  statusFilter.value = ''
  langFilter.value = ''
  userFilter.value = undefined
  mineOnly.value = false
  page.value = 1
  load()
}

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
    const r = await submissionApi.list({
      page: page.value,
      page_size: pageSize,
      ...(kw.value.trim() ? { kw: kw.value.trim() } : {}),
      ...(statusFilter.value ? { status: statusFilter.value } : {}),
      ...(langFilter.value ? { language: langFilter.value } : {}),
      ...(userFilter.value ? { user_id: userFilter.value } : {}),
      ...(mineOnly.value ? { mine: true } : {}),
    })
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

function pickStatus(v: string) {
  statusFilter.value = statusFilter.value === v ? '' : v
  statusOpen.value = false
  page.value = 1
  load()
}

function pickLang(v: string) {
  langFilter.value = langFilter.value === v ? '' : v
  langOpen.value = false
  page.value = 1
  load()
}

function toggleMine() {
  mineOnly.value = !mineOnly.value
  // 与选手筛选互斥（语义都是限定提交者，后端 mine 覆盖 user_id）
  if (mineOnly.value) userFilter.value = undefined
  page.value = 1
  load()
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
    <div class="page-head">
      <div class="kicker">SUBMISSIONS</div>
      <h1 class="page-title">
        提交记录<small>全站 {{ total }} 条 · 点击状态徽章查看逐点评测详情 · 每 5 秒自动刷新</small>
      </h1>
    </div>

    <!-- v3 filterbar：搜索胶囊 + 状态/语言/仅看自己 chips -->
    <div class="filterbar">
      <label class="soj-input search-pill" @keyup.enter="search">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
        <input v-model="kw" placeholder="按题目或提交号搜索" />
      </label>
      <button class="soj-sel" :class="{ on: statusOpen || statusFilter }" @click="statusOpen = !statusOpen">
        状态<template v-if="statusFilter"> · {{ statusMap[statusFilter]?.label ?? statusFilter }}</template>
        <el-icon class="sel-arrow" :class="{ open: statusOpen }"><ArrowDown /></el-icon>
      </button>
      <button class="soj-sel" :class="{ on: langOpen || langFilter }" @click="langOpen = !langOpen">
        语言<template v-if="langFilter"> · {{ langFilter }}</template>
        <el-icon class="sel-arrow" :class="{ open: langOpen }"><ArrowDown /></el-icon>
      </button>
      <!-- 选手筛选（2026-10-25 用户需求「选定账号查找」）：远程模糊搜账号 -->
      <el-select
        v-model="userFilter"
        class="user-sel"
        filterable
        remote
        clearable
        reserve-keyword
        placeholder="选手"
        :remote-method="searchUsers"
        :loading="userSearching"
        @change="onUserFilterChange"
      >
        <el-option v-for="u in userOptions" :key="u.id" :value="u.id" :label="u.nickname || u.username" />
      </el-select>
      <button class="soj-sel" :class="{ on: mineOnly }" @click="toggleMine">仅看自己</button>
      <el-button v-if="activeCount" link type="primary" @click="resetFilters">清空（{{ activeCount }}）</el-button>
    </div>

    <!-- 状态/语言候选行（chip 展开） -->
    <div v-if="statusOpen" class="panel-row">
      <button
        v-for="o in statusOptions"
        :key="o.value"
        class="soj-sel opt"
        :class="{ on: statusFilter === o.value }"
        @click="pickStatus(o.value)"
      >
        {{ o.label }}
      </button>
    </div>
    <div v-if="langOpen" class="panel-row">
      <button
        v-for="l in langOptions"
        :key="l"
        class="soj-sel opt"
        :class="{ on: langFilter === l }"
        @click="pickLang(l)"
      >
        {{ l }}
      </button>
    </div>

    <el-card class="tbl-card">
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
        <el-table-column label="提交号" width="92">
          <template #default="{ row }">
            <span class="tlink mono" @click="openDetail(row.id)">#{{ row.id }}</span>
          </template>
        </el-table-column>
        <el-table-column label="题目" min-width="220">
          <template #default="{ row }">
            <span class="tlink" @click="router.push(`/problems/${row.problem_id}`)">
              P{{ row.problem_id }} {{ row.problem_title }}
            </span>
            <el-tag v-if="row.contest_id" size="small" class="ml8">{{ row.contest_title || `比赛#${row.contest_id}` }}</el-tag>
            <el-tag v-else-if="row.exam_id" size="small" class="ml8">{{ row.exam_title || `考试#${row.exam_id}` }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="username" label="选手" width="120">
          <template #default="{ row }">
            <!-- v0.17 入口：点用户名进公开主页（tlink 与提交号/题目列同款链接形制） -->
            <router-link class="tlink" :to="`/users/${row.user_id}`">{{ row.username }}</router-link>
          </template>
        </el-table-column>
        <el-table-column prop="language" label="语言" width="90" />
        <el-table-column label="耗时" width="92">
          <template #default="{ row }">
            <span class="mono">{{ row.time_ms ? fmtSec(row.time_ms) : '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="内存" width="96">
          <template #default="{ row }">
            <span class="mono">{{ row.memory_kb ? fmtMem(row.memory_kb) : '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="时间" width="110">
          <template #default="{ row }">
            <!-- 相对时间含中文（N 分钟前），不挂 mono——mono 栈无中文字形会落到衬线体（2026-10-25 修复⑫） -->
            <span class="dim">{{ fmtRel(row.created_at) }}</span>
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

    <div class="sub-footer">
      <span>SOJ · 在线判题系统</span>
      <span>点击提交号查看逐点评测详情</span>
    </div>

    <SubmissionDetailDialog v-model="showDetail" :submission-id="detailId" />
  </PageShell>
</template>

<style scoped>
/* v3 filterbar：搜索胶囊 250 定宽 + chips */
.filterbar {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

.search-pill {
  width: 250px;
}

.sel-arrow {
  font-size: var(--soj-fs-12);
  transition: transform 0.2s;
}

.sel-arrow.open {
  transform: rotate(180deg);
}

/* chip 展开的候选行 */
.panel-row {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 16px;
  padding: 12px 14px;
  background: var(--el-fill-color-light);
  border-radius: 14px;
}

.soj-sel.opt {
  height: 30px;
  padding: 0 13px;
}

/* 选手筛选下拉：与 chips 同高的紧凑档 */
.user-sel {
  width: 150px;
  --el-component-size: 30px;
}

.status-tag.clickable {
  cursor: pointer;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
  padding-bottom: 2px;
}

.sub-footer {
  display: flex;
  justify-content: center;
  gap: 18px;
  flex-wrap: wrap;
  margin-top: 22px;
  padding-top: 22px;
  border-top: 1px solid var(--soj-line);
  font-size: 12.5px;
  color: var(--soj-ink-3);
}
</style>

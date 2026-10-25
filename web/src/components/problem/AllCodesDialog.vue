<script setup lang="ts">
import { fmtSec } from '@/utils/format'
// 所有代码弹窗（v0.5.4 ② 拆分自 ProblemDetailView，2026-10-19 改名并加默认排序）：
// 排序三档=最近提交（默认）/ 时间优先 / 空间优先，最多 10 条，可切语言；
// 点行经 SubmissionDetailDialog 看代码与测试点明细。
import { computed, ref, watch } from 'vue'

import { problemApi, userApi } from '@/api'
import SubmissionDetailDialog from '@/components/SubmissionDetailDialog.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, statusDesc } from '@/utils/format'
import { languageOptions } from '@/utils/code'
import type { LookupUser, Submission } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problemId: number
  problemTitle: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const PAGE_SIZE = 10
const others = ref<Submission[]>([])
const othersTotal = ref(0)
const othersPage = ref(1)
const othersSort = ref<'recent' | 'time' | 'mem'>('recent')
const othersLang = ref('')
// 提交者筛选（2026-10-25 用户需求「选定账号查找」）：远程模糊搜账号，选中按 user_id 过滤
const othersUser = ref<number | undefined>()
const userOptions = ref<LookupUser[]>([])
const userSearching = ref(false)
const showOthersDetail = ref(false)
const othersDetail = ref<Submission | null>(null)

async function searchUsers(q: string) {
  if (!q.trim()) return // 保留候选，防已选中项显示成 uid 数字
  userSearching.value = true
  try {
    userOptions.value = await userApi.lookup(q.trim())
  } catch {
    userOptions.value = []
  } finally {
    userSearching.value = false
  }
}

// 最后一页不足 PAGE_SIZE 时补空白行，让表格保持整页高度
const displayOthers = computed(() => {
  const fill = Math.max(0, PAGE_SIZE - others.value.length)
  return [...others.value, ...Array.from({ length: fill }, () => ({}) as Submission)]
})

async function loadOthers(p = 1) {
  try {
    const r = await problemApi.submissions(
      props.problemId,
      p,
      PAGE_SIZE,
      othersSort.value,
      othersLang.value || undefined,
      othersUser.value || undefined,
    )
    others.value = r.list
    othersTotal.value = r.total
    othersPage.value = p
  } catch {
    others.value = []
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      othersUser.value = undefined // 换题打开重置提交者筛选
      loadOthers()
    }
  },
)

function openOthersDetail(s: Submission) {
  if (!s.id) return // 补位的空白行不可点
  othersDetail.value = s
  showOthersDetail.value = true
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="所有代码"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="dlg-toolbar">
      <el-radio-group v-model="othersSort" @change="loadOthers(1)">
        <el-radio-button value="recent">最近提交</el-radio-button>
        <el-radio-button value="time">时间优先</el-radio-button>
        <el-radio-button value="mem">空间优先</el-radio-button>
      </el-radio-group>
      <el-select
        v-model="othersLang"
        clearable
        placeholder="全部语言"
        style="width: 160px"
        @change="loadOthers(1)"
      >
        <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
      </el-select>
      <!-- 提交者筛选（2026-10-25 用户需求「选定账号查找」）：远程模糊搜账号 -->
      <el-select
        v-model="othersUser"
        class="user-sel"
        filterable
        remote
        clearable
        reserve-keyword
        placeholder="全部提交者"
        :remote-method="searchUsers"
        :loading="userSearching"
        @change="loadOthers(1)"
      >
        <el-option v-for="u in userOptions" :key="u.id" :value="u.id" :label="u.nickname || u.username" />
      </el-select>
      <span class="dim">共 {{ othersTotal }} 份</span>
    </div>
    <el-table
stripe
      :data="displayOthers"
      size="small"
      border
      style="cursor: pointer"
      @row-click="(row: Submission) => openOthersDetail(row as Submission)"
    >
      <el-table-column label="状态" width="140">
        <template #default="{ row }">
          <template v-if="row.id">
            <el-tooltip v-if="statusDesc(row.status)" placement="top" effect="light">
              <template #content>
                <div class="status-tip">
                  <div>{{ statusDesc(row.status)!.meaning }}</div>
                  <div v-if="statusDesc(row.status)!.cause">常见原因：{{ statusDesc(row.status)!.cause }}</div>
                </div>
              </template>
              <SubStatus :status="row.status" :score="row.score" />
            </el-tooltip>
            <SubStatus v-else :status="row.status" :score="row.score" />
          </template>
        </template>
      </el-table-column>
      <el-table-column prop="username" label="提交者" min-width="110" />
      <el-table-column prop="language" label="语言" width="100" />
      <el-table-column label="总耗时" width="100">
        <template #default="{ row }">
          <template v-if="row.id">{{ fmtSec(row.time_ms) }}</template>
        </template>
      </el-table-column>
      <el-table-column label="总内存" width="110">
        <template #default="{ row }">
          <template v-if="row.id">{{ fmtMem(row.memory_kb) }}</template>
        </template>
      </el-table-column>
      <el-table-column label="提交时间">
        <template #default="{ row }">
          <template v-if="row.id">{{ fmtDate(row.created_at) }}</template>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!others.length" description="还没有人提交过" :image-size="48" />
    <div v-if="othersTotal > 0" class="side-pager">
      <el-pagination
        background
        layout="prev, pager, next"
        :total="othersTotal"
        :page-size="PAGE_SIZE"
        :current-page="othersPage"
        @current-change="(p: number) => loadOthers(p)"
      />
    </div>

    <SubmissionDetailDialog
      v-model="showOthersDetail"
      :submission="othersDetail"
      :title="othersDetail ? `所有代码 · ${othersDetail.username} · ${othersDetail.problem_title || problemTitle}` : ''"
    />
  </el-dialog>
</template>

<style scoped>
.dlg-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

/* 提交者筛选下拉 */
.user-sel {
  width: 170px;
}
</style>

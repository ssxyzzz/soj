<script setup lang="ts">
// 他人代码弹窗（v0.5e ② 拆分自 ProblemDetailView）：耗时升序为默认（并列按内存），
// 最多 10 条，可切排序与语言；点行经 SubmissionDetailDialog 看代码与测试点明细。
import { ref, watch } from 'vue'

import { problemApi } from '@/api'
import SubmissionDetailDialog from '@/components/SubmissionDetailDialog.vue'
import { fmtDate, fmtMem, statusDesc, statusStyle } from '@/utils/format'
import { languageOptions } from '@/utils/code'
import type { Submission } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problemId: number
  problemTitle: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const PAGE_SIZE = 10
const others = ref<Submission[]>([])
const othersTotal = ref(0)
const othersSort = ref<'time' | 'mem'>('time')
const othersLang = ref('')
const showOthersDetail = ref(false)
const othersDetail = ref<Submission | null>(null)

async function loadOthers() {
  try {
    const r = await problemApi.submissions(
      props.problemId,
      1,
      PAGE_SIZE,
      othersSort.value,
      othersLang.value || undefined,
    )
    others.value = r.list
    othersTotal.value = r.total
  } catch {
    others.value = []
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) loadOthers()
  },
)

function openOthersDetail(s: Submission) {
  othersDetail.value = s
  showOthersDetail.value = true
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="他人代码"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="dlg-toolbar">
      <el-radio-group v-model="othersSort" @change="loadOthers">
        <el-radio-button value="time">时间优先</el-radio-button>
        <el-radio-button value="mem">空间优先</el-radio-button>
      </el-radio-group>
      <el-select
        v-model="othersLang"
        clearable
        placeholder="全部语言"
        style="width: 160px"
        @change="loadOthers"
      >
        <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
      </el-select>
      <span class="dim">共 {{ othersTotal }} 份，显示前 {{ PAGE_SIZE }} 份</span>
    </div>
    <el-table
      :data="others"
      size="small"
      border
      style="cursor: pointer"
      @row-click="(row: Submission) => openOthersDetail(row as Submission)"
    >
      <el-table-column label="状态" width="170">
        <template #default="{ row }">
          <el-tooltip v-if="statusDesc(row.status)" placement="top" effect="light">
            <template #content>
              <div class="status-tip">
                <div>{{ statusDesc(row.status)!.meaning }}</div>
                <div v-if="statusDesc(row.status)!.cause">常见原因：{{ statusDesc(row.status)!.cause }}</div>
              </div>
            </template>
            <el-tag size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
          </el-tooltip>
          <el-tag v-else size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="username" label="提交者" min-width="110" />
      <el-table-column prop="language" label="语言" width="100" />
      <el-table-column label="耗时" width="100">
        <template #default="{ row }">{{ row.time_ms }} ms</template>
      </el-table-column>
      <el-table-column label="内存" width="110">
        <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
      </el-table-column>
      <el-table-column prop="score" label="得分" width="80" />
      <el-table-column label="提交时间">
        <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!others.length" description="还没有人提交过" :image-size="48" />

    <SubmissionDetailDialog
      v-model="showOthersDetail"
      :submission="othersDetail"
      :title="othersDetail ? `他人代码 · ${othersDetail.username} · ${othersDetail.problem_title || problemTitle}` : ''"
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
</style>

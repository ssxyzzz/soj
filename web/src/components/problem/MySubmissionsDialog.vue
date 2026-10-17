<script setup lang="ts">
// 我的提交弹窗（v0.5e ② 拆分自 ProblemDetailView）：列表点行 → 同弹窗切详情
// （代码/耗时/内存/错误信息/测试点明细）。父层提交后经 refresh() 刷新列表。
import { ref, watch } from 'vue'

import { problemApi } from '@/api'
import { fmtDate, fmtMem, statusDesc, statusStyle } from '@/utils/format'
import type { Submission } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problemId: number
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const PAGE_SIZE = 10
const mySubs = ref<Submission[]>([])
const myTotal = ref(0)
const myPage = ref(1)
const mineView = ref<'list' | 'detail'>('list')
const mineDetail = ref<Submission | null>(null)

async function loadMine(p = 1) {
  try {
    const r = await problemApi.mySubmissions(props.problemId, p, PAGE_SIZE)
    mySubs.value = r.list
    myTotal.value = r.total
    myPage.value = p
  } catch {
    mySubs.value = []
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      mineView.value = 'list'
      mineDetail.value = null
      loadMine(1)
    }
  },
)

function openMineDetail(s: Submission) {
  mineDetail.value = s
  mineView.value = 'detail'
}

defineExpose({ refresh: loadMine })
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="我的提交"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
    @closed="mineView = 'list'"
  >
    <template v-if="mineView === 'list'">
      <el-table :data="mySubs" highlight-current-row @row-click="openMineDetail">
        <el-table-column label="状态" width="170">
          <template #default="{ row }">
            <el-tooltip v-if="statusDesc(row.status)" placement="top" effect="light">
              <template #content>
                <div class="status-tip">
                  <div>{{ statusDesc(row.status)!.meaning }}</div>
                  <div v-if="statusDesc(row.status)!.cause">常见原因：{{ statusDesc(row.status)!.cause }}</div>
                </div>
              </template>
              <el-tag size="small" :type="statusStyle(row.status).type" class="status-tag">{{ statusStyle(row.status).label }}</el-tag>
            </el-tooltip>
            <el-tag v-else size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="score" label="得分" width="80" />
        <el-table-column prop="language" label="语言" width="100" />
        <el-table-column label="耗时" width="100">
          <template #default="{ row }">{{ row.time_ms }} ms</template>
        </el-table-column>
        <el-table-column label="内存" width="110">
          <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
        </el-table-column>
        <el-table-column label="提交时间">
          <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!mySubs.length" description="还没有提交过" :image-size="48" />
      <div v-if="myTotal > PAGE_SIZE" class="side-pager">
        <el-pagination
          background
          layout="prev, pager, next"
          :total="myTotal"
          :page-size="PAGE_SIZE"
          :current-page="myPage"
          @current-change="(p: number) => loadMine(p)"
        />
      </div>
    </template>
    <template v-else-if="mineDetail">
      <el-button class="mb8" @click="mineView = 'list'">← 返回列表</el-button>
      <el-descriptions :column="3" border class="detail-desc">
        <el-descriptions-item label="状态">
          <el-tooltip v-if="statusDesc(mineDetail.status)" placement="top" effect="light">
            <template #content>
              <div class="status-tip">
                <div>{{ statusDesc(mineDetail.status)!.meaning }}</div>
                <div v-if="statusDesc(mineDetail.status)!.cause">常见原因：{{ statusDesc(mineDetail.status)!.cause }}</div>
              </div>
            </template>
            <el-tag size="small" :type="statusStyle(mineDetail.status).type" class="status-tag">{{ statusStyle(mineDetail.status).label }}</el-tag>
          </el-tooltip>
          <el-tag v-else size="small" :type="statusStyle(mineDetail.status).type">{{ statusStyle(mineDetail.status).label }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="得分">{{ mineDetail.score }}</el-descriptions-item>
        <el-descriptions-item label="语言">{{ mineDetail.language }}</el-descriptions-item>
        <el-descriptions-item label="耗时">{{ mineDetail.time_ms }} ms</el-descriptions-item>
        <el-descriptions-item label="内存">{{ fmtMem(mineDetail.memory_kb) }}</el-descriptions-item>
        <el-descriptions-item label="提交时间">{{ fmtDate(mineDetail.created_at) }}</el-descriptions-item>
      </el-descriptions>
      <template v-if="mineDetail.error_info">
        <div class="detail-sub">错误信息</div>
        <pre class="mono sub-code">{{ mineDetail.error_info }}</pre>
      </template>
      <div class="detail-sub">代码</div>
      <pre class="mono sub-code">{{ mineDetail.code }}</pre>
      <template v-if="mineDetail.detail?.length">
        <div class="detail-sub">测试点明细</div>
        <el-table :data="mineDetail.detail" border>
          <el-table-column prop="index" label="测试点" width="90" />
          <el-table-column label="结果">
            <template #default="{ row }">
              <el-tag size="small" :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="耗时" width="120">
            <template #default="{ row }">{{ row.time_ms }} ms</template>
          </el-table-column>
          <el-table-column label="内存" width="120">
            <template #default="{ row }">{{ fmtMem(row.memory_kb) }}</template>
          </el-table-column>
        </el-table>
      </template>
    </template>
  </el-dialog>
</template>

<style scoped>
.side-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}

.mb8 {
  margin-bottom: 8px;
}

.detail-desc {
  margin-bottom: 4px;
}

.detail-sub {
  font-weight: 600;
  margin: 14px 0 8px;
}

.sub-code {
  margin: 0;
  padding: 10px 14px;
  background: #f8f8f8;
  border-radius: 4px;
  max-height: 320px;
  overflow: auto;
}

.status-tag {
  cursor: help;
}

.status-tip {
  line-height: 1.6;
  max-width: 260px;
  white-space: normal;
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
}
</style>

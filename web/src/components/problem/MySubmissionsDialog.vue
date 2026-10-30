<script setup lang="ts">
// 我的提交弹窗（v0.5.4 ② 拆分自 ProblemDetailView）：列表点行 → 同弹窗切详情
// （代码/耗时/内存/错误信息/测试点明细）。父层提交后经 refresh() 刷新列表。
// AI 找 bug（v0.33-④ 验收调整）：详情视图未 AC 提交可让 AI 分析（价格按题拉取、
// 结果渲染在弹窗尾；服务端把关计费/每日上限/赛时封锁，失败自动退费）。
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { aiTutorApi, problemApi } from '@/api'
import CodePane from '@/components/CodePane.vue'
import MdView from '@/components/MdView.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, fmtRel, fmtSec, isQueueStatus, statusDesc } from '@/utils/format'
import { useUserStore } from '@/stores/user'
import type { Submission } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problemId: number
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const userStore = useUserStore()

const PAGE_SIZE = 10
const mySubs = ref<Submission[]>([])
const myTotal = ref(0)
const myPage = ref(1)
const mineView = ref<'list' | 'detail'>('list')
const mineDetail = ref<Submission | null>(null)
// 测试点按题目数据顺序展示（防后端返回顺序波动）；系统级错误（沙箱失败等）并入表尾明细行
const sortedDetail = computed(() => {
  const rows = [...(mineDetail.value?.detail ?? [])].sort((a, b) => a.index - b.index)
  const info = mineDetail.value?.error_info
  if (info && !/^第 \d+ 个测试点/.test(info)) rows.push({ msg: info } as never)
  return rows
})

// 最后一页不足 PAGE_SIZE 时补空白行，让表格保持整页高度
const displayRows = computed(() => {
  const fill = Math.max(0, PAGE_SIZE - mySubs.value.length)
  return [...mySubs.value, ...Array.from({ length: fill }, () => ({}) as Submission)]
})

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
  if (!s.id) return // 补位的空白行不可点
  mineDetail.value = s
  mineView.value = 'detail'
  aiDebugContent.value = ''
  loadAiPrice()
}

// ---- AI 找 bug（v0.33-④）----
const aiReady = ref(false)
const aiName = ref('小玉')
const aiDebugPrice = ref(0)
const aiDebugging = ref(false)
const aiDebugContent = ref('')

async function loadAiPrice() {
  aiReady.value = false
  if (!userStore.token || !props.problemId) return
  try {
    const r = await aiTutorApi.status(props.problemId)
    aiReady.value = r.enabled
    aiName.value = r.name ?? '小玉'
    aiDebugPrice.value = r.debug_price ?? 0
  } catch {
    aiReady.value = false
  }
}

const canAiDebug = computed(() => {
  const s = mineDetail.value
  return !!s && s.status !== 'Accepted' && !isQueueStatus(s.status)
})

async function onAiDebug() {
  const s = mineDetail.value
  if (!s) return
  try {
    await ElMessageBox.confirm(
      `花 ${aiDebugPrice.value} 玉让 ${aiName.value} 分析这份代码的错误？（最小改动修复，不重写）`,
      `${aiName.value} 找 bug`,
      { confirmButtonText: `${aiDebugPrice.value} 玉分析`, cancelButtonText: '再想想', type: 'info' },
    )
  } catch {
    return
  }
  aiDebugging.value = true
  aiDebugContent.value = ''
  try {
    const r = await aiTutorApi.debug(s.id)
    aiDebugContent.value = r.content
    userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
    ElMessage.success('分析完成')
  } catch {
    // 拦截器统一提示（余额不足/每日上限/赛时封锁/生成失败已退费）
  } finally {
    aiDebugging.value = false
  }
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
      <el-table stripe :data="displayRows" highlight-current-row @row-click="openMineDetail">
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
            <template v-if="row.id">{{ fmtRel(row.created_at) }}</template>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!mySubs.length" description="还没有提交过" :image-size="48" />
      <div v-if="myTotal > 0" class="side-pager">
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
            <SubStatus :status="mineDetail.status" :score="mineDetail.score" />
          </el-tooltip>
          <SubStatus v-else :status="mineDetail.status" :score="mineDetail.score" />
          <!-- AI 找 bug（v0.33-④）：未 AC 且非队列态可分析 -->
          <el-button
            v-if="aiReady && canAiDebug"
            link
            type="warning"
            size="small"
            class="lk-btn"
            :loading="aiDebugging"
            @click="onAiDebug"
          >
            ✦ {{ aiName }} 找 bug（{{ aiDebugPrice }} 玉）
          </el-button>
        </el-descriptions-item>
        <el-descriptions-item label="语言">{{ mineDetail.language }}</el-descriptions-item>
        <el-descriptions-item label="总耗时">{{ fmtSec(mineDetail.time_ms) }}</el-descriptions-item>
        <el-descriptions-item label="总内存">{{ fmtMem(mineDetail.memory_kb) }}</el-descriptions-item>
        <el-descriptions-item label="提交时间">{{ fmtDate(mineDetail.created_at) }}</el-descriptions-item>
      </el-descriptions>
      <!-- 有测试点明细时错误原因已在明细列展示（表尾 err 行），仅无明细时独立成块（如编译错误） -->
      <template v-if="mineDetail.error_info && !sortedDetail.length">
        <div class="detail-sub">错误信息</div>
        <pre class="mono sub-code">{{ mineDetail.error_info }}</pre>
      </template>
      <div class="detail-sub">代码</div>
      <CodePane :code="mineDetail.code" :language="mineDetail.language" />
      <template v-if="mineDetail.detail?.length">
        <div class="detail-sub">测试点明细</div>
        <el-table stripe :data="sortedDetail" border>
          <el-table-column prop="index" label="测试点" width="90" />
          <el-table-column label="结果">
            <template #default="{ row }">
              <SubStatus v-if="row.status" :status="row.status" />
            </template>
          </el-table-column>
          <el-table-column label="明细" min-width="150">
            <template #default="{ row }">
              <span v-if="row.msg" class="case-err">{{ row.msg }}</span>
            </template>
          </el-table-column>
          <el-table-column label="耗时" width="90">
            <template #default="{ row }">{{ fmtSec(row.time_ms) }}</template>
          </el-table-column>
          <el-table-column label="内存" width="90">
            <template #default="{ row }">{{ row.memory_kb ? fmtMem(row.memory_kb) : '' }}</template>
          </el-table-column>
        </el-table>
      </template>
      <template v-else-if="mineDetail.error_info">
        <div class="detail-sub">错误信息</div>
        <pre class="mono sub-code">{{ mineDetail.error_info }}</pre>
      </template>
      <!-- AI 找 bug 结果（v0.33-④）：错误定位 → 原因 → 最小修复建议 -->
      <template v-if="aiDebugContent">
        <div class="detail-sub ai-debug-sub">✦ {{ aiName }} 找 bug</div>
        <MdView :content="aiDebugContent" variant="compact" auto-height />
      </template>
    </template>
  </el-dialog>
</template>

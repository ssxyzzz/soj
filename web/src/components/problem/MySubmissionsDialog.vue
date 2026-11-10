<script setup lang="ts">
// 我的提交弹窗（v0.5.4 ② 拆分自 ProblemDetailView）：列表点行 → 同弹窗切详情
// （代码/耗时/内存/错误信息/测试点明细）。父层提交后经 refresh() 刷新列表。
// AI 找 bug（v0.33-④ 验收调整）：详情视图未 AC 提交可让 AI 分析（价格按题拉取、
// 结果渲染在弹窗尾；服务端把关计费/每日上限/赛时封锁，失败自动退费）。
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { aiTutorApi, problemApi, submissionApi } from '@/api'
import CodePane from '@/components/CodePane.vue'
import MdView from '@/components/MdView.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtDate, fmtMem, fmtRel, fmtSec, isQueueStatus, statusDesc } from '@/utils/format'
import { useUserStore } from '@/stores/user'
import type { ContestMySubmission, Submission } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problemId: number
  /** 赛时模式（v0.90 统一批③收敛）：外部注入场内提交列表（含未公开提交）——
   *  不内部拉取不分页，数据由页面随场内 loadMine 更新；缺省 = 题库模式自查 */
  rows?: ContestMySubmission[]
  /** 赛时是否已揭榜（赛时模式）：false = OI 赛制遮罩（状态列「已记录」、耗时/内存
   *  横杠、详情裁剪测试点明细与 AI 找 bug），true = 正常展示 */
  revealed?: boolean
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const userStore = useUserStore()

// 是否处于可出分视角：题库模式恒 true；赛时模式看揭榜
const isRevealed = computed(() => !props.rows || props.revealed !== false)

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

// 最后一页不足 PAGE_SIZE 时补空白行，让表格保持整页高度（题库分页模式；赛时全量不补位）
const displayRows = computed(() => {
  if (props.rows) return displaySource.value
  const fill = Math.max(0, PAGE_SIZE - mySubs.value.length)
  return [...mySubs.value, ...Array.from({ length: fill }, () => ({}) as Submission)]
})

async function loadMine(p = 1) {
  if (props.rows) return // 赛时模式数据外部注入，不自查
  try {
    const r = await problemApi.mySubmissions(props.problemId, p, PAGE_SIZE)
    mySubs.value = r.list
    myTotal.value = r.total
    myPage.value = p
  } catch {
    mySubs.value = []
  }
}

// 展示行：赛时模式 = 外部注入的场内提交（响应式跟随页面 loadMine）；题库模式 = 分页自查。
// ContestMySubmission 与 Submission 是同表两投影（problem_id vs pid 命名差），表格/详情
// 只消费共有子集（id/language/time_ms/memory_kb/created_at/status/score/code），断言统一
const displaySource = computed<Submission[]>(() => (props.rows ? (props.rows as unknown as Submission[]) : mySubs.value))

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

async function openMineDetail(s: Submission) {
  if (!s.id) return // 补位的空白行不可点
  // 赛时模式拆形（v0.64 口径）：已揭榜的列表行瘦身不带 code，点开按需拉详情；
  // 隐藏期行自带 code（考时回看唯一通道）直接展示
  if (props.rows && props.revealed && !s.code) {
    try {
      mineDetail.value = await submissionApi.get(s.id)
    } catch {
      return // 拦截器已提示
    }
  } else {
    mineDetail.value = s
  }
  mineView.value = 'detail'
  loadAiPrice()
  // 找 bug 结果常驻（2026-10-30 反馈）：按提交缓存；没拉过就向后端取历史分析
  if (s.id && aiDebugMap.value[s.id] === undefined) {
    aiTutorApi
      .result(s.id)
      .then((r) => {
        if (r.content) aiDebugMap.value[s.id] = r.content
      })
      .catch(() => {})
  }
}

// ---- AI 找 bug（v0.33-④）----
const aiReady = ref(false)
const aiFree = ref(false) // root/特权层：免确认直接分析（2026-10-30）
const aiName = ref('小玉')
const aiDebugPrice = ref(0)
const aiDebugging = ref(false)
// 结果按提交 ID 常驻缓存（2026-10-30 反馈）：切详情/关弹窗不再丢，回看直接命中
const aiDebugMap = ref<Record<number, string>>({})
// 结果弹窗（2026-10-30 同一套拍板）：查看免费；重新生成每次扣玉、每点一次都重算
const dbg = reactive({ show: false, content: '', loading: false }) // content = 写入时渲染 html（v0.53）

function openDebugDlg() {
  const s = mineDetail.value
  if (!s) return
  dbg.content = aiDebugMap.value[s.id] ?? ''
  dbg.show = true
}

async function loadAiPrice() {
  aiReady.value = false
  if (!userStore.token || !props.problemId) return
  try {
    const r = await aiTutorApi.status(props.problemId)
    aiReady.value = r.enabled
    aiFree.value = r.free ?? false
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
  // 已有结果：直接打开结果弹窗（查看免费，拍板 B 同一套）
  if (aiDebugMap.value[s.id]) {
    openDebugDlg()
    return
  }
  // 首次分析：扣玉确认（免费层免确认，2026-10-30）
  if (!aiFree.value) {
    try {
      await ElMessageBox.confirm(
        `花 ${aiDebugPrice.value} 玉让 ${aiName.value} 分析这份代码的错误？（最小改动修复，不重写）`,
        `${aiName.value} 找 bug`,
        { confirmButtonText: `${aiDebugPrice.value} 玉分析`, cancelButtonText: '再想想', type: 'info' },
      )
    } catch {
      return
    }
  }
  dbg.content = ''
  dbg.show = true
  await runDebug(s.id)
}

// 调 AI 分析并落缓存；弹窗开着就原位替换内容
async function runDebug(sid: number) {
  aiDebugging.value = true
  dbg.loading = true
  try {
    const r = await aiTutorApi.debug(sid)
    aiDebugMap.value[sid] = r.content_html
    userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
    if (dbg.show) dbg.content = r.content_html
    ElMessage.success('分析完成')
  } catch {
    // 拦截器统一提示（余额不足/每日上限/赛时封锁/生成失败已退费）
    if (dbg.show && !aiDebugMap.value[sid]) dbg.show = false
  } finally {
    aiDebugging.value = false
    dbg.loading = false
  }
}

// 弹窗内「重新生成」：学生每次扣玉确认，每点一次都重算（拍板 B）
async function onDebugRegen() {
  const s = mineDetail.value
  if (!s || aiDebugging.value) return
  if (!aiFree.value) {
    try {
      await ElMessageBox.confirm(
        `花 ${aiDebugPrice.value} 玉重新生成分析？将替换当前内容。`,
        `${aiName.value} 找 bug`,
        { confirmButtonText: `${aiDebugPrice.value} 玉重新生成`, cancelButtonText: '再想想', type: 'info' },
      )
    } catch {
      return
    }
  }
  await runDebug(s.id)
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
              <!-- 赛时未揭榜（OI 赛制）：不出分，状态列只报「已记录」 -->
              <span v-if="!isRevealed" class="dim">已记录</span>
              <el-tooltip v-else-if="statusDesc(row.status)" placement="top" effect="light">
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
            <template v-if="row.id">{{ isRevealed ? fmtSec(row.time_ms) : '—' }}</template>
          </template>
        </el-table-column>
        <el-table-column label="总内存" width="110">
          <template #default="{ row }">
            <template v-if="row.id">{{ isRevealed && row.memory_kb ? fmtMem(row.memory_kb) : '—' }}</template>
          </template>
        </el-table-column>
        <el-table-column label="提交时间">
          <template #default="{ row }">
            <template v-if="row.id">{{ fmtRel(row.created_at) }}</template>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!displaySource.length" description="还没有提交过" :image-size="48" />
      <div v-if="!rows && myTotal > 0" class="side-pager">
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
      <!-- 详情头行：返回列表居左，AI 找 bug 居右（2026-10-30 反馈：从状态格挪出）；
           AI 找 bug 赛时未揭榜不出现（服务端本就封锁，UI 同步收敛） -->
      <div class="detail-top">
        <el-button @click="mineView = 'list'">← 返回列表</el-button>
        <el-button
          v-if="isRevealed && aiReady && canAiDebug"
          link
          type="warning"
          size="small"
          :loading="aiDebugging"
          @click="onAiDebug"
        >
          ✦ {{ aiName }} 找 bug（{{ aiDebugPrice }} 玉）
        </el-button>
      </div>
      <el-descriptions :column="3" border class="detail-desc">
        <el-descriptions-item label="状态">
          <span v-if="!isRevealed" class="dim">已记录</span>
          <el-tooltip v-else-if="statusDesc(mineDetail.status)" placement="top" effect="light">
            <template #content>
              <div class="status-tip">
                <div>{{ statusDesc(mineDetail.status)!.meaning }}</div>
                <div v-if="statusDesc(mineDetail.status)!.cause">常见原因：{{ statusDesc(mineDetail.status)!.cause }}</div>
              </div>
            </template>
            <SubStatus :status="mineDetail.status" :score="mineDetail.score" />
          </el-tooltip>
          <SubStatus v-else :status="mineDetail.status" :score="mineDetail.score" />
        </el-descriptions-item>
        <el-descriptions-item label="语言">{{ mineDetail.language }}</el-descriptions-item>
        <el-descriptions-item label="总耗时">{{ isRevealed ? fmtSec(mineDetail.time_ms) : '—' }}</el-descriptions-item>
        <el-descriptions-item label="总内存">{{ isRevealed && mineDetail.memory_kb ? fmtMem(mineDetail.memory_kb) : '—' }}</el-descriptions-item>
        <el-descriptions-item label="提交时间">{{ fmtDate(mineDetail.created_at) }}</el-descriptions-item>
      </el-descriptions>
      <!-- 有测试点明细时错误原因已在明细列展示（表尾 err 行），仅无明细时独立成块（如编译错误）；
           测试点明细/错误信息赛时未揭榜不出（数据本就不带 detail，防御性同遮） -->
      <template v-if="isRevealed && mineDetail.error_info && !sortedDetail.length">
        <div class="detail-sub">错误信息</div>
        <pre class="mono sub-code">{{ mineDetail.error_info }}</pre>
      </template>
      <div class="detail-sub">代码</div>
      <CodePane :code="mineDetail.code" :language="mineDetail.language" />
      <template v-if="isRevealed && mineDetail.detail?.length">
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
      <template v-else-if="isRevealed && mineDetail.error_info">
        <div class="detail-sub">错误信息</div>
        <pre class="mono sub-code">{{ mineDetail.error_info }}</pre>
      </template>
      <!-- AI 找 bug 改弹窗交互（2026-10-30 拍板 B 同一套）：结果在结果弹窗查看，
           不再铺在详情底部 -->
    </template>

    <!-- AI 找 bug 结果弹窗：动作在右上角（重新生成 / 关闭，拍板 C）；
         查看免费，重新生成每次扣玉、每点一次都重算 -->
    <el-dialog v-model="dbg.show" :show-close="false" width="960" top="6vh" append-to-body>
      <template #header>
        <div class="dlg-head">
          <span class="dlg-title">✦ {{ aiName }} · 找 bug</span>
          <span class="dlg-acts">
            <el-button size="small" type="warning" :loading="aiDebugging" @click="onDebugRegen">
              {{ aiFree ? '重新生成' : `重新生成（${aiDebugPrice} 玉）` }}
            </el-button>
            <el-button size="small" @click="dbg.show = false">关闭</el-button>
          </span>
        </div>
      </template>
      <div v-loading="dbg.loading" class="dbg-body">
        <MdView v-if="dbg.content" :html="dbg.content" variant="compact" />
      </div>
    </el-dialog>
  </el-dialog>
</template>

<style scoped>
/* 找 bug 结果弹窗头：标题居左、动作居右上（拍板 C） */
.dlg-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.dlg-title {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
}

.dlg-acts {
  display: inline-flex;
  gap: 8px;
}

.dbg-body {
  min-height: 160px;
  max-height: 64vh;
  overflow: auto;
}
</style>

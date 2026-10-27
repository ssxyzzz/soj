<script setup lang="ts">
// 导入题目独立页（v0.15 统一题包格式，import-design.md §2）：题库页「导入题目」跳转至此。
// 第一步选来源（SOJ / Hydro 本期实做；QDUOJ / HOJ 下期置灰），每种来源独立适配器：
// SOJ = problem.md 定题目目录 + data/ 数据；Hydro = 官方导出包（problem_zh.md 题面 + testdata/）。
// 上传后原地轮询进度（逐题结果流），取消立马生效（已导入保留）；全站单任务排队。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled, UploadFilled } from '@element-plus/icons-vue'
import type { UploadUserFile } from 'element-plus'

import { importApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import type { BatchImportStatus } from '@/types'

type Source = 'soj' | 'hydro'

const source = ref<Source>('soj')

// 来源卡片：SOJ/Hydro 本期实做；QDUOJ/HOJ 占位置灰（下期支持）
const sources: { id: Source | 'qduoj' | 'hoj'; label: string; hint: string; soon?: boolean }[] = [
  { id: 'soj', label: '从 SOJ 导入', hint: '站内统一格式：problem.md 题面 + problem.yaml + data/（config.yaml + 数据对原名 + 附属文件）' },
  { id: 'hydro', label: '从 Hydro 导入', hint: 'HydroOJ 题库导出包：problem_zh.md 题面 + problem.yaml（title/tag）+ testdata/（config.yaml + 数据 + gen.py 等）' },
  { id: 'qduoj', label: '从 QDUOJ 导入', hint: '下期支持', soon: true },
  { id: 'hoj', label: '从 HOJ 导入', hint: '下期支持', soon: true },
]

const fileList = ref<UploadUserFile[]>([])
const skipSameTitle = ref(false)
const defaultHidden = ref(false)
// 已存在题号跳过（v0.18.1 验收调整，仅 SOJ 源）：目录名带题号前缀（题号_标题 /
// 题号 标题，下划线与空格都认）——开启后按前缀题号落库、已存在则跳过不导入
// （防导出包重复导回造成重复题；不覆盖既有题）；关闭则忽略前缀、一律自动取号
const skipExistingPID = ref(false)
const starting = ref(false)
// v0.18.2 上传进度（XHR onUploadProgress 驱动；-1 = 非上传态）
const uploadPct = ref(-1)

const status = ref<BatchImportStatus>({ state: 'idle', total: 0, done: 0, ok_count: 0, fail_count: 0, skip_count: 0, results: [] })
const running = computed(() => status.value.state === 'running')
const finished = computed(() => ['done', 'cancelled', 'failed'].includes(status.value.state))
// v0.18.2 解压+识别阶段（total 未定，前端出不定态提示而非 0% 假进度；旧后端无 phase 时以 total===0 兜底）
const extracting = computed(
  () => running.value && (status.value.phase === 'extracting' || status.value.total === 0),
)
// 解压实时百分比（按字节；null = 后端还没报出总量 → 不定态兜底）
const extractPct = computed(() => {
  const t = status.value.extract_total ?? 0
  if (!t) return null
  return Math.min(100, Math.round(((status.value.extract_done ?? 0) / t) * 100))
})

let timer: ReturnType<typeof setInterval> | undefined
let loadSeq = 0

async function refreshStatus() {
  const seq = ++loadSeq
  let s: BatchImportStatus
  try {
    s = await importApi.status()
  } catch {
    return
  }
  if (seq !== loadSeq) return
  status.value = s
  if (finished.value) stopPolling()
}

function ensurePolling() {
  if (!timer) timer = setInterval(refreshStatus, 1000)
}

function stopPolling() {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

onMounted(() => {
  refreshStatus().then(() => {
    if (running.value) ensurePolling()
  })
})

onBeforeUnmount(stopPolling)

// v0.18.2 终态清空文件列表：表单常驻后 el-upload 的 limit=1 会因残留旧文件拦截新选择
// （on-exceed 而非 on-change，旧文件被原样再传一遍）。终态 = 上一批已完结，清列表放行新包。
watch(finished, (done) => {
  if (done) fileList.value = []
})

function onZipChange(_file: UploadUserFile, files: UploadUserFile[]) {
  fileList.value = files.slice(-1)
}

function onZipRemove(_file: UploadUserFile, files: UploadUserFile[]) {
  fileList.value = files
}

async function startImport() {
  const f = (fileList.value[0]?.raw as File) ?? null
  if (!f) {
    ElMessage.warning('请先选择题包 zip')
    return
  }
  if (running.value) {
    ElMessage.warning('已有导入任务进行中，等它完成或先取消')
    return
  }
  starting.value = true
  uploadPct.value = 0
  try {
    const fd = new FormData()
    fd.append('source', source.value)
    fd.append('zip', f)
    if (skipSameTitle.value) fd.append('skip_same_title', '1')
    if (defaultHidden.value) fd.append('default_hidden', '1')
    if (source.value === 'soj' && skipExistingPID.value) fd.append('skip_existing_pid', '1')
    await importApi.upload(fd, (pct) => {
      uploadPct.value = pct
    })
    ElMessage.success('导入已开始')
    await refreshStatus()
    ensurePolling()
  } catch {
    // 错误提示由 axios 拦截器统一弹出（如 409 已有任务进行中）
  } finally {
    starting.value = false
    uploadPct.value = -1
  }
}

// 取消立马生效：中断当前题识别、后续目录不再处理（已导入保留）
async function cancelImport() {
  try {
    await ElMessageBox.confirm(
      '取消后立即停止导入（已导入的题目保留）。确认取消？',
      '取消导入',
      { type: 'warning', confirmButtonText: '取消导入', cancelButtonText: '继续导入' },
    )
  } catch {
    return
  }
  await importApi.cancel()
  ElMessage.info('已取消，正在停止导入')
}

async function confirmLeave(): Promise<boolean> {
  if (!running.value) return true
  try {
    await ElMessageBox.confirm('导入仍在进行，离开将取消导入（已导入的题目保留）。确定离开？', '导入进行中', {
      type: 'warning',
      confirmButtonText: '取消导入并离开',
      cancelButtonText: '留下',
    })
  } catch {
    return false
  }
  await importApi.cancel()
  stopPolling()
  return true
}

onBeforeRouteLeave(async () => confirmLeave())

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (running.value) {
    e.preventDefault()
    e.returnValue = ''
  }
}

window.addEventListener('beforeunload', onBeforeUnload)
onBeforeUnmount(() => window.removeEventListener('beforeunload', onBeforeUnload))

function resultLine(r: { status: string; name: string; title?: string; pid?: number; reason?: string }): string {
  const who = r.title ? `「${r.title}」` : r.name
  if (r.status === 'ok') return `${r.name} → #${r.pid} ${who} 导入成功`
  if (r.status === 'skipped') return `${r.name} → ${who} 跳过：${r.reason ?? '同标题已存在'}`
  return `${r.name} → 失败：${r.reason ?? '未知原因'}`
}

function resultClass(s: string): string {
  return s === 'ok' ? 'is-ok' : s === 'skipped' ? 'is-skip' : 'is-fail'
}

const stateLabel = computed(() => {
  const map: Record<string, string> = {
    idle: '未在导入',
    running: '导入中',
    done: '已完成',
    cancelled: '已取消',
    failed: '整包失败',
  }
  return map[status.value.state] ?? status.value.state
})

const progressPct = computed(() => {
  const s = status.value
  if (!s.total) return 0
  return Math.round((s.done / s.total) * 100)
})

// v0.18.2 限窗：running 期只渲染最近 50 条——几百题的大包不再每秒整列重渲染
// （?? [] 防御：任务启动/终态 failed 时后端快照可能没有 results 字段）
const RESULT_WINDOW = 50
const reversedResults = computed(() => (status.value.results ?? []).slice(-RESULT_WINDOW).reverse())
// v0.18.2 终态：全量列出失败/跳过项（成功的靠汇总计数，几百条成功不刷屏）；正序成报告
const finishedIssues = computed(() =>
  finished.value ? (status.value.results ?? []).filter((r) => r.status !== 'ok') : [],
)
</script>

<template>
  <PageShell class="import-shell">
    <div class="page-head">
      <div class="kicker">IMPORT</div>
      <h1 class="page-title">导入题目<small>题包（含题面）统一入口 · 题号一律自动分配</small></h1>
    </div>
    <el-card>
      <template #header>
        <div class="panel-h step-head">
          <span class="pt">第一步 · 选择来源</span>
          <span v-if="running" class="sub-status yellow"><span class="dot yellow"></span>{{ stateLabel }}（{{ status.done }}/{{ status.total }}）</span>
          <el-tag v-else-if="finished" size="small" :type="status.state === 'done' ? 'success' : 'info'">{{ stateLabel }}</el-tag>
        </div>
      </template>

      <div class="dim tip">
        题包（含题面）统一入口；建题/编辑页的 zip 只收 data/ 数据与配置、不导题面。
        单题包（zip 根即题目目录）与多题包（根下每目录一题）均可，混装混格式均可；
        目录名数字前缀不指定题号，题号一律自动分配。
      </div>

      <!-- 第一步：选来源（每来源独立适配器，不做混合嗅探；v0.18.2 表单常驻：终态后直接换包再导） -->
      <div v-if="!running" class="source-grid">
        <div
          v-for="s in sources"
          :key="s.id"
          class="source-card"
          :class="{ active: source === s.id, soon: s.soon }"
          @click="!s.soon && (source = s.id as Source)"
        >
          <div class="source-title">
            {{ s.label }}
            <el-tag v-if="s.soon" size="small" type="info">下期支持</el-tag>
          </div>
          <div class="source-hint dim">{{ s.hint }}</div>
        </div>
      </div>

      <div class="panel-h phase-two">
        <span class="pt-sm">第二步 · 上传与导入</span>
        <span v-if="fileList[0]" class="dim file-name">{{ fileList[0]?.name }} · {{ ((fileList[0]?.size ?? 0) / 1024 / 1024).toFixed(1) }} MB</span>
        <el-button v-if="running" size="small" style="margin-left: auto" @click="cancelImport">取消导入</el-button>
      </div>

      <el-upload
        v-model:file-list="fileList"
        class="import-drop"
        drag
        accept=".zip"
        :limit="1"
        :auto-upload="false"
        :disabled="running"
        :on-change="onZipChange"
        :on-remove="onZipRemove"
      >
        <div class="drop-hint">
          <!-- 大号上传图标（2026-10-27 拍板：参考云上传块样式，上传入口更显眼） -->
          <div class="drop-icon"><el-icon><UploadFilled /></el-icon></div>
          <p>{{ running ? '导入进行中，等待完成或取消' : '拖入题包 zip 或点击选择' }}</p>
          <p class="dim">单题数据 ≤100MB、整包 ≤2GB（root 可在系统设置调整）；题多分批传</p>
        </div>
      </el-upload>

      <div class="switch-row">
        <el-switch v-model="skipSameTitle" :disabled="running" />
        <span>同标题跳过</span>
        <el-tooltip placement="top" content="开启后题库里已有同标题题的目录直接跳过（迁移场景防手抖重复导）；默认关 = 同标题照样新建新题">
          <el-icon class="dim"><QuestionFilled /></el-icon>
        </el-tooltip>
      </div>
      <div class="switch-row">
        <el-switch v-model="defaultHidden" :disabled="running" />
        <span>导入后默认隐藏</span>
        <el-tooltip placement="top" content="导入的题默认公开可见；开启后未在 yaml 里声明 hidden 的题一律先隐藏，整理完再放开">
          <el-icon class="dim"><QuestionFilled /></el-icon>
        </el-tooltip>
      </div>
      <div v-if="source === 'soj'" class="switch-row">
        <el-switch v-model="skipExistingPID" :disabled="running" />
        <span>已存在题号跳过</span>
        <el-tooltip placement="top" content="目录名带题号前缀（题号_标题 或 题号 标题，Windows 直接压缩的包多为空格）：开启后按前缀题号落库、已存在的题号直接跳过（防导出包重复导回造成重复题，不覆盖既有题）；关闭则忽略前缀、一律自动取号新建">
          <el-icon class="dim"><QuestionFilled /></el-icon>
        </el-tooltip>
      </div>

      <!-- v0.18.2 上传实时进度（onUploadProgress；巨型包上传期不再是盲转圈）+ v3 pbar -->
      <div v-if="uploadPct >= 0" class="upload-progress">
        <div class="soj-pbar"><i :style="{ width: `${uploadPct}%` }" /></div>
        <div class="pstat"><span>上传中 · {{ uploadPct }}%</span></div>
      </div>

      <div class="actions">
        <el-button type="primary" :loading="starting" :disabled="running || !fileList.length" @click="startImport">
          {{ uploadPct >= 0 ? `上传中 ${uploadPct}%` : '上传并导入' }}
        </el-button>
      </div>

      <!-- 进度与结果流（running 与结束态共用；v3 pbar + pstat + 深色日志窗） -->
      <div v-if="status.state !== 'idle'" class="progress-block">
        <div class="soj-pbar">
          <i
            :style="{
              width: `${extracting ? (extractPct ?? 0) : progressPct}%`,
              opacity: extracting && extractPct === null ? 0.35 : 1,
            }"
          />
        </div>
        <div class="pstat">
          <template v-if="extracting">
            <span>正在解压题包{{ extractPct !== null ? ` · ${extractPct}%` : '…（大包可能需要一两分钟）' }}</span>
          </template>
          <template v-else-if="running">
            <span>解压进度 {{ status.done }} / {{ status.total }} 题 · {{ progressPct }}%</span>
            <span>成功 {{ status.ok_count }} · 跳过 {{ status.skip_count }} · 失败 {{ status.fail_count }}</span>
            <span v-if="status.by">发起人 {{ status.by }}</span>
          </template>
          <template v-else-if="status.state === 'failed'">
            <span class="is-fail">整包失败：{{ status.error || '未知原因' }}</span>
          </template>
          <template v-else>
            <span>报告：成功 {{ status.ok_count }} · 跳过 {{ status.skip_count }} · 失败 {{ status.fail_count }}{{ status.state === 'cancelled' ? '（已取消）' : '' }}</span>
          </template>
        </div>
        <!-- 结果流：running = 最近 50 条滚动；终态 = 失败/跳过全量列出（成功靠汇总计数） -->
        <div v-if="running && reversedResults.length" class="soj-ilog result-stream">
          <div class="result-stream-head dim">
            结果流{{ (status.results?.length ?? 0) > RESULT_WINDOW ? `（仅显示最近 ${RESULT_WINDOW} 条 / 共 ${status.results?.length} 条）` : '' }}
          </div>
          <div v-for="(r, i) in reversedResults" :key="i" class="result-line" :class="resultClass(r.status)">
            {{ resultLine(r) }}
          </div>
        </div>
        <div v-else-if="finished && finishedIssues.length" class="soj-ilog result-stream">
          <div class="result-stream-head dim">失败 / 跳过（全部列出，共 {{ finishedIssues.length }} 条）</div>
          <div v-for="(r, i) in finishedIssues" :key="i" class="result-line" :class="resultClass(r.status)">
            {{ resultLine(r) }}
          </div>
        </div>
        <div v-else-if="finished && status.state !== 'failed'" class="soj-ilog result-stream">
          <div class="result-line is-ok">全部导入成功，无失败 / 跳过项</div>
        </div>
      </div>
    </el-card>

    <template #aside>
      <el-card class="aside-card">
        <template #header><span>格式速查</span></template>
        <div class="dim fmt">
          <p><b>SOJ 单题包</b>：一个目录 = problem.md（题面）+ problem.yaml（title/type/difficulty/tag/来源）+ data/（config.yaml、数据对原名、gen.py 等附属文件）。</p>
          <p><b>多题包</b>：zip 根下每目录一题；客观题另带 answer.yaml（答案）与 answer.md（解析），无 data/。</p>
          <p><b>Hydro 导出包</b>：原样上传即可（problem_zh.md 题面、testdata/、config.yaml 自动识别）。</p>
        </div>
      </el-card>
    </template>
  </PageShell>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.tip {
  margin-bottom: 14px;
  line-height: 1.7;
}

.source-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-bottom: 14px;
}

@media (max-width: 768px) {
  .source-grid {
    grid-template-columns: 1fr;
  }
}

/* v3 源卡片：14px 圆角、主蓝选中态（浅底 + 蓝边），标题藏蓝 700 */
.source-card {
  border: 1px solid rgba(11, 31, 69, 0.13);
  border-radius: 14px;
  padding: 14px 16px;
  cursor: pointer;
  transition: all 0.2s;
  background: var(--el-bg-color);
}

.source-card:hover {
  border-color: var(--soj-blue);
}

.source-card.active {
  border-color: var(--soj-blue);
  background: var(--soj-blue-soft);
  box-shadow: 0 4px 14px rgb(var(--soj-blue-rgb) / 0.12);
}

.source-card.soon {
  cursor: not-allowed;
  opacity: 0.55;
}

.source-title {
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  display: flex;
  align-items: center;
  gap: 8px;
}

.source-hint {
  margin-top: 6px;
  line-height: 1.6;
  font-size: var(--soj-fs-small);
}

.import-drop {
  width: 100%;
}

.import-drop :deep(.el-upload-dragger) {
  width: 100%;
  --el-upload-dragger-padding-vertical: 18px;
}

.drop-hint {
  text-align: center;
}

.drop-hint p {
  margin: 4px 0;
}

/* 大号上传图标块：浅蓝圆角方底 + 主蓝图标（对齐云上传参考样式），拖拽悬停微放大 */
.drop-icon {
  width: 56px;
  height: 56px;
  margin: 2px auto 10px;
  border-radius: 14px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 30px;
  transition: transform 0.15s;
}

.import-drop :deep(.el-upload-dragger:hover) .drop-icon,
.import-drop :deep(.el-upload-dragger.is-dragover) .drop-icon {
  transform: scale(1.08);
}

.switch-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}

.upload-progress {
  margin-top: 14px;
}

.actions {
  display: flex;
  gap: 8px;
  margin-top: 16px;
}

.progress-block {
  margin-top: 16px;
}

.progress-line {
  margin: 8px 0;
}

.progress-line b {
  font-weight: var(--soj-fw-emphasis);
}

.result-stream {
  max-height: 320px;
  overflow-y: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
  padding: 8px 12px;
  margin-top: 8px;
}

.result-stream-head {
  margin-bottom: 4px;
  font-size: var(--soj-fs-small);
}

.result-line {
  line-height: 1.8;
  font-size: var(--soj-fs-small);
}

.is-ok {
  color: var(--el-color-success);
}

.is-skip {
  color: var(--el-text-color-secondary);
}

.is-fail {
  color: var(--el-color-danger);
}

.fmt p {
  margin: 0 0 10px;
  line-height: 1.7;
}

.fmt b {
  color: var(--el-text-color-primary);
}

/* ---- v3-full 导入页补充（2026-10-26 修复：本段原追加在样式块闭合标签之后、从未生效；
     容器限宽 1080 随全站页头对齐退役，导入页与各页统一 1440） ---- */
.step-head {
  padding: 0 0 14px;
}

.phase-two {
  padding: 18px 0 10px;
}

.pt-sm {
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
}

.file-name {
  font-size: var(--soj-fs-13);
}

/* v3 .pstat：进度条下方三段状态行 */
.pstat {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
  margin-top: 10px;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.pstat .is-ok {
  color: var(--soj-ac);
}

.pstat .is-skip {
  color: var(--soj-tle);
}

.pstat .is-fail {
  color: var(--soj-wa);
}

/* 日志窗内的行色由全局 .soj-ilog 定义；head 行弱化 */
.soj-ilog .result-stream-head {
  color: var(--soj-code-cm);
  margin-bottom: 4px;
}

.soj-ilog .result-line {
  line-height: 1.9;
}

</style>

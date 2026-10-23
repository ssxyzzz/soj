<script setup lang="ts">
// 导入题目独立页（v0.15 统一题包格式，import-design.md §2）：题库页「导入题目」跳转至此。
// 第一步选来源（SOJ / Hydro 本期实做；QDUOJ / HOJ 下期置灰），每种来源独立适配器：
// SOJ = problem.md 定题目目录 + data/ 数据；Hydro = 官方导出包（problem_zh.md 题面 + testdata/）。
// 上传后原地轮询进度（逐题结果流），取消立马生效（已导入保留）；全站单任务排队。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
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
// 按题号覆盖更新（v0.18.1，仅 SOJ 源）：目录名带题号前缀（导出包默认 <题号>_<标题>）→ 原位全量更新
const overwriteByPID = ref(false)
const starting = ref(false)

const status = ref<BatchImportStatus>({ state: 'idle', total: 0, done: 0, ok_count: 0, updated_count: 0, fail_count: 0, skip_count: 0, results: [] })
const running = computed(() => status.value.state === 'running')
const finished = computed(() => ['done', 'cancelled', 'failed'].includes(status.value.state))

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
  try {
    const fd = new FormData()
    fd.append('source', source.value)
    fd.append('zip', f)
    if (skipSameTitle.value) fd.append('skip_same_title', '1')
    if (defaultHidden.value) fd.append('default_hidden', '1')
    if (source.value === 'soj' && overwriteByPID.value) fd.append('overwrite_by_pid', '1')
    await importApi.upload(fd)
    ElMessage.success('导入已开始')
    await refreshStatus()
    ensurePolling()
  } catch {
    // 错误提示由 axios 拦截器统一弹出（如 409 已有任务进行中）
  } finally {
    starting.value = false
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
  if (r.status === 'updated') return `${r.name} → #${r.pid} ${who} 已覆盖更新（数据已变更，历史提交如需复核请手动重测）`
  if (r.status === 'skipped') return `${r.name} → ${who} 同标题已存在，跳过`
  return `${r.name} → 失败：${r.reason ?? '未知原因'}`
}

function resultClass(s: string): string {
  return s === 'ok' || s === 'updated' ? 'is-ok' : s === 'skipped' ? 'is-skip' : 'is-fail'
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

const reversedResults = computed(() => [...status.value.results].reverse())

function resetForNextBatch() {
  ++loadSeq
  stopPolling()
  status.value = { state: 'idle', total: 0, done: 0, ok_count: 0, updated_count: 0, fail_count: 0, skip_count: 0, results: [] }
  fileList.value = []
}
</script>

<template>
  <PageShell>
    <el-card>
      <template #header>
        <div class="head">
          <span>导入题目</span>
          <el-tag v-if="running" type="warning" size="small">{{ stateLabel }}（{{ status.done }}/{{ status.total }}）</el-tag>
          <el-tag v-else-if="finished" size="small" :type="status.state === 'done' ? 'success' : 'info'">{{ stateLabel }}</el-tag>
        </div>
      </template>

      <div class="dim tip">
        题包（含题面）统一入口；建题/编辑页的 zip 只收 data/ 数据与配置、不导题面。
        单题包（zip 根即题目目录）与多题包（根下每目录一题）均可，混装混格式均可；
        目录名数字前缀不指定题号，题号一律自动分配。
      </div>

      <!-- 第一步：选来源（每来源独立适配器，不做混合嗅探） -->
      <div v-if="!running && !finished" class="source-grid">
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

      <template v-if="!finished">
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
          <el-switch v-model="overwriteByPID" :disabled="running" />
          <span>按题号覆盖更新</span>
          <el-tooltip placement="top" content="题库「打包导出」的包目录名带题号前缀（题号_标题）：开启后这些题原位覆盖更新——题面/元数据/数据全量替换，题号/题型/创建者不变，提交历史保留（数据变更后旧提交如需复核请手动重测）；无前缀目录仍新建。需对该题有编辑权（edit.all 或为创建者）">
            <el-icon class="dim"><QuestionFilled /></el-icon>
          </el-tooltip>
        </div>

        <div class="actions">
          <el-button type="primary" :loading="starting" :disabled="running || !fileList.length" @click="startImport">
            上传并导入
          </el-button>
          <el-button v-if="running" type="warning" plain @click="cancelImport">取消导入</el-button>
        </div>
      </template>

      <!-- 进度与结果流（running 与结束态共用；结束后出汇总报告） -->
      <div v-if="status.state !== 'idle'" class="progress-block">
        <el-progress :percentage="progressPct" :status="status.state === 'done' ? 'success' : undefined" />
        <div v-if="running" class="dim progress-line">
          已导入 {{ status.done }}/{{ status.total }}（成功 {{ status.ok_count }}<span v-if="status.updated_count"> · 更新 {{ status.updated_count }}</span> · 跳过 {{ status.skip_count }} · 失败 {{ status.fail_count }}）<span v-if="status.by">，发起人 {{ status.by }}</span>
        </div>
        <div v-else class="progress-line">
          <template v-if="status.state === 'failed'">
            <div class="is-fail">整包失败：{{ status.error || '未知原因' }}</div>
          </template>
          <template v-else>
            报告：成功 <b class="is-ok">{{ status.ok_count }}</b><span v-if="status.updated_count"> · 更新 <b class="is-ok">{{ status.updated_count }}</b></span> · 跳过 <b class="is-skip">{{ status.skip_count }}</b> · 失败 <b class="is-fail">{{ status.fail_count }}</b>
            <span v-if="status.state === 'cancelled'">（已取消，未导入的目录不再处理）</span>
          </template>
        </div>
        <div v-if="reversedResults.length" class="result-stream">
          <div v-for="(r, i) in reversedResults" :key="i" class="result-line" :class="resultClass(r.status)">
            {{ resultLine(r) }}
          </div>
        </div>
        <el-button v-if="finished" text type="primary" @click="resetForNextBatch">再导一批</el-button>
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
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  margin-bottom: 14px;
}

@media (max-width: 768px) {
  .source-grid {
    grid-template-columns: 1fr;
  }
}

.source-card {
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  padding: 12px 14px;
  cursor: pointer;
  transition: all 0.2s;
}

.source-card:hover {
  border-color: var(--el-color-primary);
}

.source-card.active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.source-card.soon {
  cursor: not-allowed;
  opacity: 0.55;
}

.source-title {
  font-weight: var(--soj-fw-emphasis);
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

.switch-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
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
</style>

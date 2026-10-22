<script setup lang="ts">
// 批量导入面板（v0.15）：题包（含题面）唯一入口——单一拖拽框收 zip，zip 根即单题包
// 或根下每目录一道题（SOJ / Hydro / 裸数据混装均可）。上传后停在页面轮询进度
// （逐题结果流）；离开/关闭页面前确认取消（已导入的保留）。全站单任务排队。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
import type { UploadUserFile } from 'element-plus'

import { importApi } from '@/api'
import type { BatchImportStatus } from '@/types'

const fileList = ref<UploadUserFile[]>([])
const skipSameTitle = ref(false)
const defaultHidden = ref(false)
const starting = ref(false)

const status = ref<BatchImportStatus>({ state: 'idle', total: 0, done: 0, ok_count: 0, fail_count: 0, skip_count: 0, results: [] })
const running = computed(() => status.value.state === 'running')
const finished = computed(() => ['done', 'cancelled', 'failed'].includes(status.value.state))

let timer: ReturnType<typeof setInterval> | undefined

async function refreshStatus() {
  try {
    status.value = await importApi.status()
  } catch {
    /* 轮询失败静默，下个周期重试 */
  }
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
  // 进入页面即拉一次：别的标签页/上次残留的 running 任务也能接着看
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
    fd.append('zip', f)
    if (skipSameTitle.value) fd.append('skip_same_title', '1')
    if (defaultHidden.value) fd.append('default_hidden', '1')
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

async function cancelImport() {
  try {
    await ElMessageBox.confirm(
      '取消后当前题导入完即停止（已导入的题目保留）。确认取消？',
      '取消导入',
      { type: 'warning', confirmButtonText: '取消导入', cancelButtonText: '继续导入' },
    )
  } catch {
    return
  }
  await importApi.cancel()
  ElMessage.info('已请求取消，当前题完成后停止')
}

// 离开守卫：running 时确认取消（路由内跳转 + 关闭标签页两种形态）
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

// 结果行文案与配色
function resultLine(r: { status: string; name: string; title?: string; pid?: number; reason?: string }): string {
  const who = r.title ? `「${r.title}」` : r.name
  if (r.status === 'ok') return `${r.name} → #${r.pid} ${who} 导入成功`
  if (r.status === 'skipped') return `${r.name} → ${who} 同标题已存在，跳过`
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

// 结果流倒序展示（最新在上）
const reversedResults = computed(() => [...status.value.results].reverse())
</script>

<template>
  <el-card>
    <template #header>
      <div class="panel-head">
        <span>导入题目</span>
        <el-tag v-if="running" type="warning" size="small">{{ stateLabel }}（{{ status.done }}/{{ status.total }}）</el-tag>
        <el-tag v-else-if="finished" size="small" :type="status.state === 'done' ? 'success' : 'info'">{{ stateLabel }}</el-tag>
      </div>
    </template>

    <div class="dim panel-tip">
      题包（含题面）入口：zip 根即单题包（problem.yaml + cases/ 等），或根下每个目录一道题（SOJ / Hydro / 裸数据混装均可）；
      建题/编辑页的 zip 只收数据与配置、不导题面。目录名数字前缀不指定题号，题号一律自动分配。
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
      <div v-if="running" class="dim progress-line">已导入 {{ status.done }}/{{ status.total }}（成功 {{ status.ok_count }} · 跳过 {{ status.skip_count }} · 失败 {{ status.fail_count }}）<span v-if="status.by">，发起人 {{ status.by }}</span></div>
      <div v-else class="progress-line">
        <template v-if="status.state === 'failed'">
          <div class="is-fail">整包失败：{{ status.error || '未知原因' }}</div>
        </template>
        <template v-else>
          报告：成功 <b class="is-ok">{{ status.ok_count }}</b> · 跳过 <b class="is-skip">{{ status.skip_count }}</b> · 失败 <b class="is-fail">{{ status.fail_count }}</b>
          <span v-if="status.state === 'cancelled'">（已取消，当前题之后未继续）</span>
        </template>
      </div>
      <div v-if="reversedResults.length" class="result-stream">
        <div v-for="(r, i) in reversedResults" :key="i" class="result-line" :class="resultClass(r.status)">
          {{ resultLine(r) }}
        </div>
      </div>
      <el-button v-if="finished" text type="primary" @click="status = { state: 'idle', total: 0, done: 0, ok_count: 0, fail_count: 0, skip_count: 0, results: [] }; fileList = []">
        再导一批
      </el-button>
    </div>
  </el-card>
</template>

<style scoped>
.panel-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.panel-tip {
  margin-bottom: 14px;
  line-height: 1.7;
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
</style>

<script setup lang="ts">
// 数据备份面板（v0.5.4 ② 拆分自 AdminView，v0.4.9/v0.5.1 实现）：
// 状态卡 + 一键备份下载 + Tier 1 私钥一次性弹窗 + 内置使用说明。
// v0.26 任务化：备份改后台任务（万题数据打包 GB 级不再占住请求）——run 启动即返、
// 2s 轮询状态、可取消，完成后按文件名走下载接口流式取包。
// 文案全部写死（2026-10-26 拍板）：不再提供站点自定义版本，原系统设置 backupGuide 字段退役。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi, mdApi } from '@/api'
import MdView from '@/components/MdView.vue'
import { useUserStore } from '@/stores/user'
import { copyText } from '@/utils/clipboard'
import { BACKUP_DOWNLOAD_LIMIT_BYTES, BACKUP_DOWNLOAD_URL } from '@/types'
import type { BackupStatus } from '@/types'

const userStore = useUserStore()

// 使用说明的渲染缓存（v0.53 渲染收口）：写死 md 常量经 /api/render 渲一次，
// 模块级缓存——同会话重开面板零请求；失败留空（面板主体功能不受影响）
let guideHtmlCache = ''
const guideHtml = ref('')
async function loadGuide() {
  if (guideHtmlCache) {
    guideHtml.value = guideHtmlCache
    return
  }
  try {
    const r = await mdApi.render(BACKUP_GUIDE)
    guideHtmlCache = r.html
    guideHtml.value = r.html
  } catch {
    // 拦截器已 toast；说明区留空
  }
}

// 使用说明（Markdown，写死）：定位是「偶尔查的参考手册」，默认折叠、按小标题速查。
const BACKUP_GUIDE = `## 备份了什么

整站快照，age 加密存放，没有钥匙谁也打不开：

- **每日库备**（soj-backup-db-…）：账号 / 提交 / 班级成绩等天天变的数据，保留 30 份；
- **每周数据备**（soj-backup-media-…）：测试数据与题面图片，保留 4 份；
- 面板按钮 = 全量备份（两者合一，手动兜底用）。

## 自动备份（服务器装一次）

\`\`\`
cd ~/soj && ./scripts/backup.sh --install-cron
\`\`\`

装好即每天 04:17 库备 + 每周日 04:20 数据备。面板「每日库备」红灯（超 25 小时没更新）= 没在跑，上服务器看 backups/cron.log。

## 备份纪律

- 批量导题**当天**，手动点一次「立即备份」；
- 密文多存一处（网盘 + U 盘），恢复钥匙与备份**分开存放**；
- 每季度拿最近一份跑一次校验：\`./scripts/restore.sh <备份文件> --pull-key --verify\`——没验证过恢复的备份等于没备份。

## 在本机查看备份内容（Windows）

1. 下载 age：github.com/FiloSottile/age/releases（解压即用）；高安全模式备好「恢复钥匙.txt」；
2. 在备份文件目录执行：\`age -d -i 恢复钥匙.txt -o backup.tar soj-backup-xxxx.v1.tar.age && tar -xf backup.tar\`
3. 图片 / 题目数据解开后直接看；数据库文件需管理员恢复到站点，或本机起临时 MongoDB 用 Compass 浏览。
`

const backupStatus = ref<BackupStatus | null>(null)

// 每日库备红灯：最近一次 db 备份距今超 25 小时（cron 没跑/失败的最直观信号）
const dbBackupStale = computed(() => {
  const at = backupStatus.value?.last_db_at
  if (!at) return false
  return Date.now() - new Date(at).getTime() > 25 * 3600 * 1000
})
const backupLoading = ref(false)
const backingUp = ref(false)
const showOfflineKey = ref(false)
const offlineKey = ref('')
const keySaved = ref(false)

// 备份任务（v0.26）：running 期 2s 轮询；终态提示一次后停表
const jobState = computed(() => backupStatus.value?.job ?? null)
const jobRunning = computed(() => jobState.value?.state === 'running')
let pollTimer: ReturnType<typeof setInterval> | undefined
let pollFails = 0

function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = undefined
  }
}

function startPoll() {
  stopPoll()
  pollFails = 0
  pollTimer = setInterval(async () => {
    try {
      await loadBackup()
      pollFails = 0
    } catch {
      // 状态查询持续失败（v0.37）：拦截器已在 toast，这里只管停表复位按钮——
      // 原实现轮询永不停，「立即备份」按钮永久转圈；任务本身仍在服务器后台跑
      if (++pollFails >= 5) {
        stopPoll()
        backingUp.value = false
        ElMessage.error('备份状态查询持续失败——请刷新页面查看；任务仍在服务器后台执行')
      }
      return
    }
    const st = jobState.value?.state
    if (st && st !== 'running') {
      stopPoll()
      if (st === 'done') {
        ElMessage.success('备份完成，服务器已留存；小档点文件名下载，大档用「服务器取包」命令')
      } else if (st === 'cancelled') {
        ElMessage.info('备份已取消')
      } else {
        ElMessage.error(`备份失败：${jobState.value?.error || '未知错误'}`)
      }
      backingUp.value = false
    }
  }, 2000)
}

async function runBackup() {
  backingUp.value = true
  try {
    await adminApi.backupRun()
    ElMessage.info('备份任务已启动，万题数据打包可能需要几分钟，可随时取消')
    startPoll()
  } catch {
    backingUp.value = false // 拦截器已提示（如 409 已有任务进行中）
  }
}

async function cancelBackup() {
  await adminApi.backupCancel()
  ElMessage.info('已请求取消')
}

// 体积格式化（KB/MB 一位小数，0 保底）
function fmtSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return ''
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`
}

// 大档在线下载会独占站点公网管道数分钟（2026-10-30 事故），后端已 403 硬禁——
// UI 按体积预判直接给服务器取包命令，不再发起下载（服务端 403 文案同口径兜底）
function oversizeArchive(size?: number): boolean {
  return typeof size === 'number' && size > BACKUP_DOWNLOAD_LIMIT_BYTES
}

const fetchCmdFile = ref('')
const fetchCmdVisible = computed({
  get: () => !!fetchCmdFile.value,
  set: (v: boolean) => {
    if (!v) fetchCmdFile.value = ''
  },
})
const fetchCmd = computed(() =>
  fetchCmdFile.value
    ? `docker cp soj-soj-1:/data/backups/${fetchCmdFile.value} ~/soj/backups/`
    : '',
)

function showFetchCmd(filename: string) {
  fetchCmdFile.value = filename
}

// 下载服务器留存的归档（blob + 独立超时；鉴权头与全局同一身份源）。
// 超限 403 时响应体是 JSON blob，读出文案弹取包命令（旧缓存页面/直连 URL 的兜底）
async function downloadArchive(filename: string) {
  const token = userStore.token
  try {
    const r = await axios({
      method: 'get',
      url: BACKUP_DOWNLOAD_URL,
      params: { file: filename },
      responseType: 'blob',
      timeout: 60_000,
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    const url = URL.createObjectURL(r.data as Blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success(`已下载：${filename}`)
  } catch (e) {
    const resp = (e as { response?: { status?: number; data?: Blob } }).response
    if (resp?.status === 403) {
      const text = await (resp.data instanceof Blob ? resp.data.text() : Promise.resolve(''))
      ElMessage.error(text || '归档超过在线下载体积上限，请在服务器上取包')
      showFetchCmd(filename)
      return
    }
    throw e
  }
}

onMounted(loadBackup)
onMounted(loadGuide) // 使用说明渲染缓存（v0.53 渲染收口：md 常量经 /api/render 渲一次）
onUnmounted(stopPoll)

const backupModeMeta: Record<string, { label: string; type: 'info' | 'warning' | 'success'; desc: string }> = {
  uninitialized: {
    label: '尚未备份过',
    type: 'info',
    desc: '点右上「立即备份」做第一次备份，加密钥匙会自动生成（先保存在服务器上）。',
  },
  tier0: {
    label: '普通模式（钥匙在服务器）',
    type: 'warning',
    desc: '备份已加密，但钥匙也在服务器——整机被入侵时历史备份可能一起泄露，可升级为离线钥匙。',
  },
  tier1: {
    label: '高安全模式（钥匙只在你手里）',
    type: 'success',
    desc: '服务器只有锁、没有钥匙；恢复钥匙丢了 = 所有备份永久打不开，请务必妥善保管。',
  },
}

async function loadBackup() {
  backupLoading.value = true
  try {
    backupStatus.value = await adminApi.backupStatus()
  } finally {
    backupLoading.value = false
  }
}

async function enableOfflineKey() {
  await ElMessageBox.confirm(
    '将生成一把新的「恢复钥匙」：只显示这一次，需要你自己保存好；服务器上从此只有锁、没有钥匙。升级后旧的钥匙和以前下载的加密备份会立即作废，需要马上重新备份一次。确认继续？',
    '升级为离线钥匙（更安全）',
    { type: 'warning', confirmButtonText: '生成并展示恢复钥匙', cancelButtonText: '取消' },
  )
  const r = await adminApi.backupOfflineKey()
  offlineKey.value = r.private_key
  keySaved.value = false
  showOfflineKey.value = true
  await loadBackup()
}

function downloadKey() {
  const blob = new Blob([offlineKey.value], { type: 'text/plain' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'soj-backup-private-key.txt'
  a.click()
  URL.revokeObjectURL(a.href)
}

async function copyKey() {
  copyText(offlineKey.value)
}

onMounted(loadBackup)
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>数据备份</span>
        <template v-if="userStore.can('backup.run')">
          <el-button v-if="jobRunning" type="warning" plain @click="cancelBackup">取消备份</el-button>
          <el-button v-else type="primary" :loading="backingUp" @click="runBackup">
            立即备份（服务器留存）
          </el-button>
        </template>
      </div>
    </template>
    <div v-loading="backupLoading">
      <!-- 状态一行条：模式 + 三类最近备份时间 + 巡检红灯（2026-10-26 精简排版） -->
      <div class="status-row">
        <el-tag
          v-if="backupStatus"
          :type="backupModeMeta[backupStatus.mode]?.type ?? 'info'"
          size="small"
        >
          {{ backupModeMeta[backupStatus.mode]?.label ?? backupStatus.mode }}
        </el-tag>
        <span v-if="backupStatus?.last_backup_at" class="muted">
          上次备份 {{ new Date(backupStatus.last_backup_at).toLocaleString() }}
        </span>
        <span v-else class="muted">还没有备份过</span>
        <template v-if="backupStatus?.last_db_at || backupStatus?.last_media_at">
          <span class="muted">
            每日库备
            <template v-if="backupStatus?.last_db_at">{{ new Date(backupStatus.last_db_at).toLocaleString() }}</template>
            <template v-else>未开启</template>
          </span>
          <span class="muted">
            每周数据备
            <template v-if="backupStatus?.last_media_at">{{ new Date(backupStatus.last_media_at).toLocaleString() }}</template>
            <template v-else>未开启</template>
          </span>
        </template>
        <el-tag v-if="dbBackupStale" type="danger" size="small">库备超 25 小时——自动备份可能没在跑</el-tag>
      </div>
      <!-- 备份任务行（v0.26 任务化）：进行中常驻提示；终态给出产物下载入口 -->
      <div v-if="jobState" class="job-row">
        <template v-if="jobState.state === 'running'">
          <el-tag type="primary" size="small">备份进行中（{{ jobState.tier }}）</el-tag>
          <span class="muted">开始于 {{ new Date(jobState.started_at).toLocaleString() }}，万题数据打包可能需要几分钟</span>
        </template>
        <template v-else-if="jobState.state === 'done' && jobState.file">
          <el-tag type="success" size="small">上次任务完成（{{ jobState.tier }}）</el-tag>
          <template v-if="oversizeArchive(jobState.file_size)">
            <span class="mono muted">{{ jobState.file }}（{{ fmtSize(jobState.file_size) }}）</span>
            <el-button size="small" @click="showFetchCmd(jobState.file!)">服务器取包</el-button>
          </template>
          <el-button v-else link type="primary" @click="downloadArchive(jobState.file!)">
            {{ jobState.file }}<template v-if="jobState.file_size">（{{ fmtSize(jobState.file_size) }}）</template>
          </el-button>
        </template>
        <template v-else-if="jobState.state === 'cancelled'">
          <el-tag type="info" size="small">上次任务已取消</el-tag>
        </template>
        <template v-else>
          <el-tag type="danger" size="small">上次任务失败</el-tag>
          <span class="muted">{{ jobState.error }}</span>
        </template>
      </div>
      <p class="backup-desc">{{ backupModeMeta[backupStatus?.mode ?? 'uninitialized']?.desc }}</p>
      <div v-if="userStore.can('backup.keys') && backupStatus?.mode !== 'tier1'" class="backup-actions">
        <el-button @click="enableOfflineKey">
          升级为离线钥匙（更安全，推荐）
        </el-button>
      </div>

      <el-divider content-position="center">
        <span class="section-title">如何恢复</span>
      </el-divider>
      <div class="tips">
        <div class="cmd-line">
          <code>./scripts/restore.sh soj-backup-xxxx.v1.tar.age --pull-key</code>
          <el-button
            size="small"
            link
            type="primary"
            @click="copyText('./scripts/restore.sh soj-backup-xxxx.v1.tar.age --pull-key')"
          >
            复制
          </el-button>
        </div>
        <div class="cmd-line">
          <code>./scripts/restore.sh soj-backup-xxxx.v1.tar.age 恢复钥匙.txt</code>
          <el-button
            size="small"
            link
            type="primary"
            @click="copyText('./scripts/restore.sh soj-backup-xxxx.v1.tar.age 恢复钥匙.txt')"
          >
            复制
          </el-button>
        </div>
        <p class="cmd-note">第一条 = 普通模式（钥匙在服务器自动取）；第二条 = 高安全模式（用保存的「恢复钥匙.txt」）。把 xxxx 换成实际备份文件名；恢复会覆盖现有数据，脚本会二次确认。</p>
      </div>

      <!-- 使用说明：文案写死，参考手册定位，默认折叠（2026-10-26 精简排版） -->
      <el-collapse class="guide-collapse">
        <el-collapse-item name="guide">
          <template #title>
            <span class="section-title">使用说明（备份了什么 / 自动备份 / 本机查看）</span>
          </template>
          <MdView v-if="guideHtml" :html="guideHtml" class="guide-md" />
        </el-collapse-item>
      </el-collapse>
    </div>

    <el-dialog v-model="fetchCmdVisible" title="服务器取包（大归档不走在线下载）" width="720">
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="数据备/全量备动辄数百 MB，在线下载会长时间占满站点公网管道（学生会全站变卡）——在服务器上执行下面的命令取包，秒级完成"
      />
      <div class="cmd-line" style="margin-top: var(--soj-sp-10)">
        <code>{{ fetchCmd }}</code>
        <el-button size="small" link type="primary" @click="copyText(fetchCmd)">复制</el-button>
      </div>
      <p class="cmd-note">
        在服务器（docker 宿主机）上执行；取回后再从服务器下载或用
        <code>./scripts/backup.sh --offsite</code> 异地留存。每日库备（KB 级）不受限，仍可直接在线下载。
      </p>
    </el-dialog>

    <el-dialog
      v-model="showOfflineKey"
      title="恢复钥匙（只显示这一次）"
      width="760"
      :close-on-click-modal="false"
      @closed="loadBackup()"
    >
      <el-alert
        type="error"
        :closable="false"
        show-icon
        title="这把钥匙丢了 = 所有加密备份永久打不开，服务器上也不会再有第二份"
      />
      <el-input
        :model-value="offlineKey"
        type="textarea"
        :rows="4"
        readonly
        class="mono"
        spellcheck="false"
      />
      <div class="key-actions">
        <el-button size="small" @click="copyKey">复制</el-button>
        <el-button size="small" @click="downloadKey">下载 .txt</el-button>
        <span class="key-warning">建议：存进密码管理器 + 打印一份纸质离线保存，不要只留在服务器上</span>
      </div>
      <template #footer>
        <el-checkbox v-model="keySaved">我已把它保存到至少两个安全的地方</el-checkbox>
        <el-button type="primary" :disabled="!keySaved" @click="showOfflineKey = false">
          我已保存，关闭
        </el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<style scoped>
/* 状态一行条：模式徽章 + 备份时间 + 巡检红灯，窄屏自动换行 */
.status-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-14);
  flex-wrap: wrap;
}

.backup-desc {
  margin: var(--soj-sp-12) 0;
  color: var(--el-text-color-regular);
}

/* 备份任务行（v0.26）：状态徽章 + 提示/下载链接，窄屏自动换行 */
.job-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  flex-wrap: wrap;
  margin-top: var(--soj-sp-10);
}

.backup-actions {
  display: flex;
  gap: var(--soj-sp-8);
}

.cmd-line {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  padding: var(--soj-sp-6) var(--soj-sp-10);
  background: var(--el-fill-color-light);
  border-radius: 6px;
  margin-bottom: var(--soj-sp-6);
}

.cmd-line code {
  flex: 1;
  word-break: break-all;
}

.cmd-note {
  color: var(--el-text-color-secondary);
}

.key-actions {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-top: var(--soj-sp-10);
}

.key-warning {
  color: var(--el-color-danger);
  font-weight: var(--soj-fw-emphasis);
}

/* 使用说明折叠区：标题行去掉手型箭头区的突兀感，编辑按钮不触发展开 */
.guide-collapse {
  margin-top: var(--soj-sp-16);
  border-top: 1px solid var(--el-border-color-lighter);
  border-bottom: none;
}

.guide-collapse :deep(.el-collapse-item__header) {
  height: auto;
  padding: var(--soj-sp-10) 0;
  border-bottom: none;
}

.guide-collapse :deep(.el-collapse-item__wrap) {
  border-bottom: none;
}

</style>

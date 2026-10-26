<script setup lang="ts">
// 数据备份面板（v0.5.4 ② 拆分自 AdminView，v0.4.9/v0.5.1 实现）：
// 状态卡 + 一键备份下载 + Tier 1 私钥一次性弹窗 + 内置使用说明。
// 文案全部写死（2026-10-26 拍板）：不再提供站点自定义版本，原系统设置 backupGuide 字段退役。
import { computed, onMounted, ref } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import MdView from '@/components/MdView.vue'
import { useUserStore } from '@/stores/user'
import { copyText } from '@/utils/clipboard'
import { BACKUP_RUN_URL } from '@/types'
import type { BackupStatus } from '@/types'

const userStore = useUserStore()

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

async function runBackup() {
  backingUp.value = true
  try {
    // 密文归档可能不小且服务端同步生成：独立请求，放宽超时；鉴权头与全局同一身份源
    const token = userStore.token
    const r = await axios({
      method: 'post',
      url: BACKUP_RUN_URL,
      responseType: 'blob',
      timeout: 0,
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    const cd = (r.headers['content-disposition'] as string) ?? ''
    const m = /filename="?([^";]+)"?/.exec(cd)
    const filename = m?.[1] ?? 'soj-backup.tar.age'
    const url = URL.createObjectURL(r.data as Blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success(`备份完成：${filename}（已下载到本机，服务器留存一份）`)
    await loadBackup()
  } finally {
    backingUp.value = false
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
        <el-button
          v-if="userStore.can('backup.run')"
          type="primary"
          :loading="backingUp"
          @click="runBackup"
        >
          立即备份并下载到我的电脑
        </el-button>
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
          <MdView :content="BACKUP_GUIDE" class="guide-md" />
        </el-collapse-item>
      </el-collapse>
    </div>

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
  gap: 14px;
  flex-wrap: wrap;
}

.backup-desc {
  margin: 12px 0;
  color: var(--el-text-color-regular);
}

.backup-actions {
  display: flex;
  gap: 8px;
}

.cmd-line {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: var(--el-fill-color-light);
  border-radius: 6px;
  margin-bottom: 6px;
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
  gap: 8px;
  margin-top: 10px;
}

.key-warning {
  color: var(--el-color-danger);
  font-weight: var(--soj-fw-emphasis);
}

/* 使用说明折叠区：标题行去掉手型箭头区的突兀感，编辑按钮不触发展开 */
.guide-collapse {
  margin-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  border-bottom: none;
}

.guide-collapse :deep(.el-collapse-item__header) {
  height: auto;
  padding: 10px 0;
  border-bottom: none;
}

.guide-collapse :deep(.el-collapse-item__wrap) {
  border-bottom: none;
}

/* 分区标题：区块小标题标准档（16/600） */
.section-title {
  font-size: var(--soj-fs-section);
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-text-color-primary);
}
</style>

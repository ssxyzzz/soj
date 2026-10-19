<script setup lang="ts">
// 数据备份面板（v0.5.4 ② 拆分自 AdminView，v0.4.9/v0.5.1 实现）：
// 状态卡 + 一键备份下载 + Tier 1 私钥一次性弹窗 + 可编辑使用说明。
import { computed, onMounted, ref } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import { useUserStore } from '@/stores/user'
import { copyText } from '@/utils/clipboard'
import { BACKUP_RUN_URL } from '@/types'
import type { BackupStatus } from '@/types'

const userStore = useUserStore()

// 使用说明默认内容（v0.6 起为 HTML 常量；md 原文已随换血转换）：管理员可在面板里编辑
// 并保存为站点自定义版本，清空保存即回退到这里的内容。
const DEFAULT_BACKUP_GUIDE_HTML = [
  '<h3>备份文件里有什么</h3>',
  '<p>整个网站的完整快照：用户账号、题目、提交记录、班级与考试成绩、题面图片。下载到本机的和服务器自动留存的都是加密的（age 格式），没有钥匙谁也打不开。</p>',
  '<h3>备份建议</h3>',
  '<ul><li>点一次「立即备份」= 本机下载一份 + 服务器自动留一份；</li><li>建议每周备份一次，文件多存几处（网盘 + U 盘最稳妥）。</li></ul>',
  '<h3>恢复</h3>',
  '<p>网站出问题时，把备份文件放进 soj 目录，按上方命令交给会命令行的人执行。恢复会把整个网站还原到备份那一刻（覆盖现有数据，脚本会再让你确认一次）。</p>',
  '<h3>在自己的 Windows 电脑上查看备份内容</h3>',
  '<ol><li>下载解密小工具 age：github.com/FiloSottile/age/releases（zip 解压即用，无需安装）；</li><li>准备恢复钥匙：高安全模式＝启用时保存的 txt；普通模式＝找管理员从服务器导出；</li><li>解密并解包（PowerShell，在备份文件目录执行）：</li></ol>',
  '<pre><code>age -d -i 恢复钥匙.txt -o backup.tar soj-backup-xxxx.v1.tar.age\ntar -xf backup.tar</code></pre>',
  '<ol start="4"><li>图片、题目数据解开后直接看；数据库文件（mongo.archive.gz）是二进制，需要管理员恢复到站点查看，或本机起一个临时 MongoDB 后用 Compass 浏览。</li></ol>',
  '<blockquote><p>提醒：恢复钥匙和备份文件不要放在同一台电脑、同一个网盘。</p></blockquote>',
].join('\n')

const backupStatus = ref<BackupStatus | null>(null)
const backupLoading = ref(false)
const backingUp = ref(false)
const showOfflineKey = ref(false)
const offlineKey = ref('')
const keySaved = ref(false)

// 使用说明（可编辑）：空 = 用 DEFAULT_BACKUP_GUIDE；保存在系统设置 backupGuide
const backupGuide = ref('')
const showGuideEdit = ref(false)
const guideDraft = ref('')
const guideHTML = computed(() => (backupGuide.value.trim() ? backupGuide.value : DEFAULT_BACKUP_GUIDE_HTML))

const backupModeMeta: Record<string, { label: string; type: 'info' | 'warning' | 'success'; desc: string }> = {
  uninitialized: {
    label: '尚未备份过',
    type: 'info',
    desc: '不用做任何设置：点下方按钮做第一次备份，系统会自动生成加密钥匙（先保存在服务器上）。',
  },
  tier0: {
    label: '普通模式（加密钥匙存在服务器）',
    type: 'warning',
    desc: '备份文件是加密的，落到别人手里也打不开；但加密钥匙也在服务器上——万一整台服务器被入侵，历史备份可能一起被看光。想更安全，点下方「升级为离线钥匙」。',
  },
  tier1: {
    label: '高安全模式（钥匙只在你手里）',
    type: 'success',
    desc: '服务器上只有锁、没有钥匙：就算整台服务器被入侵，历史备份也解不开。务必保管好启用时保存的那把恢复钥匙——它丢了，所有加密备份就永久打不开了。',
  },
}

async function loadBackup() {
  backupLoading.value = true
  try {
    const st = await adminApi.backupStatus()
    backupStatus.value = st
    backupGuide.value = st.guide_html ?? ''
  } finally {
    backupLoading.value = false
  }
}

function editGuide() {
  // 预填当前展示的内容：从未自定义时即为内置默认，可直接在默认文本上改；
  // 清空再保存仍回退到内置默认（见编辑弹窗标题提示）
  guideDraft.value = guideHTML.value
  showGuideEdit.value = true
}

async function saveGuide() {
  try {
    await adminApi.updateSettings({ backup_guide_html: guideDraft.value })
    // 以服务端持久化结果为准回读刷新（避免本地态与实际存储 diverge 时「看似已保存」）
    await loadBackup()
    showGuideEdit.value = false
    ElMessage.success('使用说明已保存')
  } catch {
    // 拦截器已 toast 具体错误；这里保持弹窗打开，草稿不丢
    ElMessage.error('保存失败，请重试')
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
      <div class="backup-status">
        <el-tag
          v-if="backupStatus"
          :type="backupModeMeta[backupStatus.mode]?.type ?? 'info'"
          size="large"
        >
          {{ backupModeMeta[backupStatus.mode]?.label ?? backupStatus.mode }}
        </el-tag>
        <span v-if="backupStatus?.last_backup_at" class="muted">
          上次备份：{{ new Date(backupStatus.last_backup_at).toLocaleString() }}
        </span>
        <span v-else class="muted">还没有备份过</span>
      </div>
      <p class="backup-desc">{{ backupModeMeta[backupStatus?.mode ?? 'uninitialized']?.desc }}</p>
      <div v-if="userStore.can('backup.keys') && backupStatus?.mode !== 'tier1'" class="backup-actions">
        <el-button @click="enableOfflineKey">
          升级为离线钥匙（更安全，推荐）
        </el-button>
      </div>

      <el-divider content-position="center">
        <span class="section-title">如何恢复（网站出问题时，交给会命令行的人执行）</span>
      </el-divider>
      <div class="tips">
        <p>把下载的备份文件放到本系统文件夹（soj 目录）里，点「复制」后粘贴到终端执行：</p>
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
        <p class="cmd-note">上一条＝普通模式（钥匙在服务器上，自动取用）；下一条＝高安全模式（用启用时保存的「恢复钥匙.txt」，把 xxxx 换成实际备份文件名）。恢复会整体覆盖现有数据，脚本会再让你确认一次。</p>
      </div>

      <el-divider content-position="center">
        <div class="guide-head">
          <span class="section-title">使用说明（如何备份 / 恢复 / 在本机查看）</span>
          <el-button
            v-if="userStore.can('admin.settings')"
            size="small"
            link
            type="primary"
            @click="editGuide"
          >
            编辑
          </el-button>
        </div>
      </el-divider>
      <MdView :content="guideHTML" class="guide-md" />
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

    <el-dialog
      v-model="showGuideEdit"
      title="编辑使用说明（Markdown；清空保存即恢复内置默认）"
      width="900"
      destroy-on-close
    >
      <MdEditor v-model="guideDraft" variant="compact" height="400px" />
      <template #footer>
        <el-button @click="showGuideEdit = false">取消</el-button>
        <el-button type="primary" @click="saveGuide">保存</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<style scoped>
.backup-status {
  display: flex;
  align-items: center;
  gap: 12px;
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

.guide-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

/* 分区标题：区块小标题标准档（16/600） */
.section-title {
  font-size: var(--soj-fs-section);
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-text-color-primary);
}
</style>

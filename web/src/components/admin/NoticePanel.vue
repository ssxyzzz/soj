<script setup lang="ts">
// 公告编辑面板（v0.5.4 ② 拆分自 AdminView；内容 Markdown）。
// v0.24 并入「全站通知广播」：notify.broadcast 权限位持有者可向全员发站内通知（每日频次受限）。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi, discussAdminApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import { useUserStore } from '@/stores/user'
import { NOTICE_MAX_BYTES, contentBytes } from '@/utils/md'

const userStore = useUserStore()
const homeNotice = ref('')
const savingNotice = ref(false)

const bcTitle = ref('')
const bcBody = ref('')
const broadcasting = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    homeNotice.value = s.home_notice
  } catch {
    // 加载失败不阻塞编辑（保存时会整体写入）
  }
})

async function saveNotice() {
  if (contentBytes(homeNotice.value) > NOTICE_MAX_BYTES) {
    ElMessage.error('公告过长（上限 64KB）')
    return
  }
  savingNotice.value = true
  try {
    await adminApi.updateSettings({ home_notice: homeNotice.value })
    ElMessage.success('公告已保存，首页即刻生效')
  } finally {
    savingNotice.value = false
  }
}

async function broadcast() {
  if (!bcTitle.value.trim() || !bcBody.value.trim()) {
    ElMessage.warning('标题与内容不能为空')
    return
  }
  broadcasting.value = true
  try {
    const r = await discussAdminApi.broadcast(bcTitle.value.trim(), bcBody.value.trim())
    ElMessage.success(`已广播给 ${r.count} 名用户`)
    bcTitle.value = ''
    bcBody.value = ''
  } finally {
    broadcasting.value = false
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>首页公告（留空则首页不展示）</span>
        <el-button type="primary" :loading="savingNotice" @click="saveNotice">保存公告</el-button>
      </div>
    </template>
    <MdEditor v-model="homeNotice" height="300px" />
  </el-card>

  <!-- 全站通知广播（v0.24）：与首页公告互补——公告是挂首页的「墙」，广播是推到每个
       学生铃铛里的「信」；受 notify.broadcast 权限位与每日频次约束 -->
  <el-card v-if="userStore.can('notify.broadcast')" style="margin-top: 16px">
    <template #header>
      <div class="head">
        <span>全站通知广播（推送到全员消息中心，受每日频次限制）</span>
        <el-button type="primary" :loading="broadcasting" @click="broadcast">发送广播</el-button>
      </div>
    </template>
    <el-input v-model="bcTitle" maxlength="60" show-word-limit placeholder="标题（60 字内，如：本周六晚 SVM 模拟赛报名开启）" style="margin-bottom: 12px" />
    <el-input
      v-model="bcBody"
      type="textarea"
      :rows="4"
      maxlength="500"
      show-word-limit
      placeholder="正文（500 字内，纯文本）"
    />
  </el-card>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
</style>

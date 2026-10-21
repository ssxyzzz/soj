<script setup lang="ts">
// 公告编辑面板（v0.5.4 ② 拆分自 AdminView；内容 Markdown）
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import { NOTICE_MAX_BYTES, contentBytes } from '@/utils/md'

const homeNotice = ref('')
const savingNotice = ref(false)

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
</template>

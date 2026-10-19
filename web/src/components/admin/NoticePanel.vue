<script setup lang="ts">
// 公告编辑面板（v0.5.4 ② 拆分自 AdminView；v0.6 起内容为富文本 HTML）
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import { NOTICE_MAX_BYTES, richtextBytes } from '@/utils/richtext'

const homeNotice = ref('')
const savingNotice = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    homeNotice.value = s.home_notice_html
  } catch {
    // 加载失败不阻塞编辑（保存时会整体写入）
  }
})

async function saveNotice() {
  if (richtextBytes(homeNotice.value) > NOTICE_MAX_BYTES) {
    ElMessage.error('公告过长（上限 64KB）')
    return
  }
  savingNotice.value = true
  try {
    await adminApi.updateSettings({ home_notice_html: homeNotice.value })
    ElMessage.success('公告已保存，首页即刻生效')
  } finally {
    savingNotice.value = false
  }
}
</script>

<template>
  <el-card>
    <template #header><span>首页公告（留空则首页不展示）</span></template>
    <MdEditor v-model="homeNotice" height="300px" />
    <div class="notice-save">
      <el-button type="primary" :loading="savingNotice" @click="saveNotice">保存公告</el-button>
    </div>
  </el-card>
</template>

<style scoped>
.notice-save {
  margin-top: 12px;
}
</style>

<script setup lang="ts">
// 系统设置面板（v0.5.4 ② 拆分自 AdminView）
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'

const rateLimit = ref(3)
const savingSettings = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    rateLimit.value = s.rate_limit_per_minute
  } catch {
    // 加载失败保留默认值展示
  }
})

async function saveSettings() {
  savingSettings.value = true
  try {
    await adminApi.updateSettings({ rate_limit_per_minute: rateLimit.value })
    ElMessage.success('设置已保存')
  } finally {
    savingSettings.value = false
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>系统设置</span>
        <el-button type="primary" :loading="savingSettings" @click="saveSettings">保存</el-button>
      </div>
    </template>
    <el-form label-position="left" label-width="220px" style="max-width: 520px">
      <el-form-item label="普通用户每分钟提交上限">
        <el-input-number v-model="rateLimit" :min="1" :max="60" />
      </el-form-item>
    </el-form>
  </el-card>
</template>

<script setup lang="ts">
// 系统设置 · 账号与通知页（2026-10-28 拆分）：注册取号起点 / 通知保留 / 全站广播频次。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'

const userSeqStart = ref(100)
const notifyRetentionDays = ref(90)
const notifyBroadcastPerDay = ref(1)

const saving = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    userSeqStart.value = s.user_seq_start ?? 100
    notifyRetentionDays.value = s.notify_retention_days ?? 90
    notifyBroadcastPerDay.value = s.notify_broadcast_per_day ?? 1
  } catch {
    // 加载失败保留默认值展示
  }
})

async function save() {
  saving.value = true
  try {
    await adminApi.updateSettings({
      user_seq_start: userSeqStart.value,
      notify_retention_days: notifyRetentionDays.value,
      notify_broadcast_per_day: notifyBroadcastPerDay.value,
    })
    ElMessage.success('账号与通知设置已保存')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>账号与通知</span>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </template>
    <el-form label-position="left" label-width="220px" style="max-width: 680px">
      <el-divider content-position="left">账号</el-divider>
      <el-form-item label="注册取号起点">
        <el-input-number v-model="userSeqStart" :min="1" :max="1000000000" :controls="false" style="width: 160px" />
        <span class="dim" style="margin-left: 8px">
          自动分配的用户 ID 从该号起（默认 100）；调大即时生效、只前进不回退（计数器已超过起点时从计数器继续）；后台手动指定 ID 不受此限
        </span>
      </el-form-item>

      <el-divider content-position="left">站内通知（v0.24）</el-divider>
      <el-form-item label="通知保留天数">
        <el-input-number v-model="notifyRetentionDays" :min="7" :max="365" />
        <span class="dim" style="margin-left: 8px">站内通知过期自动清理（改动只影响新通知）</span>
      </el-form-item>
      <el-form-item label="全站广播每日上限">
        <el-input-number v-model="notifyBroadcastPerDay" :min="0" :max="100" />
        <span class="dim" style="margin-left: 8px">root 全员公告的每日频次上限（0 = 不限制）</span>
      </el-form-item>
    </el-form>
  </el-card>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
</style>

<script setup lang="ts">
// 系统设置面板（v0.5.4 ② 拆分自 AdminView）
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import { useSiteStore } from '@/stores/site'

const rateLimit = ref(3)
// 每日可下载数据的题目数（0 = 不限制）；作用于拥有「下载数据」权限位的用户
const dataDownloadLimit = ref(5)
// 代码自动补全解锁等级（v0.11）：等级 ≥ 该值才能开启自动补全
const autocompleteMinLevel = ref(5)
// 允许自助注册：关闭后登录页不再出现「注册」入口，建号走用户管理
const allowRegister = ref(true)
// ICP 备案号（v0.13）：备案下来后填入，页脚悬挂并链接工信部备案系统；空 = 不展示
const icpNumber = ref('')
// 批量导入限额（v0.15）：单题数据 / 整包 zip（MB）
const batchProblemMB = ref(100)
const batchZipMB = ref(2048)
const savingSettings = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    rateLimit.value = s.rate_limit_per_minute
    dataDownloadLimit.value = s.data_download_daily_limit
    autocompleteMinLevel.value = s.autocomplete_min_level
    allowRegister.value = s.allow_register
    icpNumber.value = s.icp_number || ''
    batchProblemMB.value = s.batch_import_problem_mb
    batchZipMB.value = s.batch_import_zip_mb
  } catch {
    // 加载失败保留默认值展示
  }
})

async function saveSettings() {
  savingSettings.value = true
  try {
    await adminApi.updateSettings({
      rate_limit_per_minute: rateLimit.value,
      data_download_daily_limit: dataDownloadLimit.value,
      autocomplete_min_level: autocompleteMinLevel.value,
      allow_register: allowRegister.value,
      icp_number: icpNumber.value.trim(),
      batch_import_problem_mb: batchProblemMB.value,
      batch_import_zip_mb: batchZipMB.value,
    })
    ElMessage.success('设置已保存')
    // 备案号改了页脚要跟着变：重置站点公开状态的进程内缓存（下次 ensure 重拉）
    useSiteStore().refresh()
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
      <el-form-item label="每日可下载数据的题目数">
        <el-input-number v-model="dataDownloadLimit" :min="0" :max="1000" />
        <span class="dim" style="margin-left: 8px">
          0 为不限制；限定拥有「下载数据」权限位的用户每天最多下载几道题的数据包（同题重复下载不重复计数）
        </span>
      </el-form-item>
      <el-form-item label="代码自动补全解锁等级">
        <el-input-number v-model="autocompleteMinLevel" :min="1" :max="10" />
        <span class="dim" style="margin-left: 8px">
          学生等级达到该档后才能在设置里开启代码自动补全（1–10）
        </span>
      </el-form-item>
      <el-form-item label="允许自助注册">
        <el-switch v-model="allowRegister" />
        <span class="dim" style="margin-left: 8px">
          关闭后登录页不再提供注册入口，建号请走「用户管理」；已注册账号不受影响
        </span>
      </el-form-item>
      <el-form-item label="ICP 备案号">
        <el-input
          v-model="icpNumber"
          placeholder="如 皖ICP备2026000000号-1（留空不展示）"
          maxlength="64"
          style="max-width: 320px"
        />
        <span class="dim" style="margin-left: 8px">
          备案通过后填入，页脚展示并链接工信部备案系统；留空不展示
        </span>
      </el-form-item>
      <el-form-item label="批量导入单题数据上限（MB）">
        <el-input-number v-model="batchProblemMB" :min="1" :max="4096" />
        <span class="dim" style="margin-left: 8px">
          后台「导入题目」页单道题数据的体积上限，超限该题跳过（v0.15）
        </span>
      </el-form-item>
      <el-form-item label="批量导入整包上限（MB）">
        <el-input-number v-model="batchZipMB" :min="1" :max="8192" />
        <span class="dim" style="margin-left: 8px">
          单次上传题包 zip 的体积上限；题多请分批传
        </span>
      </el-form-item>
    </el-form>
  </el-card>
</template>

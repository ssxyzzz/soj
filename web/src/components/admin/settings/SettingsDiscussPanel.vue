<script setup lang="ts">
// 系统设置 · 讨论区页（2026-10-28 拆分）：三区开关 / 锁区 / 代码门槛 / 限速 / 冷静期 / 折叠 / 激励。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import { useSiteStore } from '@/stores/site'

const discussGlobalEnabled = ref(true)
const discussProblemEnabled = ref(true)
const discussContestEnabled = ref(true)
const discussGlobalLocked = ref(false)
const discussTopicDailyLimit = ref(5)
const discussReplyIntervalSec = ref(30)
const discussNewbieDays = ref(7)
const discussReportFold = ref(3)

const saving = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    discussGlobalEnabled.value = s.discuss_global_enabled ?? true
    discussProblemEnabled.value = s.discuss_problem_enabled ?? true
    discussContestEnabled.value = s.discuss_contest_enabled ?? true
    discussGlobalLocked.value = !!s.discuss_global_locked
    discussTopicDailyLimit.value = s.discuss_topic_daily_limit ?? 5
    discussReplyIntervalSec.value = s.discuss_reply_interval_sec ?? 30
    discussNewbieDays.value = s.discuss_newbie_days ?? 7
    discussReportFold.value = s.discuss_report_fold ?? 3
  } catch {
    // 加载失败保留默认值展示
  }
})

async function save() {
  saving.value = true
  try {
    await adminApi.updateSettings({
      discuss_global_enabled: discussGlobalEnabled.value,
      discuss_problem_enabled: discussProblemEnabled.value,
      discuss_contest_enabled: discussContestEnabled.value,
      discuss_global_locked: discussGlobalLocked.value,
      discuss_topic_daily_limit: discussTopicDailyLimit.value,
      discuss_reply_interval_sec: discussReplyIntervalSec.value,
      discuss_newbie_days: discussNewbieDays.value,
      discuss_report_fold: discussReportFold.value,
    })
    ElMessage.success('讨论区设置已保存')
    // 导航项/入口卡片显隐走公开站点缓存，改完即时生效
    useSiteStore().refresh()
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>讨论区（学习分享 / 题内 / 比赛三区）</span>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </template>
    <el-form label-position="left" label-width="220px" style="max-width: 680px">
      <el-form-item label="导航栏讨论区">
        <el-switch v-model="discussGlobalEnabled" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">全站学习分享区总开关</span>
      </el-form-item>
      <el-form-item label="临时锁定讨论区">
        <el-switch v-model="discussGlobalLocked" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">
          考试期间一键锁区防考题外流（只封导航栏区发帖/入口提示，历史帖仍可读；题内/比赛不受影响）
        </span>
      </el-form-item>
      <el-form-item label="题内讨论">
        <el-switch v-model="discussProblemEnabled" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">题目页「讨论」入口与接口总开关</span>
      </el-form-item>
      <el-form-item label="比赛讨论区">
        <el-switch v-model="discussContestEnabled" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">主题库比赛讨论区总开关（班级考试无讨论区）</span>
      </el-form-item>
      <el-form-item label="每人每日发帖上限">
        <el-input-number v-model="discussTopicDailyLimit" :min="0" :max="100" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">三区合计（0 = 不限制）</span>
      </el-form-item>
      <el-form-item label="回复间隔（秒）">
        <el-input-number v-model="discussReplyIntervalSec" :min="0" :max="3600" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">相邻两次回复的最小间隔（0 = 不限制）</span>
      </el-form-item>
      <el-form-item label="新号冷静期（天）">
        <el-input-number v-model="discussNewbieDays" :min="0" :max="30" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">注册未满该天数不能在导航栏讨论区发主楼（root 不受限；0 = 不限制）</span>
      </el-form-item>
      <el-form-item label="举报自动折叠阈值">
        <el-input-number v-model="discussReportFold" :min="0" :max="100" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">同一内容未处理举报达到该人数自动折叠（0 = 不自动折叠）</span>
      </el-form-item>
    </el-form>
  </el-card>
</template>


<script setup lang="ts">
// 系统设置面板（v0.5.4 ② 拆分自 AdminView；v0.16 并入邮箱通道）
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

// ---- 邮箱通道（v0.16）----
const mailChannel = ref<'log' | 'smtp'>('log')
const smtpHost = ref('')
const smtpPort = ref(465)
const smtpUsername = ref('')
const smtpPassword = ref('')
const smtpFrom = ref('')
const registerVerifyMode = ref<'none' | 'email'>('none')
const passwordResetEnabled = ref(false)

// ---- 讨论区与通知（v0.24）----
const discussGlobalEnabled = ref(true)
const discussProblemEnabled = ref(true)
const discussContestEnabled = ref(true)
const discussGlobalLocked = ref(false)
const discussCodeThreshold = ref(60)
const discussTopicDailyLimit = ref(5)
const discussReplyIntervalSec = ref(30)
const discussNewbieHours = ref(48)
const discussReportFold = ref(3)
const discussAcceptXp = ref(10)
const notifyRetentionDays = ref(90)
const notifyBroadcastPerDay = ref(1)

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
    mailChannel.value = s.mail_channel ?? 'log'
    smtpHost.value = s.smtp_host || ''
    smtpPort.value = s.smtp_port || 465
    smtpUsername.value = s.smtp_username || ''
    smtpPassword.value = s.smtp_password || ''
    smtpFrom.value = s.smtp_from || ''
    registerVerifyMode.value = s.register_verify_mode ?? 'none'
    passwordResetEnabled.value = !!s.password_reset_enabled
    discussGlobalEnabled.value = s.discuss_global_enabled ?? true
    discussProblemEnabled.value = s.discuss_problem_enabled ?? true
    discussContestEnabled.value = s.discuss_contest_enabled ?? true
    discussGlobalLocked.value = !!s.discuss_global_locked
    discussCodeThreshold.value = s.discuss_code_threshold ?? 60
    discussTopicDailyLimit.value = s.discuss_topic_daily_limit ?? 5
    discussReplyIntervalSec.value = s.discuss_reply_interval_sec ?? 30
    discussNewbieHours.value = s.discuss_newbie_hours ?? 48
    discussReportFold.value = s.discuss_report_fold ?? 3
    discussAcceptXp.value = s.discuss_accept_xp ?? 10
    notifyRetentionDays.value = s.notify_retention_days ?? 90
    notifyBroadcastPerDay.value = s.notify_broadcast_per_day ?? 1
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
      // 邮箱通道（v0.16）
      mail_channel: mailChannel.value,
      smtp_host: smtpHost.value.trim(),
      smtp_port: smtpPort.value,
      smtp_username: smtpUsername.value.trim(),
      smtp_password: smtpPassword.value,
      smtp_from: smtpFrom.value.trim(),
      register_verify_mode: registerVerifyMode.value,
      password_reset_enabled: passwordResetEnabled.value,
      // 讨论区与通知（v0.24）
      discuss_global_enabled: discussGlobalEnabled.value,
      discuss_problem_enabled: discussProblemEnabled.value,
      discuss_contest_enabled: discussContestEnabled.value,
      discuss_global_locked: discussGlobalLocked.value,
      discuss_code_threshold: discussCodeThreshold.value,
      discuss_topic_daily_limit: discussTopicDailyLimit.value,
      discuss_reply_interval_sec: discussReplyIntervalSec.value,
      discuss_newbie_hours: discussNewbieHours.value,
      discuss_report_fold: discussReportFold.value,
      discuss_accept_xp: discussAcceptXp.value,
      notify_retention_days: notifyRetentionDays.value,
      notify_broadcast_per_day: notifyBroadcastPerDay.value,
    })
    ElMessage.success('设置已保存')
    // 备案号/注册验证方式/找回开关改了登录页要跟着变：重置站点公开状态缓存
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
    <el-form label-position="left" label-width="220px" style="max-width: 640px">
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

      <el-divider content-position="left">邮箱通道（验证码 / 找回 / 通知）</el-divider>
      <el-form-item label="发信通道">
        <el-radio-group v-model="mailChannel">
          <el-radio-button value="log">log（打日志，人工转告）</el-radio-button>
          <el-radio-button value="smtp">SMTP（真实发信）</el-radio-button>
        </el-radio-group>
        <span class="dim" style="margin-left: 8px">
          log 模式验证码不外发、打服务端日志（开发调试/校内人工转告）；切 smtp 后配置不完整将拒绝启动
        </span>
      </el-form-item>
      <template v-if="mailChannel === 'smtp'">
        <el-form-item label="SMTP 主机">
          <el-input v-model="smtpHost" placeholder="如 smtp.qq.com" style="max-width: 320px" />
        </el-form-item>
        <el-form-item label="SMTP 端口">
          <el-input-number v-model="smtpPort" :min="1" :max="65535" />
          <span class="dim" style="margin-left: 8px">常见 465（SSL）/ 587（STARTTLS）</span>
        </el-form-item>
        <el-form-item label="SMTP 账号">
          <el-input v-model="smtpUsername" style="max-width: 320px" />
        </el-form-item>
        <el-form-item label="SMTP 授权码">
          <el-input
            v-model="smtpPassword"
            type="password"
            show-password
            style="max-width: 320px"
            placeholder="服务商邮箱设置里生成的授权码，非登录密码"
          />
        </el-form-item>
        <el-form-item label="发件人">
          <el-input v-model="smtpFrom" placeholder="如 SOJ <no-reply@example.com>" style="max-width: 320px" />
        </el-form-item>
      </template>
      <el-form-item label="注册验证方式">
        <el-radio-group v-model="registerVerifyMode">
          <el-radio-button value="none">无（现状）</el-radio-button>
          <el-radio-button value="email">邮箱验证码</el-radio-button>
        </el-radio-group>
        <span class="dim" style="margin-left: 8px">
          邮箱模式：注册表单出「邮箱 + 验证码」，邮箱唯一绑定；改完注册页刷新生效
        </span>
      </el-form-item>
      <el-form-item label="忘记密码自助找回">
        <el-switch v-model="passwordResetEnabled" />
        <span class="dim" style="margin-left: 8px">
          开启后登录页出现「忘记密码」入口（验证码重置，防枚举）；关闭时学生找回请联系老师重置
        </span>
      </el-form-item>

      <el-divider content-position="left">讨论区（学习分享 / 题内 / 比赛三区）</el-divider>
      <el-form-item label="导航栏讨论区">
        <el-switch v-model="discussGlobalEnabled" />
        <span class="dim" style="margin-left: 8px">全站学习分享区总开关</span>
      </el-form-item>
      <el-form-item label="临时锁定讨论区">
        <el-switch v-model="discussGlobalLocked" />
        <span class="dim" style="margin-left: 8px">
          考试期间一键锁区防考题外流（只封导航栏区发帖/入口提示，历史帖仍可读；题内/比赛不受影响）
        </span>
      </el-form-item>
      <el-form-item label="题内讨论">
        <el-switch v-model="discussProblemEnabled" />
        <span class="dim" style="margin-left: 8px">题目页「讨论」入口与接口总开关</span>
      </el-form-item>
      <el-form-item label="比赛讨论区">
        <el-switch v-model="discussContestEnabled" />
        <span class="dim" style="margin-left: 8px">主题库比赛讨论区总开关（班级考试无讨论区）</span>
      </el-form-item>
      <el-form-item label="讨论代码门槛（分）">
        <el-input-number v-model="discussCodeThreshold" :min="0" :max="100" />
        <span class="dim" style="margin-left: 8px">
          题内讨论代码块：本题最高得分 ≥ 该值才可见（0 = 不隐藏）；每题可在编辑页覆盖
        </span>
      </el-form-item>
      <el-form-item label="每人每日发帖上限">
        <el-input-number v-model="discussTopicDailyLimit" :min="0" :max="100" />
        <span class="dim" style="margin-left: 8px">三区合计（0 = 不限制）</span>
      </el-form-item>
      <el-form-item label="回复间隔（秒）">
        <el-input-number v-model="discussReplyIntervalSec" :min="0" :max="3600" />
        <span class="dim" style="margin-left: 8px">相邻两次回复的最小间隔（0 = 不限制）</span>
      </el-form-item>
      <el-form-item label="新号冷静期（小时）">
        <el-input-number v-model="discussNewbieHours" :min="0" :max="720" />
        <span class="dim" style="margin-left: 8px">注册未满该时长不能在导航栏讨论区发主楼（0 = 不限制）</span>
      </el-form-item>
      <el-form-item label="举报自动折叠阈值">
        <el-input-number v-model="discussReportFold" :min="0" :max="100" />
        <span class="dim" style="margin-left: 8px">同一内容未处理举报达到该人数自动折叠（0 = 不自动折叠）</span>
      </el-form-item>
      <el-form-item label="采纳经验奖励">
        <el-input-number v-model="discussAcceptXp" :min="0" :max="1000" />
        <span class="dim" style="margin-left: 8px">回复被楼主采纳加的经验值（0 = 不奖励）</span>
      </el-form-item>
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

<script setup lang="ts">
// 系统设置 · 注册与邮箱页（2026-10-28 拆分）：自助注册开关 / 验证方式 / 找回开关 / SMTP 通道。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import { useSiteStore } from '@/stores/site'

const allowRegister = ref(true)
const registerVerifyMode = ref<'none' | 'email'>('none')
const passwordResetEnabled = ref(false)

const mailChannel = ref<'log' | 'smtp'>('log')
const smtpHost = ref('')
const smtpPort = ref(465)
const smtpUsername = ref('')
const smtpPassword = ref('')
const smtpFrom = ref('')

const saving = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    allowRegister.value = s.allow_register
    registerVerifyMode.value = s.register_verify_mode ?? 'none'
    passwordResetEnabled.value = !!s.password_reset_enabled
    mailChannel.value = s.mail_channel ?? 'log'
    smtpHost.value = s.smtp_host || ''
    smtpPort.value = s.smtp_port || 465
    smtpUsername.value = s.smtp_username || ''
    smtpPassword.value = s.smtp_password || ''
    smtpFrom.value = s.smtp_from || ''
  } catch {
    // 加载失败保留默认值展示
  }
})

async function save() {
  saving.value = true
  try {
    await adminApi.updateSettings({
      allow_register: allowRegister.value,
      register_verify_mode: registerVerifyMode.value,
      password_reset_enabled: passwordResetEnabled.value,
      mail_channel: mailChannel.value,
      smtp_host: smtpHost.value.trim(),
      smtp_port: smtpPort.value,
      smtp_username: smtpUsername.value.trim(),
      smtp_password: smtpPassword.value,
      smtp_from: smtpFrom.value.trim(),
    })
    ElMessage.success('注册与邮箱设置已保存')
    // 注册开关/验证方式/找回开关改了登录页要跟着变：重置站点公开状态缓存
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
        <span>注册与邮箱</span>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </template>
    <el-form label-position="left" label-width="220px" style="max-width: 680px">
      <el-form-item label="允许自助注册">
        <el-switch v-model="allowRegister" />
        <span class="dim" style="margin-left: 8px">
          关闭后登录页不再提供注册入口，建号请走「用户管理」；已注册账号不受影响
        </span>
      </el-form-item>
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

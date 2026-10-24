<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { authApi } from '@/api'
import { verifyApi } from '@/api/verify'
import { useSiteStore } from '@/stores/site'
import { useUserStore } from '@/stores/user'

const router = useRouter()
const route = useRoute()
const userStore = useUserStore()
const site = useSiteStore()

const mode = ref<'login' | 'register'>('login')
const loading = ref(false)
const form = reactive({ username: '', password: '', nickname: '', email: '', code: '' })

// 注册开关（后台系统设置）：关闭时隐藏「注册」页签，并兜底切回登录
site.ensure().then(() => {
  if (!site.registerOpen) mode.value = 'login'
})

// ---- 发码倒计时（注册与忘记密码各一套）----
function useCountdown() {
  const seconds = ref(0)
  let timer: ReturnType<typeof setInterval> | undefined
  function start() {
    seconds.value = 60
    timer = setInterval(() => {
      if (--seconds.value <= 0) {
        clearInterval(timer)
        timer = undefined
      }
    }, 1000)
  }
  return { seconds, start }
}
const regCd = useCountdown()
const fpCd = useCountdown()

const emailMode = computed(() => site.registerVerifyMode === 'email')

async function sendRegisterCode() {
  if (!form.email.trim()) {
    ElMessage.warning('请先输入邮箱')
    return
  }
  const r = await verifyApi.sendCode({ purpose: 'register', email: form.email.trim() })
  ElMessage.success(r.message)
  regCd.start()
}

async function submit() {
  if (!form.username || !form.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  if (mode.value === 'register' && emailMode.value && (!form.email.trim() || !form.code.trim())) {
    ElMessage.warning('请输入邮箱并填写验证码')
    return
  }
  loading.value = true
  try {
    const r =
      mode.value === 'login'
        ? await authApi.login(form.username, form.password)
        : await authApi.register(
            form.username,
            form.password,
            form.nickname,
            emailMode.value ? form.email.trim() : undefined,
            emailMode.value ? form.code.trim() : undefined,
          )
    userStore.setLogin(r.token, r.user.nickname || r.user.username, r.user.role, r.user.perms ?? [], r.user.id)
    ElMessage.success(mode.value === 'login' ? '登录成功' : '注册成功')
    router.push((route.query.redirect as string) || '/')
  } catch {
    // 错误提示已由 axios 拦截器统一弹出
  } finally {
    loading.value = false
  }
}

// ---- 忘记密码（v0.16 自助找回；入口仅后台开启后展示）----
const fpOpen = ref(false)
const fpForm = reactive({ account: '', code: '', password: '', password2: '' })
const fpSending = ref(false)
const fpLoading = ref(false)

function openForgot() {
  Object.assign(fpForm, { account: '', code: '', password: '', password2: '' })
  fpOpen.value = true
}

async function sendForgotCode() {
  if (!fpForm.account.trim()) {
    ElMessage.warning('请输入用户名或邮箱')
    return
  }
  fpSending.value = true
  try {
    // 防枚举：无论账号是否存在，统一按「已发送」提示；账号（用户名/邮箱）由后端解析出绑定邮箱
    const r = await verifyApi.sendCode({ purpose: 'reset', email: fpForm.account.trim() })
    ElMessage.success(r.message)
    fpCd.start()
  } catch {
    // 拦截器统一提示
  } finally {
    fpSending.value = false
  }
}

async function submitForgot() {
  if (!fpForm.code.trim() || !fpForm.password) {
    ElMessage.warning('请填写验证码和新密码')
    return
  }
  if (fpForm.password !== fpForm.password2) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }
  fpLoading.value = true
  try {
    await verifyApi.resetPassword(fpForm.account.trim(), fpForm.code.trim(), fpForm.password)
    ElMessage.success('密码已重置，请使用新密码登录')
    fpOpen.value = false
    mode.value = 'login'
  } catch {
    // 拦截器统一提示
  } finally {
    fpLoading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <el-card class="card">
      <template #header>
        <div class="tabs">
          <el-radio-group v-model="mode">
            <el-radio-button value="login">登录</el-radio-button>
            <el-radio-button v-if="site.registerOpen" value="register">注册</el-radio-button>
          </el-radio-group>
        </div>
      </template>
      <el-form label-position="top" @keyup.enter="submit">
        <el-form-item label="用户名">
          <el-input v-model="form.username" placeholder="3-32 位字母/数字/下划线" />
        </el-form-item>
        <el-form-item v-if="mode === 'register'" label="昵称（可选）">
          <el-input v-model="form.nickname" placeholder="默认与用户名相同" />
        </el-form-item>
        <template v-if="mode === 'register' && emailMode">
          <el-form-item label="邮箱">
            <el-input v-model="form.email" placeholder="用于接收验证码" />
          </el-form-item>
          <el-form-item label="邮箱验证码">
            <div class="code-row">
              <el-input v-model="form.code" placeholder="6 位数字" />
              <el-button :disabled="regCd.seconds.value > 0" @click="sendRegisterCode">
                {{ regCd.seconds.value > 0 ? `${regCd.seconds.value}s 后重发` : '获取验证码' }}
              </el-button>
            </div>
          </el-form-item>
        </template>
        <el-form-item :label="mode === 'register' ? '密码（至少 8 位）' : '密码'">
          <el-input v-model="form.password" type="password" show-password placeholder="至少 8 位" />
        </el-form-item>
        <el-button type="primary" plain class="submit" :loading="loading" @click="submit">
          {{ mode === 'login' ? '登录' : '注册' }}
        </el-button>
        <div v-if="mode === 'login' && site.passwordResetEnabled" class="forgot-row">
          <el-link type="primary" :underline="false" @click="openForgot">忘记密码？</el-link>
        </div>
      </el-form>
    </el-card>

    <!-- 忘记密码：用户名/邮箱 → 发码（防枚举统一提示）→ 验证码 + 新密码×2 -->
    <el-dialog v-model="fpOpen" title="找回密码" width="420px">
      <el-form label-position="top">
        <el-form-item label="用户名或邮箱">
          <el-input v-model="fpForm.account" placeholder="绑定邮箱优先，填用户名则向其绑定邮箱发码" />
        </el-form-item>
        <el-form-item label="邮箱验证码">
          <div class="code-row">
            <el-input v-model="fpForm.code" placeholder="6 位数字" />
            <el-button :loading="fpSending" :disabled="fpCd.seconds.value > 0" @click="sendForgotCode">
              {{ fpCd.seconds.value > 0 ? `${fpCd.seconds.value}s 后重发` : '获取验证码' }}
            </el-button>
          </div>
        </el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="fpForm.password" type="password" show-password placeholder="至少 8 位" />
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input v-model="fpForm.password2" type="password" show-password placeholder="再次输入" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="fpOpen = false">取消</el-button>
        <el-button type="primary" :loading="fpLoading" @click="submitForgot">重置密码</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-bg-color-page);
}

.card {
  width: 380px;
}

.tabs {
  text-align: center;
}

.submit {
  width: 100%;
}

.code-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.forgot-row {
  text-align: right;
  margin-top: 10px;
}
</style>

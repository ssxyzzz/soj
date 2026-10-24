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
  <!-- v0.19 登录页（web-design-v3.md §4.2）：全屏居中白卡，无导航；
       背景透出 body 三层画布（渐变 + 光晕 + 青网格），逻辑与旧版零差异 -->
  <div class="login-page">
    <div class="login-card">
      <div class="login-brand">
        <span class="brand-mark">S</span>
        <span class="brand-word">SOJ</span>
        <span class="login-sub">登录 SOJ，开始你的刷题之旅</span>
      </div>
      <div class="tabs">
        <el-radio-group v-model="mode">
          <el-radio-button value="login">登录</el-radio-button>
          <el-radio-button v-if="site.registerOpen" value="register">注册</el-radio-button>
        </el-radio-group>
      </div>
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
        <el-button type="primary" class="submit" :loading="loading" @click="submit">
          {{ mode === 'login' ? '登 录' : '注 册' }}
        </el-button>
        <div v-if="mode === 'login' && site.passwordResetEnabled" class="forgot-row">
          <el-link type="primary" :underline="false" @click="openForgot">忘记密码？</el-link>
        </div>
      </el-form>
    </div>

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
  padding: 48px 20px;
  /* v0.19：透出全局三层画布（原 --el-bg-color-page 实底移除） */
  background: transparent;
}

.login-card {
  width: 400px;
  max-width: 100%;
  background: #fff;
  border: 1px solid var(--soj-line);
  border-radius: 20px;
  padding: 34px 34px 30px;
  box-shadow:
    0 1px 2px rgba(11, 31, 69, 0.04),
    0 18px 48px rgba(11, 31, 69, 0.1);
}

.login-brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 11px;
  margin-bottom: 22px;
  text-align: center;
}

/* 品牌双色（与顶栏同款；此处独立 scoped 复刻一份，登录页无顶栏上下文） */
.brand-mark {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-teal));
  color: #fff;
  font-size: 18px;
  font-weight: var(--soj-fw-bold);
  box-shadow: 0 8px 20px rgba(36, 85, 218, 0.3);
}

.brand-word {
  font-size: 27px;
  font-weight: var(--soj-fw-bold);
  letter-spacing: 1px;
  background: linear-gradient(
    90deg,
    #16a7d2 0%,
    #1557b8 25%,
    #2c5de0 50%,
    #16a7d2 75%,
    #1557b8 100%
  );
  background-size: 220% 100%;
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  animation: brand-flow 7s linear infinite;
}

@keyframes brand-flow {
  to {
    background-position: 220% 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .brand-word {
    animation: none;
    background-position: 50% 0;
  }
}

.login-sub {
  font-size: var(--soj-fs-small);
  color: var(--el-text-color-secondary);
}

.tabs {
  display: flex;
  justify-content: center;
  margin-bottom: 20px;
}

/* 分段切换胶囊化（登录/注册） */
.tabs :deep(.el-radio-button__inner) {
  border-radius: 0;
  padding: 9px 26px;
}

.tabs :deep(.el-radio-button:first-child .el-radio-button__inner) {
  border-radius: 999px 0 0 999px;
}

.tabs :deep(.el-radio-button:last-child .el-radio-button__inner) {
  border-radius: 0 999px 999px 0;
}

.submit {
  width: 100%;
  height: 46px;
  font-size: var(--soj-fs-section);
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

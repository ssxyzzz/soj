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
  <!-- v3-full 登录页：全屏居中白卡、无导航；页面自带独立画布（spot 中心偏上 + 网格 mask 38%），
       覆盖全局 body 网格层（自有渐变底不透）；左上返回首页胶囊 -->
  <div class="login-page">
    <button class="login-back" type="button" @click="router.push('/')">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M15 6l-6 6 6 6" /></svg>
      返回首页
    </button>
    <div class="login-stage">
      <div class="login-card">
        <div class="login-brand">
          <img class="brand-mark" src="/favicon.svg" alt="SOJ" />
          <span class="brand-word">SOJ</span>
          <span class="login-sub">登录 SOJ，开始你的刷题之旅</span>
        </div>
        <div v-if="site.registerOpen" class="seg-row">
          <div class="soj-seg">
            <button type="button" :class="{ on: mode === 'login' }" @click="mode = 'login'">登录</button>
            <button type="button" :class="{ on: mode === 'register' }" @click="mode = 'register'">注册</button>
          </div>
        </div>
        <el-form label-position="top" @keyup.enter="submit">
          <el-form-item label="用户名 / 昵称">
            <el-input v-model="form.username" placeholder="请输入用户名" />
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
          <el-form-item :label="mode === 'register' ? '密码（8-64 位，不含弱口令）' : '密码'">
            <el-input v-model="form.password" type="password" show-password placeholder="至少 8 位" />
          </el-form-item>
          <div v-if="mode === 'login'" class="login-row">
            <span class="dim">验证码登录请联系管理员</span>
            <el-link v-if="site.passwordResetEnabled" type="primary" :underline="false" @click="openForgot">忘记密码？</el-link>
          </div>
          <el-button type="primary" class="submit" :loading="loading" @click="submit">
            {{ mode === 'login' ? '登 录' : '注 册' }}
          </el-button>
          <p v-if="site.registerOpen" class="login-alt">
            {{ mode === 'login' ? '还没有账号？' : '已有账号？' }}
            <a @click="mode = mode === 'login' ? 'register' : 'login'">{{ mode === 'login' ? '注册新账号' : '去登录' }}</a>
          </p>
        </el-form>
      </div>
      <div class="login-foot">
        SOJ · Gin + Vue 3 + MongoDB + go-judge 在线判题系统<br />
        <a v-if="site.icpNumber" href="https://beian.miit.gov.cn/" target="_blank" rel="noopener noreferrer">{{ site.icpNumber }}</a>
      </div>
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
/* v3-full #scr-login：独立画布——冷白渐变（不透，盖住全局 body 网格层）+ 中心偏上青光晕；
   网格层在 ::after（mask 椭圆 90% 70% at 50% 38%，与全局 50% 0% 版区分） */
.login-page {
  position: relative;
  min-height: 100vh;
  background:
    radial-gradient(520px 340px at 50% 16%, rgb(var(--soj-teal-soft-rgb) / 0.14), transparent 62%),
    linear-gradient(var(--soj-bg-top), var(--soj-bg-bottom));
}

.login-page::after {
  content: '';
  position: fixed;
  inset: 0;
  pointer-events: none;
  background:
    linear-gradient(rgb(var(--soj-teal-soft-rgb) / 0.11) 1px, transparent 1px),
    linear-gradient(90deg, rgb(var(--soj-teal-soft-rgb) / 0.11) 1px, transparent 1px);
  background-size: 44px 44px;
  -webkit-mask-image: radial-gradient(ellipse 90% 70% at 50% 38%, #000 26%, transparent 80%);
  mask-image: radial-gradient(ellipse 90% 70% at 50% 38%, #000 26%, transparent 80%);
}

/* v3 .login-back：左上返回首页胶囊（白.8 + blur） */
.login-back {
  position: fixed;
  top: 22px;
  left: 26px;
  z-index: 2;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font: inherit;
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-ink-2);
  padding: 8px 16px;
  border: 1px solid var(--soj-line);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(8px);
  cursor: pointer;
  transition: all 0.15s;
}

.login-back:hover {
  color: var(--soj-blue);
  border-color: var(--soj-blue-line);
}

.login-stage {
  position: relative;
  z-index: 1;
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 48px 20px;
}

.login-card {
  width: 400px;
  max-width: 100%;
  background: #fff;
  border: 1px solid var(--soj-line);
  border-radius: 20px;
  padding: 36px 34px 30px;
  box-shadow:
    0 1px 2px rgba(11, 31, 69, 0.04),
    0 18px 48px rgba(11, 31, 69, 0.1);
}

.login-brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  margin-bottom: 26px;
  text-align: center;
}

/* 品牌标志（与顶栏同款放大版；此处独立 scoped 复刻一份，登录页无顶栏上下文） */
.brand-mark {
  width: 46px;
  height: 46px;
  border-radius: 14px;
}

.brand-word {
  font-size: 30px;
  font-weight: var(--soj-fw-heavy);
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
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

/* 登录/注册分段：v3 .seg 全局件（居中） */
.seg-row {
  display: flex;
  justify-content: center;
  margin-bottom: 20px;
}

/* v3 字段口径：label 13/600 ink-2，输入 46 高圆角 12（聚焦环走全局规则） */
.login-card :deep(.el-form-item__label) {
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-ink-2);
}

.login-card :deep(.el-input) {
  --el-input-height: 46px;
}

/* v3 .login-row：左灰提示 + 右忘记密码链接 */
.login-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: var(--soj-fs-13);
  margin: 2px 0 14px;
}

/* v3 .login-btn：48 高胶囊渐变主钮 */
.submit {
  width: 100%;
  height: 48px;
  font-size: var(--soj-fs-16);
}

.login-alt {
  margin: 18px 0 0;
  text-align: center;
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

.login-alt a {
  color: var(--soj-blue);
  font-weight: var(--soj-fw-bold);
  cursor: pointer;
}

.login-alt a:hover {
  text-decoration: underline;
}

/* v3 .login-foot：卡片下方两行 12px 页脚 */
.login-foot {
  margin-top: 26px;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  text-align: center;
  line-height: 2;
}

.login-foot a {
  color: var(--soj-ink-3);
  text-decoration: none;
}

.login-foot a:hover {
  color: var(--soj-blue);
}

.code-row {
  display: flex;
  gap: 8px;
  width: 100%;
}
</style>

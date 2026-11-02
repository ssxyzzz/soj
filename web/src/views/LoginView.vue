<script setup lang="ts">
import { computed, onScopeDispose, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { authApi } from '@/api'
import { verifyApi } from '@/api/verify'
import AmbientGrid from '@/components/AmbientGrid.vue'
import HomeAmbient from '@/components/HomeAmbient.vue'
import SiteFooterLine from '@/components/SiteFooterLine.vue'
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
  // v0.27：发码后 60s 内登录成功跳走是最常见路径，卸载时清掉残留倒计时
  onScopeDispose(() => {
    if (timer !== undefined) clearInterval(timer)
  })
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
  <!-- v3-full 登录页：全屏居中白卡、无导航；画布 = 全局冷白底 + AmbientGrid（首页同款
       「网格在鼠标处显影」，2026-11-02 用户拍板替换原自带青晕+网格 mask）；结构 = 列弹性：
       stage 吃满剩余空间居中卡片，页脚钉在页面底部，整页恰满一屏无滚动条 -->
  <div class="login-page">
    <AmbientGrid />
    <HomeAmbient />
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
    </div>
    <div class="login-foot">
      <!-- 页脚信息行（2026-11-02）：SiteFooterLine 共用组件，与登录后主布局页脚同款单行 -->
      <SiteFooterLine />
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
/* 画布（2026-11-02 用户拍板）：撤掉自带冷白渐变+青晕+网格 mask（旧版盖住全局画布，
   顶部青晕在宽视口下呈截断色块），改为挂载 AmbientGrid —— 首页同款「网格在鼠标处
   显影」（组件自管 body.ambient-on 撤全局静态光晕/网格）；reduced-motion 时组件
   不启用，回落全局静态画布，与其余页面一致。布局 = 列弹性恰满一屏：stage 吃剩余
   空间居中卡片，页脚钉在底部，常规视口无滚动条 */
.login-page {
  position: relative;
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}

/* v3 .login-back：左上返回首页胶囊（白.8 + blur） */
.login-back {
  position: fixed;
  top: 22px;
  left: 26px;
  z-index: 2;
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-8);
  font: inherit;
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-ink-2);
  padding: var(--soj-sp-8) var(--soj-sp-16);
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
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: var(--soj-sp-32) var(--soj-sp-20);
}

.login-card {
  width: 400px;
  max-width: 100%;
  background: var(--soj-white);
  border: 1px solid var(--soj-line);
  border-radius: var(--soj-radius-dialog); /* 登录卡 = 弹窗档圆角 20 */
  padding: var(--soj-sp-32) var(--soj-sp-32) var(--soj-sp-32);
  box-shadow:
    0 1px 2px rgba(11, 31, 69, 0.04),
    0 18px 48px rgba(11, 31, 69, 0.1);
}

.login-brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--soj-sp-12);
  margin-bottom: var(--soj-sp-24);
  text-align: center;
}

/* 品牌标志（与顶栏同款放大版；此处独立 scoped 复刻一份，登录页无顶栏上下文） */
.brand-mark {
  width: 46px;
  height: 46px;
  border-radius: 14px;
}

.brand-word {
  font-size: var(--soj-fs-30);
  font-weight: var(--soj-fw-heavy);
  letter-spacing: 1px;
  /* 品牌渐变三停走 :root token（与 MainLayout 装饰条同源，v0.57 收口） */
  background: linear-gradient(
    90deg,
    var(--soj-brand-a) 0%,
    var(--soj-brand-b) 25%,
    var(--soj-brand-c) 50%,
    var(--soj-brand-a) 75%,
    var(--soj-brand-b) 100%
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
  margin: var(--soj-sp-2) 0 var(--soj-sp-14);
}

/* v3 .login-btn：48 高胶囊渐变主钮 */
.submit {
  width: 100%;
  height: 48px;
  font-size: var(--soj-fs-16);
}

.login-alt {
  margin: var(--soj-sp-16) 0 0;
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

/* v3 .login-foot：钉在页面底部的页脚（列弹性布局的最末元素）；信息行由
   SiteFooterLine 共用组件渲染（与登录后主布局页脚同款单行），此处只管钉底与字号 */
.login-foot {
  padding: 0 var(--soj-sp-20) 22px;
  font-size: var(--soj-fs-12);
  text-align: center;
}

.code-row {
  display: flex;
  gap: var(--soj-sp-8);
  width: 100%;
}
</style>

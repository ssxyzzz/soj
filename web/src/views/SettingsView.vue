<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'

import { authApi } from '@/api'
import { verifyApi } from '@/api/verify'
import { useLevelsStore } from '@/stores/levels'
import { useUserStore } from '@/stores/user'
import { autocompleteAvailable, languageOptions, mergedPrefs } from '@/utils/code'

// 个人设置（v0.4.8）：本人自助字段（昵称/性别/学校/年级/QQ）+ 偏好（默认语言/代码模板）。
// 昵称是否可改由后端按 user.nickname.edit 权限位现算（can_edit_nickname），默认角色默认开启。
const router = useRouter()
const userStore = useUserStore()
const levels = useLevelsStore()
levels.ensure() // 补全解锁等级提示（/api/levels 公开配置）
const loading = ref(false)
const savingProfile = ref(false)
const savingPrefs = ref(false)

const nickname = ref('')
const canEditNickname = ref(false)

// '' 表示未填写（后端显式空串=清空）
const profileForm = reactive({ gender: '', school: '', grade: '', qq: '' })

const prefsForm = reactive<{
  defaultLang: string
  codeTemplates: Record<string, string>
  autocomplete: boolean
  mdEditMode: 'wysiwyg' | 'ir' | 'sv'
  mdTheme: 'light' | 'ant-design' | 'dark' | 'wechat'
}>({ defaultLang: '', codeTemplates: {}, autocomplete: true, mdEditMode: 'wysiwyg', mdTheme: 'light' })

// 模板编辑器当前编辑的语言（按语言各存一套，切换带出对应模板）
const tplLang = ref('cpp')

async function load() {
  loading.value = true
  try {
    const p = await authApi.getProfile()
    nickname.value = p.nickname ?? ''
    canEditNickname.value = p.can_edit_nickname
    profileForm.gender = p.gender ?? ''
    profileForm.school = p.school ?? ''
    profileForm.grade = p.grade ?? ''
    profileForm.qq = p.qq ?? ''
    prefsForm.defaultLang = p.prefs?.defaultLang ?? ''
    prefsForm.codeTemplates = { ...(p.prefs?.codeTemplates ?? {}) }
    userStore.setLevel(p.level ?? null) // 顶栏徽章与补全门槛同步（AC 后进设置页即刷新）
    // 开关对未达标用户是禁用的：显示有效值（关），而不是库里可能残留的 true
    prefsForm.autocomplete = autocompleteAvailable() ? (p.prefs?.autocomplete ?? true) : false
    prefsForm.mdEditMode = (p.prefs?.mdEditMode || 'wysiwyg') as typeof prefsForm.mdEditMode
    prefsForm.mdTheme = (p.prefs?.mdTheme || 'light') as typeof prefsForm.mdTheme
  } finally {
    loading.value = false
  }
}

async function saveProfile() {
  if (canEditNickname.value && !nickname.value.trim()) {
    ElMessage.warning('昵称不能为空')
    return
  }
  savingProfile.value = true
  try {
    const p = await authApi.updateProfile({
      ...(canEditNickname.value ? { nickname: nickname.value.trim() } : {}),
      gender: profileForm.gender,
      school: profileForm.school,
      grade: profileForm.grade,
      qq: profileForm.qq,
    })
    nickname.value = p.nickname ?? nickname.value
    if (p.nickname) userStore.setNickname(p.nickname) // 顶栏即时更新
    ElMessage.success('个人信息已保存')
  } finally {
    savingProfile.value = false
  }
}

async function savePrefs() {
  savingPrefs.value = true
  try {
    // 全量合成后整体覆盖（避免清掉在线编程保存的字体/字号），先乐观更新本地再落库
    const merged = mergedPrefs({
      defaultLang: prefsForm.defaultLang,
      codeTemplates: prefsForm.codeTemplates,
      autocomplete: prefsForm.autocomplete,
      mdEditMode: prefsForm.mdEditMode,
      mdTheme: prefsForm.mdTheme,
    })
    userStore.setPrefs(merged)
    const p = await authApi.updateProfile({ prefs: merged })
    ElMessage.success('偏好已保存，提交弹窗 / 在线编程 / 编辑器即刻生效')
    userStore.setPrefs(p.prefs ?? merged) // 本次会话立即生效（布局层只在挂载时拉取）
  } finally {
    savingPrefs.value = false
  }
}

onMounted(() => {
  load()
  loadEmail()
})

// ---- 修改密码（v0.14）：旧密码 + 新密码两次确认；成功后全部登录态失效，强制重新登录 ----
const savingPwd = ref(false)
const pwdForm = reactive({ old: '', new: '', confirm: '' })

async function changePassword() {
  if (!pwdForm.old || !pwdForm.new || !pwdForm.confirm) {
    ElMessage.warning('请完整填写原密码与新密码')
    return
  }
  if (pwdForm.new.length < 8) {
    ElMessage.warning('新密码至少 8 位')
    return
  }
  if (pwdForm.new !== pwdForm.confirm) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }
  if (pwdForm.new === pwdForm.old) {
    ElMessage.warning('新密码不能与原密码相同')
    return
  }
  savingPwd.value = true
  try {
    await authApi.changePassword(pwdForm.old, pwdForm.new)
    ElMessage.success('密码已修改，全部登录态已失效，请重新登录')
    userStore.logout()
    router.push('/login')
    return // 已跳登录页，不再清表单
  } finally {
    savingPwd.value = false
    pwdForm.old = ''
    pwdForm.new = ''
    pwdForm.confirm = ''
  }
}

// ---- 绑定/换绑邮箱（v0.16）----
// 无绑定：邮箱 + 新邮箱验证码。换绑：先验证旧邮箱（密码或旧邮箱验证码二选一），
// 再向新邮箱发码——旧邮箱验证在发码请求里内联提交（后端发码前校验）。
const email = ref('')
const bindOpen = ref(false)
const bindCd = ref(0)
let bindTimer: ReturnType<typeof setInterval> | undefined
const bindSending = ref(false)
const bindLoading = ref(false)
const bindForm = reactive({
  email: '',
  code: '',
  oldMode: 'password' as 'password' | 'code',
  oldPassword: '',
  oldCode: '',
})

async function loadEmail() {
  try {
    const me = await authApi.me()
    email.value = (me as { email?: string }).email ?? ''
  } catch {
    /* 未登录等场景静默 */
  }
}

function openBind() {
  // 换绑场景新邮箱留空：预填旧邮箱会让「获取验证码」把码发往旧邮箱，
  // 用户按提示输码提交后提示绑定成功但邮箱实际没换。旧邮箱取码走
  // 「发送至旧邮箱」按钮（sendBindCode(true) 显式指定目标）。
  Object.assign(bindForm, { email: '', code: '', oldMode: 'password', oldPassword: '', oldCode: '' })
  bindOpen.value = true
}

function startBindCd() {
  bindCd.value = 60
  bindTimer = setInterval(() => {
    if (--bindCd.value <= 0) {
      clearInterval(bindTimer)
      bindTimer = undefined
    }
  }, 1000)
}

async function sendBindCode(toOld = false) {
  // toOld：旧邮箱验证码模式的「发送至旧邮箱」按钮——目标固定为当前绑定邮箱
  //（后端视作旧邮箱验证码路径，不校验旧邮箱），不读「新邮箱」输入框。
  const target = toOld ? email.value : bindForm.email.trim().toLowerCase()
  if (!target) {
    ElMessage.warning('请输入邮箱')
    return
  }
  bindSending.value = true
  try {
    const payload: Parameters<typeof verifyApi.sendCode>[0] = { purpose: 'bind', email: target }
    if (email.value && target !== email.value) {
      // 换绑：内联带旧邮箱验证（密码或旧邮箱验证码二选一）
      payload.old_mode = bindForm.oldMode
      if (bindForm.oldMode === 'password') payload.old_password = bindForm.oldPassword
      else payload.old_code = bindForm.oldCode.trim()
    }
    const r = await verifyApi.sendCode(payload)
    ElMessage.success(r.message)
    startBindCd()
  } catch {
    // 拦截器统一提示
  } finally {
    bindSending.value = false
  }
}

async function submitBind() {
  if (!bindForm.email.trim() || !bindForm.code.trim()) {
    ElMessage.warning('请输入邮箱与验证码')
    return
  }
  bindLoading.value = true
  try {
    const r = await verifyApi.bindEmail(bindForm.email.trim(), bindForm.code.trim())
    email.value = r.email
    ElMessage.success('邮箱已绑定')
    bindOpen.value = false
  } catch {
    // 拦截器统一提示
  } finally {
    bindLoading.value = false
  }
}

onMounted(() => {
  load()
  loadEmail()
})
</script>

<template>
  <div class="settings-wrap page" v-loading="loading">
    <div class="page-head">
      <div class="kicker">SETTINGS</div>
      <h1 class="page-title">
        个人设置<small>{{ userStore.nickname || userStore.uid }}<template v-if="userStore.level?.level"> · Lv.{{ userStore.level.level }} {{ levels.nameOf(userStore.level.level) }}</template></small>
      </h1>
    </div>

    <!-- 个人信息 -->
    <el-card shadow="never" class="fcard">
      <div class="panel-h" style="padding-left: 0"><span class="pt">个人信息</span></div>
      <div class="fbody">
        <div class="frow">
          <span class="fl">昵称<div class="fd">用于榜单、题解等公开页面</div></span>
          <div class="fctrl">
            <el-input
              v-model="nickname"
              :disabled="!canEditNickname"
              maxlength="32"
              placeholder="用于榜单、题解等公开页面展示"
              style="width: 320px"
            />
            <span v-if="!canEditNickname" class="fd-inline">当前角色不允许自行修改昵称，请联系管理员</span>
          </div>
        </div>
        <div class="frow">
          <span class="fl">性别</span>
          <el-select v-model="profileForm.gender" clearable placeholder="未填写" style="width: 160px">
            <el-option value="男" label="男" />
            <el-option value="女" label="女" />
          </el-select>
        </div>
        <div class="frow">
          <!-- v0.17 公开主页：学校/年级对外展示；性别/QQ 维持仅自己可见 -->
          <span class="fl">学校<div class="fd">展示在公开主页</div></span>
          <el-input v-model="profileForm.school" maxlength="64" placeholder="选填" style="width: 320px" />
        </div>
        <div class="frow">
          <span class="fl">年级<div class="fd">展示在公开主页</div></span>
          <el-input v-model="profileForm.grade" maxlength="16" placeholder="选填，如：初一 / 高二 / 大一" style="width: 320px" />
        </div>
        <div class="frow">
          <span class="fl">QQ<div class="fd">仅自己可见</div></span>
          <el-input v-model="profileForm.qq" maxlength="16" placeholder="选填" style="width: 320px" />
        </div>
        <div class="frow">
          <span class="fl"></span>
          <div><el-button type="primary" size="small" :loading="savingProfile" @click="saveProfile">保存个人信息</el-button></div>
        </div>
      </div>
    </el-card>

    <!-- 修改密码 -->
    <el-card shadow="never" class="fcard">
      <div class="panel-h" style="padding-left: 0"><span class="pt">修改密码</span></div>
      <div class="fbody">
        <div class="frow">
          <span class="fl">原密码</span>
          <el-input v-model="pwdForm.old" type="password" show-password style="width: 320px" />
        </div>
        <div class="frow">
          <span class="fl">新密码<div class="fd">8-64 位，不含弱口令</div></span>
          <el-input
            v-model="pwdForm.new"
            type="password"
            show-password
            maxlength="64"
            placeholder="至少 8 位"
            style="width: 320px"
          />
        </div>
        <div class="frow">
          <span class="fl">确认新密码</span>
          <div class="fctrl">
            <el-input
              v-model="pwdForm.confirm"
              type="password"
              show-password
              maxlength="64"
              placeholder="再输入一次新密码"
              style="width: 320px"
              @keyup.enter="changePassword"
            />
            <el-button type="primary" size="small" :loading="savingPwd" @click="changePassword">修改密码</el-button>
          </div>
        </div>
        <div class="frow">
          <span class="fl"></span>
          <span class="fd-inline">修改成功后需重新登录；该账号在其他设备的登录态也会一并失效</span>
        </div>
      </div>
    </el-card>

    <!-- 绑定邮箱 -->
    <el-card shadow="never" class="fcard">
      <div class="panel-h" style="padding-left: 0">
        <span class="pt">绑定邮箱</span>
        <span v-if="email" class="sub-status green"><span class="dot green"></span>已验证</span>
      </div>
      <div class="fbody">
        <div class="frow">
          <span class="fl">当前邮箱</span>
          <div class="fctrl">
            <span :class="{ dim: !email }">{{ email || '未绑定' }}</span>
            <el-button size="small" @click="openBind">{{ email ? '换绑' : '绑定' }}</el-button>
          </div>
        </div>
        <div class="frow">
          <span class="fl">换绑须知<div class="fd">需旧邮箱或密码验证</div></span>
          <span class="fd-inline">绑定后可用于找回密码（需管理员开启）；换绑需验证旧邮箱，换绑成功会向原邮箱发通知</span>
        </div>
      </div>
    </el-card>

    <!-- 偏好 -->
    <el-card shadow="never" class="fcard">
      <div class="panel-h" style="padding-left: 0"><span class="pt">偏好</span></div>
      <div class="fbody">
        <div class="frow">
          <span class="fl">默认语言<div class="fd">提交弹窗与在线编程预填</div></span>
          <el-select v-model="prefsForm.defaultLang" clearable placeholder="跟随系统默认（C++ 14）" style="width: 320px">
            <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
          </el-select>
        </div>
        <div class="frow frow-top">
          <span class="fl">代码模板<div class="fd">仅用于在线编程起始代码，留空则全空白</div></span>
          <div class="tpl">
            <el-select v-model="tplLang" style="width: 160px">
              <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
            </el-select>
            <el-input
              v-model="prefsForm.codeTemplates[tplLang]"
              type="textarea"
              :rows="8"
              class="mono"
              spellcheck="false"
              placeholder="留空则在线编程起始为空白，由作答者自行编写"
            />
          </div>
        </div>
        <div class="frow">
          <span class="fl">代码自动补全<div class="fd">Lv.{{ levels.autocompleteMinLevel() }} 档解锁</div></span>
          <div class="fctrl">
            <el-switch
              v-model="prefsForm.autocomplete"
              :disabled="!autocompleteAvailable()"
              @change="savePrefs"
            />
            <span class="fd-inline">
              {{
                autocompleteAvailable()
                  ? '代码编辑器中的关键字 / 词法补全'
                  : `达到 Lv.${levels.autocompleteMinLevel()} 后可开启自动补全（老师角色不受限）`
              }}
            </span>
          </div>
        </div>
        <div class="frow">
          <span class="fl">Markdown 编辑模式<div class="fd">题面 / 公告等编辑器默认模式</div></span>
          <div class="fctrl">
            <el-radio-group v-model="prefsForm.mdEditMode">
              <el-radio-button value="wysiwyg">所见即所得</el-radio-button>
              <el-radio-button value="ir">即时渲染</el-radio-button>
              <el-radio-button value="sv">分屏预览</el-radio-button>
            </el-radio-group>
          </div>
        </div>
        <div class="frow">
          <span class="fl">Markdown 主题<div class="fd">仅作用于编辑器内容区</div></span>
          <div class="fctrl">
            <el-select v-model="prefsForm.mdTheme" style="width: 240px">
              <el-option value="light" label="明亮（站内默认）" />
              <el-option value="ant-design" label="Ant Design" />
              <el-option value="dark" label="暗色" />
              <el-option value="wechat" label="微信" />
            </el-select>
            <el-button type="primary" size="small" :loading="savingPrefs" @click="savePrefs">保存偏好</el-button>
          </div>
        </div>
      </div>
    </el-card>

    <el-dialog v-model="bindOpen" :title="email ? '换绑邮箱' : '绑定邮箱'" width="440px">
      <el-form label-position="top">
        <template v-if="email">
          <el-form-item label="验证旧邮箱（二选一）">
            <el-radio-group v-model="bindForm.oldMode">
              <el-radio-button value="password">密码验证</el-radio-button>
              <el-radio-button value="code">旧邮箱验证码</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="bindForm.oldMode === 'password'" label="登录密码">
            <el-input v-model="bindForm.oldPassword" type="password" show-password />
          </el-form-item>
          <el-form-item v-else label="旧邮箱验证码">
            <el-input v-model="bindForm.oldCode" placeholder="发送至当前绑定邮箱">
              <template #append>
                <el-button
                  :loading="bindSending"
                  :disabled="bindCd > 0"
                  @click="sendBindCode(true)"
                >
                  {{ bindCd > 0 ? `${bindCd}s` : '发送至旧邮箱' }}
                </el-button>
              </template>
            </el-input>
            <div class="dim">验证码发送至 {{ email }}</div>
          </el-form-item>
        </template>
        <el-form-item label="新邮箱">
          <el-input v-model="bindForm.email" placeholder="输入要绑定的新邮箱" />
        </el-form-item>
        <el-form-item label="新邮箱验证码">
          <div class="email-row">
            <el-input v-model="bindForm.code" placeholder="6 位数字" />
            <el-button :loading="bindSending" :disabled="bindCd > 0" @click="sendBindCode()">
              {{ bindCd > 0 ? `${bindCd}s 后重发` : '获取验证码' }}
            </el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="bindOpen = false">取消</el-button>
        <el-button type="primary" :loading="bindLoading" @click="submitBind">绑定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
/* v3 settings-wrap：单列表单页；容器与全站统一 1440（2026-10-26 页头对齐拍板），宽度由卡内表单自带留白控制 */
.settings-wrap {
  max-width: 1440px;
}

.fcard {
  margin-bottom: 16px;
}

.fcard :deep(.el-card__body) {
  padding: 8px 26px 14px;
}

/* frow 内边距收紧（全局 .frow 基础上适配卡体内距） */
.fbody .frow {
  grid-template-columns: 190px minmax(0, 1fr);
}

.frow-top {
  align-items: flex-start;
}

.fctrl {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
}

.fd-inline {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  line-height: 1.6;
}

.tpl {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.email-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}

@media (max-width: 640px) {
  .fbody .frow {
    grid-template-columns: 1fr;
    gap: 8px;
  }
}
</style>

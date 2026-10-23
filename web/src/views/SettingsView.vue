<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import { authApi } from '@/api'
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
    prefsForm.autocomplete = p.prefs?.autocomplete ?? true
    prefsForm.mdEditMode = (p.prefs?.mdEditMode || 'wysiwyg') as typeof prefsForm.mdEditMode
    prefsForm.mdTheme = (p.prefs?.mdTheme || 'light') as typeof prefsForm.mdTheme
    userStore.setLevel(p.level ?? null) // 顶栏徽章与补全门槛同步（AC 后进设置页即刷新）
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

onMounted(load)

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
</script>

<template>
  <PageShell v-loading="loading">
    <el-card shadow="never">
      <template #header><span>个人信息</span></template>
      <el-form label-position="left" label-width="120px" style="max-width: 560px">
        <el-form-item label="昵称">
          <div class="nickname-field">
            <el-input
              v-model="nickname"
              :disabled="!canEditNickname"
              maxlength="32"
              placeholder="用于榜单、题解等公开页面展示"
            />
            <span v-if="!canEditNickname" class="dim">当前角色不允许自行修改昵称，请联系管理员</span>
          </div>
        </el-form-item>
        <el-form-item label="性别">
          <el-select v-model="profileForm.gender" clearable placeholder="未填写" style="width: 160px">
            <el-option value="男" label="男" />
            <el-option value="女" label="女" />
          </el-select>
        </el-form-item>
        <el-form-item label="学校">
          <el-input v-model="profileForm.school" maxlength="64" placeholder="选填，仅自己可见" />
        </el-form-item>
        <el-form-item label="年级">
          <el-input v-model="profileForm.grade" maxlength="16" placeholder="选填，如：初一 / 高二 / 大一" />
        </el-form-item>
        <el-form-item label="QQ">
          <el-input v-model="profileForm.qq" maxlength="16" placeholder="选填，仅自己可见" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" plain :loading="savingProfile" @click="saveProfile">保存</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" style="margin-top: 16px">
      <template #header><span>修改密码</span></template>
      <el-form label-position="left" label-width="120px" style="max-width: 560px">
        <el-form-item label="原密码">
          <el-input v-model="pwdForm.old" type="password" show-password />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input
            v-model="pwdForm.new"
            type="password"
            show-password
            maxlength="64"
            placeholder="至少 8 位"
          />
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input
            v-model="pwdForm.confirm"
            type="password"
            show-password
            maxlength="64"
            placeholder="再输入一次新密码"
            @keyup.enter="changePassword"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" plain :loading="savingPwd" @click="changePassword">修改密码</el-button>
          <span class="dim" style="margin-left: 8px">
            修改成功后需重新登录；该账号在其他设备的登录态也会一并失效
          </span>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" style="margin-top: 16px">
      <template #header><span>偏好</span></template>
      <el-form label-position="left" label-width="150px" style="max-width: 640px">
        <el-form-item label="默认语言">
          <el-select v-model="prefsForm.defaultLang" clearable placeholder="跟随系统默认（C++ 14）" style="width: 240px">
            <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
          </el-select>
        </el-form-item>
        <el-form-item label="代码模板">
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
              :placeholder="`留空则提交时全空白，由作答者自行编写`"
            />
          </div>
        </el-form-item>
        <el-form-item label="代码自动补全">
          <div class="ac-row">
            <el-switch
              v-model="prefsForm.autocomplete"
              :disabled="!autocompleteAvailable()"
              @change="savePrefs"
            />
            <span class="dim">
              {{
                autocompleteAvailable()
                  ? '代码编辑器中的关键字 / 词法补全'
                  : `达到 Lv.${levels.autocompleteMinLevel()} 后可开启自动补全（老师角色不受限）`
              }}
            </span>
          </div>
        </el-form-item>
        <el-form-item label="Markdown 编辑模式">
          <div class="pref-stack">
            <el-radio-group v-model="prefsForm.mdEditMode">
              <el-radio-button value="wysiwyg">所见即所得</el-radio-button>
              <el-radio-button value="ir">即时渲染</el-radio-button>
              <el-radio-button value="sv">分屏预览</el-radio-button>
            </el-radio-group>
            <span class="dim">题面 / 公告等编辑器的默认模式；编辑器工具栏也可随时切换（自动记住）</span>
          </div>
        </el-form-item>
        <el-form-item label="Markdown 主题">
          <div class="pref-stack">
            <el-select v-model="prefsForm.mdTheme" style="width: 240px">
              <el-option value="light" label="明亮（站内默认）" />
              <el-option value="ant-design" label="Ant Design" />
              <el-option value="dark" label="暗色" />
              <el-option value="wechat" label="微信" />
            </el-select>
            <span class="dim">仅作用于编辑器内容区；题目展示等阅读面固定明亮主题</span>
          </div>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" plain :loading="savingPrefs" @click="savePrefs">保存</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <template #aside>
      <el-card shadow="never">
        <template #header><span>说明</span></template>
        <div class="tips">
          <p>昵称会显示在榜单、题解等公开页面；性别 / 学校 / 年级 / QQ 仅自己可见。</p>
          <p>默认语言与代码模板会在打开提交弹窗、在线编程时自动预填；切换语言带出对应模板。</p>
          <p>Markdown 编辑模式与主题跟随账号保存，在编辑器工具栏里切换也会自动同步。</p>
        </div>
      </el-card>
    </template>
  </PageShell>
</template>

<style scoped>
.tpl {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.nickname-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 100%;
}

.ac-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* Markdown 编辑模式/主题行：说明文字排在控件下方整行展示，不挤压控件 */
.pref-stack {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  width: 100%;
}
</style>

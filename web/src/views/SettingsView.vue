<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'

import PageShell from '@/components/PageShell.vue'
import { authApi } from '@/api'
import { useUserStore } from '@/stores/user'
import { autocompleteAvailable, languageOptions, mergedPrefs } from '@/utils/code'

// 个人设置（v0.4.8）：本人自助字段（昵称/性别/学校/年级/QQ）+ 偏好（默认语言/代码模板）。
// 昵称是否可改由后端按 user.nickname.edit 权限位现算（can_edit_nickname），默认角色默认开启。
const userStore = useUserStore()
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
}>({ defaultLang: '', codeTemplates: {}, autocomplete: true })

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
      <template #header><span>偏好</span></template>
      <el-form label-position="left" label-width="120px" style="max-width: 640px">
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
              {{ autocompleteAvailable() ? '代码编辑器中的关键字 / 词法补全，默认开启' : '达到指定等级后可开启自动补全' }}
            </span>
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
</style>

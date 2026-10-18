<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'

import PageShell from '@/components/PageShell.vue'
import { authApi } from '@/api'
import { useUserStore } from '@/stores/user'
import { autocompleteAvailable, languageOptions, mergedPrefs } from '@/utils/code'

// 个人设置（v0.4.8）：本人自助字段（性别/学校/年级/QQ）+ 偏好（默认语言/代码模板/MD 编辑模式）。
// 真实姓名只读展示——仅管理员后台可改（班级名单对号用）。
const userStore = useUserStore()
const loading = ref(false)
const savingProfile = ref(false)
const savingPrefs = ref(false)

const realName = ref('')

// '' 表示未填写（后端显式空串=清空）
const profileForm = reactive({ gender: '', school: '', grade: '', qq: '' })

const prefsForm = reactive<{
  defaultLang: string
  mdEditMode: '' | 'wysiwyg' | 'ir' | 'sv'
  codeTemplates: Record<string, string>
  autocomplete: boolean
}>({ defaultLang: '', mdEditMode: '', codeTemplates: {}, autocomplete: true })

// 模板编辑器当前编辑的语言（按语言各存一套，切换带出对应模板）
const tplLang = ref('cpp')

async function load() {
  loading.value = true
  try {
    const p = await authApi.getProfile()
    realName.value = p.real_name ?? ''
    profileForm.gender = p.gender ?? ''
    profileForm.school = p.school ?? ''
    profileForm.grade = p.grade ?? ''
    profileForm.qq = p.qq ?? ''
    prefsForm.defaultLang = p.prefs?.defaultLang ?? ''
    prefsForm.mdEditMode = p.prefs?.mdEditMode ?? ''
    prefsForm.codeTemplates = { ...(p.prefs?.codeTemplates ?? {}) }
    prefsForm.autocomplete = p.prefs?.autocomplete ?? true
  } finally {
    loading.value = false
  }
}

async function saveProfile() {
  savingProfile.value = true
  try {
    await authApi.updateProfile({
      gender: profileForm.gender,
      school: profileForm.school,
      grade: profileForm.grade,
      qq: profileForm.qq,
    })
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
      mdEditMode: prefsForm.mdEditMode,
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
        <el-form-item label="真实姓名">
          <el-input v-model="realName" disabled placeholder="仅管理员可修改，用于班级名单对号" />
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
        <el-form-item label="MD 编辑模式">
          <el-select v-model="prefsForm.mdEditMode" clearable placeholder="所见即所得（默认）" style="width: 240px">
            <el-option value="wysiwyg" label="所见即所得" />
            <el-option value="ir" label="即时渲染" />
            <el-option value="sv" label="分屏" />
          </el-select>
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
          <p>真实姓名仅管理员后台可修改，用于班级名单对号；性别 / 学校 / 年级 / QQ 仅自己可见。</p>
          <p>默认语言与代码模板会在打开提交弹窗、在线编程时自动预填；切换语言带出对应模板。</p>
          <p>MD 编辑模式决定公告 / 题面等 Markdown 编辑器的初始形态，编辑中仍可随时切换。</p>
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

.ac-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
</style>

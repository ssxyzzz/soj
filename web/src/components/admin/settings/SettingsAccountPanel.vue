<script setup lang="ts">
// 系统设置 · 账号与通知页（2026-10-28 拆分）：注册取号起点 / 通知保留 / 全站广播频次。
// v0.34 增：资料自改五开关（昵称/性别/年级/学校/地区）+ 年级自动晋级状态（全自动，无手动触发）。
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import type { GradePromotionInfo } from '@/types'

const userSeqStart = ref(100)
const notifyRetentionDays = ref(90)
const notifyBroadcastPerDay = ref(1)

// 资料自改开关（v0.34）：false = 学生端锁定该字段（root 用户管理仍可改）
const profileEdit = ref({ nickname: true, gender: true, grade: true, school: true, region: true })
const profileFields = [
  { k: 'nickname' as const, label: '昵称' },
  { k: 'gender' as const, label: '性别' },
  { k: 'grade' as const, label: '年级' },
  { k: 'school' as const, label: '学校' },
  { k: 'region' as const, label: '地区' },
]

// 年级晋级（v0.34）：状态展示 + 手动立即晋级（错时钟/提前晋级兜底）
const promo = ref<GradePromotionInfo | null>(null)
const promoLoading = ref(false)

const saveSettings = ref(false)
const saving = computed(() => saveSettings.value)

const promoTotal = computed(() =>
  promo.value ? promo.value.grades.reduce((s, g) => s + g.count, 0) : 0,
)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    userSeqStart.value = s.user_seq_start ?? 100
    notifyRetentionDays.value = s.notify_retention_days ?? 90
    notifyBroadcastPerDay.value = s.notify_broadcast_per_day ?? 1
    profileEdit.value.nickname = s.profile_edit_nickname ?? true
    profileEdit.value.gender = s.profile_edit_gender ?? true
    profileEdit.value.grade = s.profile_edit_grade ?? true
    profileEdit.value.school = s.profile_edit_school ?? true
    profileEdit.value.region = s.profile_edit_region ?? true
  } catch {
    // 加载失败保留默认值展示
  }
  loadPromo()
})

async function loadPromo() {
  promoLoading.value = true
  try {
    promo.value = await adminApi.gradePromotionInfo()
  } catch {
    promo.value = null
  } finally {
    promoLoading.value = false
  }
}

async function save() {
  saveSettings.value = true
  try {
    await adminApi.updateSettings({
      user_seq_start: userSeqStart.value,
      notify_retention_days: notifyRetentionDays.value,
      notify_broadcast_per_day: notifyBroadcastPerDay.value,
      profile_edit_nickname: profileEdit.value.nickname,
      profile_edit_gender: profileEdit.value.gender,
      profile_edit_grade: profileEdit.value.grade,
      profile_edit_school: profileEdit.value.school,
      profile_edit_region: profileEdit.value.region,
    })
    ElMessage.success('账号与通知设置已保存')
  } finally {
    saveSettings.value = false
  }
}

function fmtNext(at: string | undefined) {
  if (!at) return '—'
  const d = new Date(at)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>账号与通知</span>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </template>
    <el-form label-position="left" label-width="220px" style="max-width: 680px">
      <el-divider content-position="left">账号</el-divider>
      <el-form-item>
        <template #label>
          <TipLabel label="注册取号起点" tip="自动分配的用户 ID 从该号起按序补空（默认 100，随机注册/手动指定/改 ID 留下的空缺按序回填）；起点以下号段不参与；调大即时生效，调小按新起点重扫；手动指定 ID 不受此限" />
        </template>
        <el-input-number v-model="userSeqStart" :min="1" :max="1000000000" :controls="false" style="width: 160px" />
      </el-form-item>

      <el-divider content-position="left">资料自改</el-divider>
      <el-form-item>
        <template #label>
          <TipLabel label="允许本人修改的字段" tip="关闭 = 学生端该字段只读（保存不生效）；root 用户管理编辑不受限。昵称同时受角色权限位 user.nickname.edit 约束（双闸）；QQ / 邮箱不锁。年级关闭后仍会每年自动晋级" />
        </template>
        <div class="pe-switches">
          <label v-for="f in profileFields" :key="f.k" class="pe-sw">
            <el-switch v-model="profileEdit[f.k]" />
            <span>{{ f.label }}</span>
          </label>
        </div>
      </el-form-item>

      <el-divider content-position="left">
        <TipLabel label="年级自动晋级" tip="一年级 → 六年级 → 初一 → 初三 → 高一 → 高三 → 已毕业，每年 9/1 自动进一档；花名册/编辑里的年级别名（如 7 / 七年级）入库即归一为标准名（初一）" />
      </el-divider>
      <el-form-item label="当前学年">
        <span v-if="promo">
          {{ promo.current_year }}–{{ promo.current_year + 1 }} 学年 · 下次自动晋级 {{ fmtNext(promo.next_at) }}（每年 9 月 1 日）
        </span>
        <span v-else class="dim">读取中…</span>
      </el-form-item>
      <el-form-item label="各档位在册">
        <span v-if="promo && promoTotal" class="grade-counts">
          <span
            v-for="g in promo.grades.filter((x) => x.count > 0)"
            :key="g.no"
            class="gc"
          >
            {{ g.name || '未设置' }} <b>{{ g.count }}</b>
          </span>
        </span>
        <span v-else class="dim">暂无设年级的用户</span>
      </el-form-item>

      <el-divider content-position="left">站内通知</el-divider>
      <el-form-item>
        <template #label>
          <TipLabel label="通知保留天数" tip="站内通知过期自动清理（改动只影响新通知）" />
        </template>
        <el-input-number v-model="notifyRetentionDays" :min="7" :max="365" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="全站广播每日上限" tip="root 全员公告的每日频次上限（0 = 不限制）" />
        </template>
        <el-input-number v-model="notifyBroadcastPerDay" :min="0" :max="100" />
      </el-form-item>
    </el-form>
  </el-card>
</template>

<style scoped>

.pe-switches {
  display: flex;
  flex-wrap: wrap;
  gap: var(--soj-sp-8) var(--soj-sp-20);
}

.pe-sw {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-6);
  font-size: var(--soj-fs-14);
}

.grade-counts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--soj-sp-4) var(--soj-sp-14);
}

.gc {
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-secondary);
}
</style>

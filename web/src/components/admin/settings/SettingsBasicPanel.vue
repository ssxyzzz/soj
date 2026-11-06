<script setup lang="ts">
// 系统设置 · 基础页（2026-10-28 拆分）：判题限速 / 数据下载限额 / 补全等级 / 批量导入限额 / ICP。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import { useSiteStore } from '@/stores/site'

const rateLimit = ref(3)
// 每日可下载数据的题目数（0 = 不限制）；作用于拥有「下载数据」权限位的用户
const dataDownloadLimit = ref(5)
// 代码自动补全解锁等级（v0.11）：等级 ≥ 该值才能开启自动补全（缺省 4）
const autocompleteMinLevel = ref(4)
// ICP 备案号（v0.13）：备案下来后填入，页脚悬挂并链接工信部备案系统；空 = 不展示
const icpNumber = ref('')
// 公安网备号（2026-11-02）：同页脚悬挂，备案号中数字即公安部备案查询 code；空 = 不展示
const mpsNumber = ref('')
// 批量导入限额（v0.15）：单题数据 / 整包 zip（MB）
const batchProblemMB = ref(100)
const batchZipMB = ref(2048)
// 自动题号起点：无前缀自动取号的最小题号（默认 1001，1~起点-1 留给带题号的导入题包）
const problemSeqStart = ref(1001)
// 玉经济体（v0.33-①）：货币名（1–6 字，全站文案统一读此值）+ 比赛奖池获奖比例
const currencyName = ref('玉')
const jadeAwardRatePct = ref(50)
// 玉折扣阶梯（v0.40）：Lv5–10 六档百分比（95 = 九五折…70 = 七折）；仅作用于题解解锁与 AI
const jadeDiscount = ref<number[]>([95, 90, 85, 80, 75, 70])
// 玉经济体（v0.33-②）：题解解锁系数 + 悬赏上下限 + 实物兑换开关
const unlockMult = ref(5)
const bountyMin = ref(5)
const bountyMax = ref(500)
const shopEnabled = ref(false)
// 倍率总则批（v0.45）：连击加成三参（每满 step 连击 +pct%、封顶 capPct%）+ 复仇窗口
const jadeComboStep = ref(10)
const jadeComboPct = ref(10)
const jadeComboCapPct = ref(100)
const jadeRevengeHours = ref(24)

const saving = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    rateLimit.value = s.rate_limit_per_minute
    dataDownloadLimit.value = s.data_download_daily_limit
    autocompleteMinLevel.value = s.autocomplete_min_level
    icpNumber.value = s.icp_number || ''
    mpsNumber.value = s.mps_number || ''
    batchProblemMB.value = s.batch_import_problem_mb
    batchZipMB.value = s.batch_import_zip_mb
    problemSeqStart.value = s.problem_seq_start ?? 1001
    currencyName.value = s.currency_name || '玉'
    jadeAwardRatePct.value = s.jade_award_rate_pct ?? 50
    jadeDiscount.value = s.jade_discount?.length === 6 ? [...s.jade_discount] : [95, 90, 85, 80, 75, 70]
    unlockMult.value = s.jade_solution_unlock_mult ?? 5
    bountyMin.value = s.jade_bounty_min ?? 5
    bountyMax.value = s.jade_bounty_max ?? 500
    shopEnabled.value = !!s.jade_shop_enabled
    jadeComboStep.value = s.jade_combo_step ?? 10
    jadeComboPct.value = s.jade_combo_pct ?? 10
    jadeComboCapPct.value = s.jade_combo_cap_pct ?? 100
    jadeRevengeHours.value = s.jade_revenge_hours ?? 24
  } catch {
    // 加载失败保留默认值展示
  }
})

async function save() {
  saving.value = true
  try {
    await adminApi.updateSettings({
      rate_limit_per_minute: rateLimit.value,
      data_download_daily_limit: dataDownloadLimit.value,
      autocomplete_min_level: autocompleteMinLevel.value,
      icp_number: icpNumber.value.trim(),
      mps_number: mpsNumber.value.trim(),
      batch_import_problem_mb: batchProblemMB.value,
      batch_import_zip_mb: batchZipMB.value,
      problem_seq_start: problemSeqStart.value,
      currency_name: currencyName.value.trim(),
      jade_award_rate_pct: jadeAwardRatePct.value,
      jade_discount: [...jadeDiscount.value],
      jade_solution_unlock_mult: unlockMult.value,
      jade_bounty_min: bountyMin.value,
      jade_bounty_max: bountyMax.value,
      jade_shop_enabled: shopEnabled.value,
      jade_combo_step: jadeComboStep.value,
      jade_combo_pct: jadeComboPct.value,
      jade_combo_cap_pct: jadeComboCapPct.value,
      jade_revenge_hours: jadeRevengeHours.value,
    })
    ElMessage.success('基础设置已保存')
    // ICP 改了页脚要即时跟随：重置站点公开状态缓存
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
        <span>基础设置</span>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </template>
    <el-form label-position="left" label-width="220px" style="max-width: 680px">
      <el-divider content-position="left">判题与下载</el-divider>
      <el-form-item>
        <template #label>
          <TipLabel label="每分钟提交上限" tip="普通用户的全站提交频率闸门（次/分钟）" />
        </template>
        <el-input-number v-model="rateLimit" :min="1" :max="60" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="每日可下载数据的题目数" tip="0 为不限制；限定拥有「下载数据」权限位的用户每天最多下载几道题的数据包（同题重复下载不重复计数）" />
        </template>
        <el-input-number v-model="dataDownloadLimit" :min="0" :max="1000" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="代码自动补全解锁等级" tip="学生等级达到该档后才能在设置里开启代码自动补全（1–10）" />
        </template>
        <el-input-number v-model="autocompleteMinLevel" :min="1" :max="10" />
      </el-form-item>

      <el-divider content-position="left">批量导入</el-divider>
      <el-form-item>
        <template #label>
          <TipLabel label="单题数据上限（MB）" tip="后台「导入题目」页单道题数据的体积上限，超限该题跳过" />
        </template>
        <el-input-number v-model="batchProblemMB" :min="1" :max="4096" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="整包上限（MB）" tip="单次上传题包 zip 的体积上限；题多请分批传" />
        </template>
        <el-input-number v-model="batchZipMB" :min="1" :max="8192" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="自动题号起点" tip="无前缀自动取号的最小题号（默认 1001）；1~起点-1 的保留段留给带题号的导入题包；调大即时生效、只前进不回退；导入/建题指定题号不受此限" />
        </template>
        <el-input-number v-model="problemSeqStart" :min="1" :max="1000000000" :controls="false" style="width: 160px" />
      </el-form-item>

      <el-divider content-position="left">玉经济</el-divider>
      <el-form-item>
        <template #label>
          <TipLabel label="货币名" tip="做题奖励货币的名称（1–6 个字，如：玉 / 金币 / 钻石 / 奖励币），导航栏、流水、商店等全站文案统一显示" />
        </template>
        <el-input
          v-model="currencyName"
          placeholder="玉"
          maxlength="6"
          style="width: 120px"
        />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="比赛奖池获奖比例（%）" tip="真正参赛（得分 > 0）人数 × 该比例 = 名次奖名额（向上取整，至少 1 人），名次奖按线性递减瓜分名次奖池" />
        </template>
        <el-input-number v-model="jadeAwardRatePct" :min="1" :max="100" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="玉折扣阶梯（Lv5–10）" tip="消费折扣只作用于题解解锁与 AI 助教（悬赏是玉流转、实物一律原价）；Lv5 起生效，越高越便宜，逐档不能高于上一档（保存时自动校正）" />
        </template>
        <div class="disc-grid">
          <span v-for="(label, i) in ['Lv5', 'Lv6', 'Lv7', 'Lv8', 'Lv9', 'Lv10']" :key="label" class="disc-cell">
            {{ label }}
            <el-input-number v-model="jadeDiscount[i]" :min="10" :max="100" size="small" style="width: 90px" />
            %
          </span>
        </div>
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="题解解锁价系数" tip="解锁价 = 题目难度 × 系数（默认 5，D10 = 50 玉）；首次通过后免费" />
        </template>
        <el-input-number v-model="unlockMult" :min="1" :max="100" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="悬赏额上下限（玉）" tip="发帖挂悬赏的范围（默认 5～500，另受余额约束）" />
        </template>
        <el-input-number v-model="bountyMin" :min="1" :max="100000" style="width: 130px" />
          ～
        <el-input-number v-model="bountyMax" :min="1" :max="1000000" style="width: 140px" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="开放实物兑换" tip="开启后在「玉」页面右列显示实物兑换区（物品目录与学生订单）" />
        </template>
        <el-switch v-model="shopEnabled" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="连击玉加成" tip="连续 AC 跨日累计（WA/断更清零、同题重交不涨）；默认每 10 连击 +10%、100 连击 ×2.0 封顶。与升维祝福/每日一题/复仇/PK 名次同为倍率，发放时只取最高一项，不连乘（峰值 ×2）" />
        </template>
        <div class="combo-row">
          每满
          <el-input-number v-model="jadeComboStep" :min="1" :max="1000" size="small" style="width: 90px" />
          连击 +<el-input-number v-model="jadeComboPct" :min="1" :max="100" size="small" style="width: 80px" />
          %，封顶 +<el-input-number v-model="jadeComboCapPct" :min="0" :max="900" size="small" style="width: 90px" />%
        </div>
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="每日复仇窗口（小时）" tip="最早一次错误提交距今满该时长才可复仇（默认 24 = 隔夜订正；当日顺路做对无加成）；每日仅 1 个复仇名额" />
        </template>
        <el-input-number v-model="jadeRevengeHours" :min="1" :max="720" />
      </el-form-item>

      <el-divider content-position="left">备案信息</el-divider>
      <el-form-item>
        <template #label>
          <TipLabel label="ICP 备案号" tip="备案通过后填入，页脚展示并链接工信部备案系统；留空不展示" />
        </template>
        <el-input
          v-model="icpNumber"
          placeholder="如 皖ICP备2026000000号-1"
          maxlength="64"
          style="max-width: 320px"
        />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="公安网备号" tip="备案通过后填入，页脚展示；号内数字自动生成公安部备案查询链接；留空不展示" />
        </template>
        <el-input
          v-model="mpsNumber"
          placeholder="如 皖公网安备34010000000001号"
          maxlength="64"
          style="max-width: 320px"
        />
      </el-form-item>
    </el-form>
  </el-card>
</template>

<style scoped>
/* 玉折扣阶梯：3 列对齐网格（Lv5–7 / Lv8–10 两行，格内 Lv 徽字 + 输入 + %） */
.disc-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, max-content));
  gap: var(--soj-sp-8) var(--soj-sp-20);
}

.disc-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-4);
}

/* 连击加成行：文字与三个小输入混排 */
.combo-row {
  display: flex;
  gap: var(--soj-sp-8);
  align-items: center;
  flex-wrap: wrap;
}
</style>

<script setup lang="ts">
// 系统设置 · 基础页（2026-10-28 拆分）：判题限速 / 数据下载限额 / 补全等级 / 批量导入限额 / ICP。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import { useSiteStore } from '@/stores/site'

const rateLimit = ref(3)
// 每日可下载数据的题目数（0 = 不限制）；作用于拥有「下载数据」权限位的用户
const dataDownloadLimit = ref(5)
// 代码自动补全解锁等级（v0.11）：等级 ≥ 该值才能开启自动补全
const autocompleteMinLevel = ref(5)
// ICP 备案号（v0.13）：备案下来后填入，页脚悬挂并链接工信部备案系统；空 = 不展示
const icpNumber = ref('')
// 批量导入限额（v0.15）：单题数据 / 整包 zip（MB）
const batchProblemMB = ref(100)
const batchZipMB = ref(2048)
// 自动题号起点：无前缀自动取号的最小题号（默认 1001，1~起点-1 留给带题号的导入题包）
const problemSeqStart = ref(1001)
// 玉经济体（v0.33-①）：货币名（1–6 字，全站文案统一读此值）+ 比赛奖池获奖比例
const currencyName = ref('玉')
const jadeAwardRatePct = ref(50)
// 玉经济体（v0.33-②）：题解解锁系数 + 悬赏上下限 + 实物兑换开关
const unlockMult = ref(5)
const bountyMin = ref(5)
const bountyMax = ref(500)
const shopEnabled = ref(false)

const saving = ref(false)

onMounted(async () => {
  try {
    const s = await adminApi.getSettings()
    rateLimit.value = s.rate_limit_per_minute
    dataDownloadLimit.value = s.data_download_daily_limit
    autocompleteMinLevel.value = s.autocomplete_min_level
    icpNumber.value = s.icp_number || ''
    batchProblemMB.value = s.batch_import_problem_mb
    batchZipMB.value = s.batch_import_zip_mb
    problemSeqStart.value = s.problem_seq_start ?? 1001
    currencyName.value = s.currency_name || '玉'
    jadeAwardRatePct.value = s.jade_award_rate_pct ?? 50
    unlockMult.value = s.jade_solution_unlock_mult ?? 5
    bountyMin.value = s.jade_bounty_min ?? 5
    bountyMax.value = s.jade_bounty_max ?? 500
    shopEnabled.value = !!s.jade_shop_enabled
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
      batch_import_problem_mb: batchProblemMB.value,
      batch_import_zip_mb: batchZipMB.value,
      problem_seq_start: problemSeqStart.value,
      currency_name: currencyName.value.trim(),
      jade_award_rate_pct: jadeAwardRatePct.value,
      jade_solution_unlock_mult: unlockMult.value,
      jade_bounty_min: bountyMin.value,
      jade_bounty_max: bountyMax.value,
      jade_shop_enabled: shopEnabled.value,
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
      <el-form-item label="普通用户每分钟提交上限">
        <el-input-number v-model="rateLimit" :min="1" :max="60" />
      </el-form-item>
      <el-form-item label="每日可下载数据的题目数">
        <el-input-number v-model="dataDownloadLimit" :min="0" :max="1000" />
        <span class="dim" style="margin-left: 8px">
          0 为不限制；限定拥有「下载数据」权限位的用户每天最多下载几道题的数据包（同题重复下载不重复计数）
        </span>
      </el-form-item>
      <el-form-item label="代码自动补全解锁等级">
        <el-input-number v-model="autocompleteMinLevel" :min="1" :max="10" />
        <span class="dim" style="margin-left: 8px">
          学生等级达到该档后才能在设置里开启代码自动补全（1–10）
        </span>
      </el-form-item>
      <el-form-item label="批量导入单题数据上限（MB）">
        <el-input-number v-model="batchProblemMB" :min="1" :max="4096" />
        <span class="dim" style="margin-left: 8px">
          后台「导入题目」页单道题数据的体积上限，超限该题跳过（v0.15）
        </span>
      </el-form-item>
      <el-form-item label="批量导入整包上限（MB）">
        <el-input-number v-model="batchZipMB" :min="1" :max="8192" />
        <span class="dim" style="margin-left: 8px">单次上传题包 zip 的体积上限；题多请分批传</span>
      </el-form-item>
      <el-form-item label="自动题号起点">
        <el-input-number v-model="problemSeqStart" :min="1" :max="1000000000" :controls="false" style="width: 160px" />
        <span class="dim" style="margin-left: 8px">
          无前缀自动取号的最小题号（默认 1001）；1~起点-1 的保留段留给带题号的导入题包；调大即时生效、只前进不回退；导入/建题指定题号不受此限
        </span>
      </el-form-item>
      <el-form-item label="货币名">
        <el-input
          v-model="currencyName"
          placeholder="玉"
          maxlength="6"
          style="width: 120px"
        />
        <span class="dim" style="margin-left: 8px">
          做题奖励货币的名称（1–6 个字，如：玉 / 金币 / 钻石 / 奖励币），导航栏、流水、商店等全站文案统一显示
        </span>
      </el-form-item>
      <el-form-item label="比赛奖池获奖比例（%）">
        <el-input-number v-model="jadeAwardRatePct" :min="1" :max="100" />
        <span class="dim" style="margin-left: 8px">
          真正参赛（得分 &gt; 0）人数 × 该比例 = 名次奖名额（向上取整，至少 1 人），名次奖按线性递减瓜分名次奖池
        </span>
      </el-form-item>
      <el-form-item label="题解解锁价系数">
        <el-input-number v-model="unlockMult" :min="1" :max="100" />
        <span class="dim" style="margin-left: 8px">
          解锁价 = 题目难度 × 系数（默认 5，D10 = 50 玉）；首次通过后免费
        </span>
      </el-form-item>
      <el-form-item label="悬赏额上下限（玉）">
        <el-input-number v-model="bountyMin" :min="1" :max="100000" style="width: 130px" />
          ～
        <el-input-number v-model="bountyMax" :min="1" :max="1000000" style="width: 140px" />
        <span class="dim" style="margin-left: 8px">发帖挂悬赏的范围（默认 5～500，另受余额约束）</span>
      </el-form-item>
      <el-form-item label="开放实物兑换">
        <el-switch v-model="shopEnabled" />
        <span class="dim" style="margin-left: 8px">
          开启后导航栏出现「兑换」入口，物品目录与订单履约在后台「实物兑换」面板（root）
        </span>
      </el-form-item>
      <el-form-item label="ICP 备案号">
        <el-input
          v-model="icpNumber"
          placeholder="如 皖ICP备2026000000号-1（留空不展示）"
          maxlength="64"
          style="max-width: 320px"
        />
        <span class="dim" style="margin-left: 8px">
          备案通过后填入，页脚展示并链接工信部备案系统；留空不展示
        </span>
      </el-form-item>
    </el-form>
  </el-card>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
</style>

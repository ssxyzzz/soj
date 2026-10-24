<script setup lang="ts">
// 等级面板（v0.11，root）：等级名称 / 升级阈值 / 衰减系数。
// 十阶档数与色轴固定不开放改。特效开关字段（effects_*）在配置里保留但 UI 已隐藏——
// v3-full 改版（2026-10-24）等级徽章全静态，开关不再产生视觉差异。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import { DEFAULT_LEVEL_NAMES, useLevelsStore } from '@/stores/levels'

const levels = useLevelsStore()

const form = reactive({
  names: [...DEFAULT_LEVEL_NAMES],
  difficulty_names: [...DEFAULT_LEVEL_NAMES],
  thresholds: [200, 600, 1200, 2000, 3000, 4200, 5600, 7200, 9000] as number[],
  decay1: 0.5,
  decay2: 0.2,
  decay3: 0.05,
  effects_enabled: true,
  effects_tiers: Array.from({ length: 10 }, () => true),
})
const saving = ref(false)

onMounted(async () => {
  try {
    const { config } = await adminApi.getLevelConfig()
    form.names = [...config.names]
    form.difficulty_names = [...(config.difficulty_names?.length === 10 ? config.difficulty_names : config.names)]
    form.thresholds = [...config.thresholds]
    form.decay1 = config.decay1
    form.decay2 = config.decay2
    form.decay3 = config.decay3
    form.effects_enabled = config.effects_enabled
    form.effects_tiers = [...config.effects_tiers]
  } catch {
    // 加载失败保留默认值展示
  }
})

async function save() {
  if (form.names.some((n) => !n.trim()) || form.difficulty_names.some((n) => !n.trim())) {
    ElMessage.warning('等级名称与难度名称均不能为空')
    return
  }
  for (let i = 1; i < form.thresholds.length; i++) {
    if (form.thresholds[i] <= form.thresholds[i - 1]) {
      ElMessage.warning('升级阈值需严格递增（累计经验）')
      return
    }
  }
  saving.value = true
  try {
    await adminApi.saveLevelConfig({
      names: form.names.map((n) => n.trim()),
      difficulty_names: form.difficulty_names.map((n) => n.trim()),
      thresholds: form.thresholds,
      decay1: form.decay1,
      decay2: form.decay2,
      decay3: form.decay3,
      effects_enabled: form.effects_enabled,
      effects_tiers: [...form.effects_tiers],
    })
    ElMessage.success('等级配置已保存')
    await levels.reload() // 刷新全局共享配置：排名/顶栏徽章名称即时跟随
  } finally {
    saving.value = false
  }
}

// 等级名旁的预览 chip（v3-full 静态浅色胶囊，Lv.10 蓝青渐变实底）
function chipClass(i: number): Record<string, boolean> {
  return { 'ulv-tag': true, [`ulv-${i + 1}`]: true }
}

// 难度名预览 chip（与题库难度徽章同款静态浅色胶囊）
function diffChipClass(i: number): Record<string, boolean> {
  return { 'diff-tag': true, [`diff-${i + 1}`]: true }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>等级设置</span>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </template>

    <div class="section-title">等级名称（Lv.1–10，站点文案可自定义；色块即徽章实时预览）</div>
    <div class="names-grid">
      <div v-for="(label, i) in form.names" :key="i" class="name-cell">
        <span class="tier-chip" :class="chipClass(i)">Lv.{{ i + 1 }}</span>
        <el-input v-model="form.names[i]" maxlength="12" :placeholder="DEFAULT_LEVEL_NAMES[i]" />
      </div>
    </div>

    <div class="section-title">难度名称（难度 1–10 档的展示文字，默认与等级名相同、可独立修改；题库徽章/筛选与建题选择器均显示此名）</div>
    <div class="names-grid">
      <div v-for="(label, i) in form.difficulty_names" :key="i" class="name-cell">
        <span class="tier-chip" :class="diffChipClass(i)">{{ form.difficulty_names[i] }}</span>
        <el-input v-model="form.difficulty_names[i]" maxlength="12" :placeholder="DEFAULT_LEVEL_NAMES[i]" />
      </div>
    </div>

    <div class="section-title">升级阈值（累计经验；默认 = 每档同难度题约 200 题；两行五列与上方名称区对应，Lv.1 起步档恒为 0）</div>
    <div class="thresholds-grid">
      <div class="threshold-cell">
        <span class="tier-chip" :class="chipClass(0)">Lv.1</span>
        <el-input-number :model-value="0" disabled :controls="false" style="width: 110px" />
      </div>
      <div v-for="(t, i) in form.thresholds" :key="i" class="threshold-cell">
        <span class="tier-chip" :class="chipClass(i + 1)">Lv.{{ i + 2 }}</span>
        <el-input-number v-model="form.thresholds[i]" :min="1" :step="50" :controls="false" style="width: 110px" />
      </div>
    </div>

    <div class="section-title">衰减系数（打低于当前等级的题按系数折算经验；低 ≥4 级恒为 0）</div>
    <div class="decay-row">
      <span class="dim">低 1 级</span>
      <el-input-number v-model="form.decay1" :min="0" :max="1" :step="0.05" :precision="2" :controls="false" />
      <span class="dim">低 2 级</span>
      <el-input-number v-model="form.decay2" :min="0" :max="1" :step="0.05" :precision="2" :controls="false" />
      <span class="dim">低 3 级</span>
      <el-input-number v-model="form.decay3" :min="0" :max="1" :step="0.01" :precision="2" :controls="false" />
    </div>
  </el-card>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.section-title {
  font-weight: var(--soj-fw-emphasis);
  margin: 18px 0 10px;
}

.names-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 10px 14px;
}

.name-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 预览 chip：v3-full 静态浅色胶囊（底/边/字由全局 ulv、diff 两族注入），仅定形制 */
.tier-chip {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 10px;
  border-radius: 999px;
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
}

.thresholds-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 10px 14px;
}

/* 与名称区 name-cell 同构：色块 chip（Lv.2–10）+ 输入框一行排布 */
.threshold-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.decay-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
</style>

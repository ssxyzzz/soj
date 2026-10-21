<script setup lang="ts">
// 等级面板（v0.11，root）：等级名称 / 升级阈值 / 衰减系数 / 特效开关。
// 十阶档数与色轴固定不开放改；特效全局关 = 全站等级徽章退静态
//（尊重不想炫酷的部署方，开源场景各部署自定）。难度徽章恒静态，不受此面板影响。
import { onMounted, reactive, ref, watch } from 'vue'
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

// 全局开关打开时若逐级开关全是关的，自动全选——否则「开了全局没反应」
//（全局开 ∧ 逐级全关 = 仍然全部静态，等价于没开）
watch(
  () => form.effects_enabled,
  (on) => {
    if (on && !form.effects_tiers.some((t) => t)) {
      form.effects_tiers = Array.from({ length: 10 }, () => true)
    }
  },
)

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
    await levels.reload() // 刷新全局共享配置：排名/顶栏徽章的特效即时跟随
  } finally {
    saving.value = false
  }
}

// 达 Lv.L 的阈值展示辅助：Lv.2 对应 thresholds[0]
function thresholdLabel(i: number): string {
  return `达 Lv.${i + 2} 累计经验`
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

    <div class="section-title">等级名称（Lv.1–10，站点文案可自定义）</div>
    <div class="names-grid">
      <div v-for="(label, i) in form.names" :key="i" class="name-cell">
        <span class="tier-chip" :class="`tier-bg-${i + 1} tier-static-${i + 1}`">Lv.{{ i + 1 }}</span>
        <el-input v-model="form.names[i]" maxlength="12" :placeholder="DEFAULT_LEVEL_NAMES[i]" />
      </div>
    </div>

    <div class="section-title">难度名称（难度 1–10 档的展示文字，默认与等级名相同、可独立修改；题库徽章/筛选与建题选择器均显示此名）</div>
    <div class="names-grid">
      <div v-for="(label, i) in form.difficulty_names" :key="i" class="name-cell">
        <span class="tier-chip" :class="`tier-bg-${i + 1} tier-static-${i + 1}`">{{ i + 1 }}</span>
        <el-input v-model="form.difficulty_names[i]" maxlength="12" :placeholder="DEFAULT_LEVEL_NAMES[i]" />
      </div>
    </div>

    <div class="section-title">升级阈值（累计经验；默认 = 每档同难度题约 200 题）</div>
    <div class="thresholds-grid">
      <el-form-item v-for="(t, i) in form.thresholds" :key="i" :label="thresholdLabel(i)" label-position="top">
        <el-input-number v-model="form.thresholds[i]" :min="1" :step="50" :controls="false" style="width: 120px" />
      </el-form-item>
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

    <div class="section-title">特效开关（等级徽章动画/光效；关闭后该档退静态样式）</div>
    <div class="effects-row">
      <el-switch v-model="form.effects_enabled" active-text="全局启用" />
      <template v-if="form.effects_enabled">
        <el-checkbox
          v-for="i in 10"
          :key="i"
          v-model="form.effects_tiers[i - 1]"
          class="effect-check"
          :disabled="!form.effects_enabled"
        >
          Lv.{{ i }}
        </el-checkbox>
      </template>
      <span v-else class="dim">已全局关闭：全站等级徽章显示静态样式（题目难度徽章不受影响，恒为静态）</span>
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

.tier-chip {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 3px 8px;
  border-radius: 999px;
  color: #fff;
  font-size: var(--soj-fs-small);
  font-weight: var(--soj-fw-regular); /* 徽章字重全局改常规（2026-10-22） */
}

.thresholds-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 220px));
  gap: 4px 20px;
}

.decay-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.effects-row {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

.effect-check {
  margin-right: 0;
}
</style>

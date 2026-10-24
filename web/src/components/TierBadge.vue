<script setup lang="ts">
// 十阶色轴徽章（v3-full 静态重设计 2026-10-24）：学生等级与题目难度共用一条色轴
// 与浅色胶囊形制（底 11% / 边 25% / 字深一档，第 10 档极境蓝青渐变实底白字）。
// kind=level → ulv-*；kind=difficulty → diff-*。旧特效体系（光晕/呼吸星/流光、
// 后台特效开关分支）已退役——levels store 的 effectsOn 保留字段但不再影响渲染。
import { computed } from 'vue'

import { useLevelsStore } from '@/stores/levels'

const props = withDefaults(
  defineProps<{
    tier: number // 1–10
    kind?: 'level' | 'difficulty'
    label?: string // 覆盖默认文案（等级名 / 难度名）
    size?: 'sm' | 'md' | 'lg'
  }>(),
  { kind: 'difficulty', size: 'sm', label: undefined },
)

const levels = useLevelsStore()
const clamped = computed(() => Math.min(10, Math.max(1, Math.round(props.tier || 1))))

const text = computed(() => {
  if (props.label) return props.label
  if (props.kind === 'level') return `✦ Lv.${clamped.value} ${levels.nameOf(clamped.value)}`
  return levels.difficultyNameOf(clamped.value)
})

const cls = computed(() => {
  const family = props.kind === 'level' ? 'ulv' : 'diff'
  return ['tier-badge', `tier-${props.size}`, `${family}-tag`, `${family}-${clamped.value}`]
})
</script>

<template>
  <span :class="cls"><slot>{{ text }}</slot></span>
</template>

<style scoped>
.tier-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  white-space: nowrap;
  border-radius: 999px;
  font-weight: var(--soj-fw-bold);
  line-height: 1.4;
  vertical-align: middle;
}

/* sm = v3 .tier 基准档（12px/700，2px 10px）——表格/榜单/顶栏通用 */
.tier-sm {
  font-size: var(--soj-fs-12);
  padding: 2px 10px;
}

.tier-md {
  font-size: var(--soj-fs-13);
  padding: 3px 11px;
}

.tier-lg {
  font-size: 15px;
  padding: 4px 14px;
}
</style>

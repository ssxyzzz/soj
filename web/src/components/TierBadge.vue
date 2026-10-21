<script setup lang="ts">
// 十阶色轴徽章（v0.11）：学生等级与题目难度共用一条色轴（--soj-tier-1..10）。
// kind=level 走特效链（受后台等级面板特效开关控制）；kind=difficulty 恒静态
//（低档纯色/描边、高档静态渐变，无动画），色值与同档等级一致。
import { computed } from 'vue'

import { useLevelsStore } from '@/stores/levels'

const props = withDefaults(
  defineProps<{
    tier: number // 1–10
    kind?: 'level' | 'difficulty'
    label?: string // 覆盖默认文案（等级名 / D难度）
    size?: 'sm' | 'md' | 'lg'
  }>(),
  { kind: 'difficulty', size: 'sm', label: undefined },
)

const levels = useLevelsStore()
const clamped = computed(() => Math.min(10, Math.max(1, Math.round(props.tier || 1))))

const text = computed(() => {
  if (props.label) return props.label
  if (props.kind === 'level') return `Lv.${clamped.value} ${levels.nameOf(clamped.value)}`
  return `D${clamped.value}`
})

const cls = computed(() => {
  const arr = ['tier-badge', `tier-${props.size}`, `tier-bg-${clamped.value}`]
  if (props.kind === 'level' && levels.effectsOn(clamped.value)) {
    arr.push(`tier-fx-${clamped.value}`)
  } else {
    arr.push(`tier-static-${clamped.value}`)
  }
  return arr
})
</script>

<template>
  <span :class="cls">{{ text }}</span>
</template>

<style scoped>
.tier-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  white-space: nowrap;
  border-radius: 999px;
  color: #fff;
  font-weight: var(--soj-fw-emphasis);
  line-height: 1;
  vertical-align: middle;
  /* 亮银/亮金等浅档渐变上保白字可读 */
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
}
.tier-sm {
  font-size: var(--soj-fs-tag);
  padding: 3px 10px;
}
.tier-md {
  font-size: var(--soj-fs-body);
  padding: 4px 12px;
}
.tier-lg {
  font-size: var(--soj-fs-18);
  padding: 6px 18px;
}
</style>

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
  return levels.difficultyNameOf(clamped.value)
})

const cls = computed(() => {
  if (props.kind === 'level') {
    const arr = ['tier-badge', `tier-${props.size}`, `tier-bg-${clamped.value}`]
    arr.push(levels.effectsOn(clamped.value) ? `tier-fx-${clamped.value}` : `tier-static-${clamped.value}`)
    return arr
  }
  // 难度：浅色 tag 风（与站点标签同浓度）+ 后四档渐进微特效（极境暗黑深邃）
  const arr = ['tier-badge', `tier-${props.size}`, 'diff-tag', `diff-${clamped.value}`]
  const fx = ['', '', '', '', '', '', 'diff-g7', 'diff-g8', 'diff-g9', 'diff-g10'][clamped.value - 1]
  if (fx) arr.push(fx)
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
  font-weight: var(--soj-fw-regular);
  line-height: 1;
  vertical-align: middle;
  /* 亮银/亮金等浅档渐变上保白字可读 */
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
}

/* 难度（浅色 tag 风）：覆盖白字底色——深彩字、无投影；极境暗黑单独回白字幽光 */
.tier-badge.diff-tag {
  color: color-mix(in srgb, var(--dc) 72%, #2c3e50);
  text-shadow: none;
}
.tier-badge.diff-g10 {
  color: #e8eaf6;
  text-shadow: 0 0 6px rgba(120, 130, 220, 0.45);
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

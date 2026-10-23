<script setup lang="ts">
// 十阶色轴徽章（v0.11；v6.2 渲染重制）：学生等级与题目难度共用一条色轴与浅色 tag 体系。
// kind=level → ulv-*（浅色，浓度高于难度 chip；特效开时光晕/呼吸星逐级递增，极境彩色渐变）；
// kind=difficulty → diff-*（浅色，10% 浓度；后四档渐进微特效，极境暗黑深邃）。
// 两者均受后台等级面板特效开关控制（难度恒浅色，特效开关只影响等级）。
import { computed } from 'vue'

import { LEVEL_STARS, useLevelsStore } from '@/stores/levels'

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
  if (props.kind === 'level') return `Lv.${clamped.value} ${levels.nameOf(clamped.value)}`
  return levels.difficultyNameOf(clamped.value)
})

const cls = computed(() => {
  // 关特效（等级侧全局/逐级关）：等级徽章与难度徽章样式完全一致（diff-* 浅色 + 高档微特效）
  const diffFx = ['', '', '', '', '', '', 'diff-g7', 'diff-g8', 'diff-g9', 'diff-g10'][clamped.value - 1]
  if (props.kind === 'level' && !levels.effectsOn(clamped.value)) {
    const arr = ['tier-badge', `tier-${props.size}`, 'diff-tag', `diff-${clamped.value}`]
    if (diffFx) arr.push(diffFx)
    return arr
  }
  if (props.kind === 'level') {
    // 开特效：浅色 tag 体系（ulv-*，浓度高于难度 chip），光晕/呼吸星逐级递增，极境彩色渐变
    const arr = ['tier-badge', `tier-${props.size}`, 'ulv-tag', `ulv-${clamped.value}`, `ulv-e${clamped.value}`]
    return arr
  }
  // 难度：浅色 tag 风（与站点标签同浓度）+ 后四档渐进微特效（极境暗黑深邃）
  const arr = ['tier-badge', `tier-${props.size}`, 'diff-tag', `diff-${clamped.value}`]
  if (diffFx) arr.push(diffFx)
  return arr
})

// 呼吸星（仅等级 + 特效开）：数量/大小/位置/错拍延迟按档配置
const stars = computed(() => {
  if (props.kind !== 'level' || !levels.effectsOn(clamped.value)) return []
  return (LEVEL_STARS[clamped.value] ?? []).map((s) => {
    let style = `font-size:${s.s}px;animation-delay:${s.d}s;`
    if (s.t != null) style += `top:${s.t}px;`
    if (s.l != null) style += `left:${s.l}px;`
    if (s.r != null) style += `right:${s.r}px;`
    if (s.b != null) style += `bottom:${s.b}px;`
    return style
  })
})
</script>

<template>
  <span :class="cls"><slot>{{ text }}</slot><i
    v-for="(s, i) in stars"
    :key="i"
    class="lv-star"
    :style="s"
  >✦</i></span>
</template>

<style scoped>
.tier-badge {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  white-space: nowrap;
  /* 圆角矩形（2026-10-23 验收：难度与等级徽章统一站点 tag 口径，不出胶囊；
     SOJ 顶栏标识是独立类不受此影响） */
  border-radius: var(--el-border-radius-base);
  color: #fff;
  font-weight: var(--soj-fw-regular);
  line-height: 1;
  vertical-align: middle;
  /* 亮银/亮金等浅档渐变上保白字可读 */
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
}

/* 等级（浅色 tag 族）：覆盖白字底色为档位深彩字、无投影；极境彩色渐变回白字 */
.tier-badge.ulv-tag {
  color: color-mix(in srgb, var(--lc) 88%, #1f2d3d);
  text-shadow: none;
}
.tier-badge.ulv-e10 {
  color: #fff;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.25);
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

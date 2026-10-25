<script setup lang="ts">
// 十阶色轴徽章（v0.21 重设计）：学生等级与题目难度共用一条色轴、两种形制——
// kind=level → ulv-*（同色实色胶囊 + 特效链：描边/光晕呼吸/星芒/流光/心跳/极境渐变流动）；
// kind=difficulty → diff-*（同色静态浅色 tag + 7px 45° 旋转方块点缀（色随字，CSS 伪元素实现），
// 阅读面与站点统一、不炫；第 10 档极境 = 浅色彩虹款）。
// 呼吸星按档位内置配置（1–4 颗，位置/尺寸/错拍）；系统「减少动效」时动画整体退静态。
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
  if (props.kind === 'level') {
    // 实色 + 特效链（Lv.3 起挂 fx；fx 类按档位递进，极境由 .ulv-10 双类压轴）
    const arr = ['tier-badge', `tier-${props.size}`, 'ulv-tag', `ulv-${clamped.value}`]
    if (clamped.value >= 3) arr.push(`ulv-fx${clamped.value}`)
    return arr
  }
  return ['tier-badge', `tier-${props.size}`, 'diff-tag', `diff-${clamped.value}`]
})

// 呼吸星配置（仅等级）：t/l/r/b 定位 px、s 尺寸 px、d 错拍延迟 s
const STARS: Record<number, Array<{ t?: number; l?: number; r?: number; b?: number; s: number; d: number }>> = {
  5: [{ r: 5, t: 1, s: 7, d: 0 }],
  6: [
    { r: 5, t: 1, s: 8, d: 0 },
    { l: 6, b: 1, s: 6, d: 1.6 },
  ],
  7: [
    { r: 5, t: 1, s: 8, d: 0 },
    { l: 6, b: 1, s: 6.5, d: 1.6 },
  ],
  8: [
    { r: 5, t: 1, s: 8, d: 0 },
    { l: 6, b: 1, s: 6, d: 1.4 },
    { l: 6, t: 1, s: 6.5, d: 2.8 },
  ],
  9: [
    { r: 5, t: 1, s: 9, d: 0 },
    { l: 6, b: 1, s: 6, d: 1.4 },
    { l: 6, t: 1, s: 7, d: 2.8 },
  ],
  10: [
    { r: 5, t: 1, s: 9, d: 0 },
    { l: 6, b: 1, s: 7, d: 1.4 },
    { l: 6, t: 1, s: 6, d: 2.6 },
    { r: 6, b: 1, s: 8, d: 3.6 },
  ],
}

// 星的定位/错拍内联样式（仅等级 5+ 渲染）
const stars = computed(() => {
  if (props.kind !== 'level') return []
  return (STARS[clamped.value] ?? []).map((st) => {
    let style = `font-size:${st.s}px;animation-delay:${st.d}s;`
    if (st.t != null) style += `top:${st.t}px;`
    if (st.l != null) style += `left:${st.l}px;`
    if (st.r != null) style += `right:${st.r}px;`
    if (st.b != null) style += `bottom:${st.b}px;`
    return style
  })
})
</script>

<template>
  <span :class="cls"><slot>{{ text }}</slot><i
    v-for="(st, i) in stars"
    :key="i"
    class="lv-star"
    :style="st"
  >✦</i></span>
</template>

<style scoped>
.tier-badge {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  white-space: nowrap;
  border-radius: 999px;
  font-weight: var(--soj-fw-bold);
  line-height: 1.4;
  vertical-align: middle;
}

/* 难度（浅色 tag 族）：覆盖白字为档位深彩字、去阴影——与站点标签同语言、静态不炫 */
.tier-badge.diff-tag {
  color: var(--dct);
  text-shadow: none;
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

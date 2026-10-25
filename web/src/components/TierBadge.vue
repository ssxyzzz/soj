<script setup lang="ts">
// 十阶色轴徽章（v0.21 重设计；v9 换装 2026-10-25）：学生等级与题目难度共用一条色轴、两种形制——
// kind=level → ulv-*（同色实色胶囊 + 特效链：玻璃高光/光晕呼吸/星芒/流光/三级心跳/金脉/彩搏/粒子）；
// kind=difficulty → diff-*（同色静态浅色 tag + 7px 45° 旋转方块点缀（色随字，CSS 伪元素实现），
// 阅读面与站点统一、不炫；D9 黑金款；第 10 档极境 = 浅色彩虹 + 深金真边框）。
// 呼吸星/心跳粒子按档位内置配置（位置/尺寸/错拍/外飞向量）；系统「减少动效」时动画退静态、粒子隐藏。
// 拍板稿 = private_doc/preview/soj-tier-preview-b-heartbeat-v9.html。
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

// 等级徽章一律包一层 .lvw（inline-flex 透传壳）：Lv.9/10 的心跳粒子挂在壳上——
// .ulv-tag 的 overflow:hidden（流光裁剪用）会裁掉徽章内部的粒子，必须放外层
const isLevel = computed(() => props.kind === 'level')

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

// 心跳粒子配置（v9，仅 Lv.9 墨色 5 粒 / Lv.10 六彩 6 粒）：
// c 颜色、t/l/r/b 定位 px、dx/dy 外飞向量 px、sz 尺寸 px（缺省 4）
const PARTICLES: Record<
  number,
  Array<{ c: string; t?: number; l?: number; r?: number; b?: number; dx: number; dy: number; sz?: number }>
> = {
  9: [
    { c: '#1a140a', l: 7, t: -2, dx: -7, dy: -6 },
    { c: '#241c0e', r: 9, t: -2, dx: 7, dy: -5 },
    { c: '#171208', r: -2, t: 10, dx: 9, dy: 1, sz: 3 },
    { c: '#241c0e', l: 6, b: -2, dx: -6, dy: 6 },
    { c: '#171208', l: -2, t: 9, dx: -9, dy: 0, sz: 3 },
  ],
  10: [
    { c: '#e63946', l: 8, t: -2, dx: -7, dy: -6 },
    { c: '#ee7e1f', r: 10, t: -2, dx: 7, dy: -6 },
    { c: '#e9c963', r: -2, t: 10, dx: 9, dy: 1 },
    { c: '#3cb371', r: 9, b: -2, dx: 7, dy: 6 },
    { c: '#4a8dda', l: 7, b: -2, dx: -7, dy: 6 },
    { c: '#7e3e9e', l: -2, t: 9, dx: -9, dy: 0 },
  ],
}

// 粒子内联样式（颜色/定位/外飞向量；仅 Lv.9+ 渲染）
const particles = computed(() => {
  if (props.kind !== 'level') return []
  return (PARTICLES[clamped.value] ?? []).map((pt) => {
    let style = `color:${pt.c};--dx:${pt.dx}px;--dy:${pt.dy}px;`
    if (pt.t != null) style += `top:${pt.t}px;`
    if (pt.l != null) style += `left:${pt.l}px;`
    if (pt.r != null) style += `right:${pt.r}px;`
    if (pt.b != null) style += `bottom:${pt.b}px;`
    if (pt.sz != null) style += `width:${pt.sz}px;height:${pt.sz}px;`
    return style
  })
})
</script>

<template>
  <span v-if="isLevel" class="lvw">
    <span :class="cls"><slot>{{ text }}</slot><i
      v-for="(st, i) in stars"
      :key="i"
      class="lv-star"
      :style="st"
    >✦</i></span><i
      v-for="(pt, i) in particles"
      :key="i"
      class="lv-pt"
      :style="pt"
    />
  </span>
  <span v-else :class="cls"><slot>{{ text }}</slot></span>
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

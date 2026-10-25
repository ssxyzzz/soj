<script setup lang="ts">
// 主页专属鼠标氛围特效（simple408.cn 同款，2026-10-25）：青色光斑惯性跟随（lerp 拖尾）
// + 移动时的细涟漪环。teleport 到 body 末尾、z-index -1——压在青网格画布（body::after）
// 之上、页面内容（#app）之下：网格被光斑「照亮」，白卡与文字不接光、零交互干扰。
// 仅 HomeView 挂载此组件，其他页面保持纯净；prefers-reduced-motion 时不启用。
import { onBeforeUnmount, onMounted, ref } from 'vue'

const layer = ref<HTMLElement>()
const spot = ref<HTMLElement>()

// 目标点（鼠标）/ 当前点（光斑）；rAF 里按 0.1 缓动逼近产生拖尾波澜感
let tx = -600
let ty = -600
let cx = -600
let cy = -600
let raf = 0
let running = false
let seen = false // 首次移动后才显示光斑（加载时光斑不可见）
let lastRing = 0

function tick() {
  cx += (tx - cx) * 0.1
  cy += (ty - cy) * 0.1
  if (spot.value) {
    spot.value.style.transform = `translate3d(${cx - 250}px, ${cy - 250}px, 0)`
  }
  // 追平后停帧省电；下次 mousemove 重启
  if (Math.abs(tx - cx) < 0.5 && Math.abs(ty - cy) < 0.5) {
    running = false
    return
  }
  raf = requestAnimationFrame(tick)
}

function spawnRing(x: number, y: number) {
  if (!layer.value) return
  const el = document.createElement('i')
  el.className = 'cursor-ripple-ring'
  el.style.left = `${x}px`
  el.style.top = `${y}px`
  layer.value.appendChild(el)
  el.addEventListener('animationend', () => el.remove())
}

function onMove(e: MouseEvent) {
  tx = e.clientX
  ty = e.clientY
  if (!seen) {
    seen = true
    // 首次出现直接落在鼠标处，避免从屏外横穿
    cx = tx
    cy = ty
    spot.value?.classList.add('on')
  }
  if (!running) {
    running = true
    raf = requestAnimationFrame(tick)
  }
  // 涟漪节流：140ms 一环，移动才生（静止不泛涟漪）
  const now = performance.now()
  if (now - lastRing > 140) {
    lastRing = now
    spawnRing(tx, ty)
  }
}

function onLeave() {
  spot.value?.classList.remove('on')
  seen = false
}

function bind() {
  window.addEventListener('mousemove', onMove, { passive: true })
  document.documentElement.addEventListener('mouseleave', onLeave)
}

function unbind() {
  window.removeEventListener('mousemove', onMove)
  document.documentElement.removeEventListener('mouseleave', onLeave)
}

onMounted(() => {
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
  bind()
})

onBeforeUnmount(() => {
  unbind()
  if (raf) cancelAnimationFrame(raf)
})
</script>

<template>
  <teleport to="body">
    <div ref="layer" class="cursor-fx" aria-hidden="true">
      <div ref="spot" class="cursor-spot" />
    </div>
  </teleport>
</template>

<style scoped>
/* 承载层：铺满视口、负 z 沉底（body::after 网格之上、#app 内容之下）、不接交互 */
.cursor-fx {
  position: fixed;
  inset: 0;
  z-index: -1;
  pointer-events: none;
  overflow: hidden;
}

/* 青色光斑：外圈青晕（照亮网格）+ 内圈主蓝亮核，缓动跟随 */
.cursor-spot {
  position: absolute;
  left: 0;
  top: 0;
  width: 500px;
  height: 500px;
  border-radius: 50%;
  opacity: 0;
  transition: opacity 0.6s ease;
  background:
    radial-gradient(closest-side, rgba(45, 104, 239, 0.07), transparent 70%),
    radial-gradient(closest-side, rgba(22, 167, 210, 0.1), transparent 65%);
  will-change: transform;
}

.cursor-spot.on {
  opacity: 1;
}
</style>

<style>
/* 涟漪环：JS 动态创建（无 scoped 属性），样式挂全局；600ms 扩散淡出后由 JS 移除 */
.cursor-ripple-ring {
  position: absolute;
  width: 24px;
  height: 24px;
  margin: -12px 0 0 -12px;
  border-radius: 50%;
  border: 1px solid rgba(22, 167, 210, 0.35);
  pointer-events: none;
  animation: cursor-ripple 0.6s ease-out forwards;
}

@keyframes cursor-ripple {
  from {
    transform: scale(0.4);
    opacity: 0.5;
  }
  to {
    transform: scale(9);
    opacity: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .cursor-ripple-ring {
    display: none;
  }
}
</style>

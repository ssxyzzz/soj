<script setup lang="ts">
// 首页专属光效（2026-10-25 重设计）：跟随光晕（520px 柔青斑 lerp 惯性拖尾，0.28 紧跟）
// + 移动时的低频涟漪环。网格显影与静态画布撤下由全站 AmbientGrid（MainLayout 挂载）
// 负责——本组件只叠首页的光，仅 HomeView 挂载；prefers-reduced-motion 时不启用。
import { onBeforeUnmount, onMounted, ref } from 'vue'

const layer = ref<HTMLElement>()
const spot = ref<HTMLElement>()

// 目标点（鼠标）/ 当前点（光斑）；0.28 紧跟（2026-10-25 手感定稿），保留一丝拖尾波澜
let tx = -600
let ty = -600
let cx = -600
let cy = -600
let raf = 0
let running = false
let seen = false // 首次移动后才显示（加载时不可见）
let lastRing = 0

function tick() {
  cx += (tx - cx) * 0.28
  cy += (ty - cy) * 0.28
  if (spot.value) {
    spot.value.style.transform = `translate3d(${cx - 260}px, ${cy - 260}px, 0)`
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
  el.className = 'ambient-ring'
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
  // 涟漪节流：220ms 一环，移动才生（静止不泛涟漪）
  const now = performance.now()
  if (now - lastRing > 220) {
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
    <div ref="layer" class="home-ambient-fx" aria-hidden="true">
      <div ref="spot" class="ambient-spot" />
    </div>
  </teleport>
</template>

<style scoped>
/* 承载层：铺满视口、负 z 沉底（#app 内容之下）、不接交互 */
.home-ambient-fx {
  position: fixed;
  inset: 0;
  z-index: -1;
  pointer-events: none;
  overflow: hidden;
}

/* 跟随光晕：大柔青斑（网格的「灯」），lerp 惯性拖尾；
   2026-10-25 用户反馈再淡一档：青 .14→.08、蓝核 .06→.03 */
.ambient-spot {
  position: absolute;
  left: 0;
  top: 0;
  width: 520px;
  height: 520px;
  border-radius: 50%;
  opacity: 0;
  transition: opacity 0.6s ease;
  background:
    radial-gradient(closest-side, rgba(45, 104, 239, 0.03), transparent 72%),
    radial-gradient(closest-side, rgba(122, 184, 196, 0.08), transparent 65%);
  will-change: transform;
}

.ambient-spot.on {
  opacity: 1;
}
</style>

<style>
/* 涟漪环：JS 动态创建（无 scoped 属性），样式挂全局；700ms 扩散淡出后由 JS 移除。
   2026-10-25 用户反馈再淡一档：描边 0.28→0.14、峰值透明度 0.4→0.22 */
.ambient-ring {
  position: absolute;
  width: 24px;
  height: 24px;
  margin: -12px 0 0 -12px;
  border-radius: 50%;
  border: 1px solid rgba(22, 167, 210, 0.14);
  pointer-events: none;
  animation: ambient-ring 0.7s ease-out forwards;
}

@keyframes ambient-ring {
  from {
    transform: scale(0.4);
    opacity: 0.22;
  }
  to {
    transform: scale(8);
    opacity: 0;
  }
}
</style>

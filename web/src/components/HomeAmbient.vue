<script setup lang="ts">
// 首页专属氛围特效（2026-10-25 重设计，替代 CursorRipple）：参考 simple408.cn 的
// 工程图纸气质——静态网格画布撤下，改为「图纸在灯下显影」：
//   · 跟随光晕：大柔青斑 lerp 惯性拖尾（沿用原跟随机制）；
//   · 局部网格：全屏 44px 网格层，radial mask 圆心 = 光斑位置（rAF 同步 --gx/--gy
//     CSS 变量，mask 走合成层不触发 layout）——网格只在鼠标周围显现；
//   · 涟漪环：移动时节流泛环（降频降浓，仅点缀）。
// 挂载时给 body 加 home-ambient 类（撤全局青网格与双光晕，留冷白渐变底），卸载还原；
// prefers-reduced-motion 时整套不启用（body 类也不加，静态画布保留）。
// 仅 HomeView 挂载，其他页面保持原画布。
import { onBeforeUnmount, onMounted, ref } from 'vue'

const layer = ref<HTMLElement>()
const spot = ref<HTMLElement>()
const grid = ref<HTMLElement>()

// 目标点（鼠标）/ 当前点（光斑）；rAF 里按 0.1 缓动逼近产生拖尾波澜感
let tx = -600
let ty = -600
let cx = -600
let cy = -600
let raf = 0
let running = false
let seen = false // 首次移动后才显示（加载时不可见）
let lastRing = 0

function paint() {
  if (spot.value) {
    spot.value.style.transform = `translate3d(${cx - 260}px, ${cy - 260}px, 0)`
  }
  if (grid.value) {
    grid.value.style.setProperty('--gx', `${cx}px`)
    grid.value.style.setProperty('--gy', `${cy}px`)
  }
}

function tick() {
  // 0.28：紧跟鼠标（2026-10-25 用户反馈「更跟手」回调 0.1），保留一丝拖尾波澜
  cx += (tx - cx) * 0.28
  cy += (ty - cy) * 0.28
  paint()
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
    paint()
    spot.value?.classList.add('on')
    grid.value?.classList.add('on')
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
  grid.value?.classList.remove('on')
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
  document.body.classList.add('home-ambient')
  bind()
})

onBeforeUnmount(() => {
  unbind()
  if (raf) cancelAnimationFrame(raf)
  document.body.classList.remove('home-ambient') // 离开首页还原静态画布
})
</script>

<template>
  <teleport to="body">
    <div ref="layer" class="home-ambient-fx" aria-hidden="true">
      <div ref="spot" class="ambient-spot" />
      <div ref="grid" class="ambient-grid" />
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

/* 跟随光晕：大柔青斑（网格的「灯」），lerp 惯性拖尾 */
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
    radial-gradient(closest-side, rgba(45, 104, 239, 0.06), transparent 72%),
    radial-gradient(closest-side, rgba(122, 184, 196, 0.14), transparent 65%);
  will-change: transform;
}

.ambient-spot.on {
  opacity: 1;
}

/* 局部网格：44px 双向线全屏铺底，radial mask 圆心跟随光斑——网格只在鼠标周围显影；
   --gx/--gy 由 rAF 写入（组件根上无 scoped 命中问题：变量挂在 ref 元素自身） */
.ambient-grid {
  position: absolute;
  inset: 0;
  --gx: 50vw;
  --gy: 40vh;
  background:
    linear-gradient(rgba(122, 184, 196, 0.18) 1px, transparent 1px),
    linear-gradient(90deg, rgba(122, 184, 196, 0.18) 1px, transparent 1px);
  background-size: 44px 44px;
  -webkit-mask-image: radial-gradient(circle 300px at var(--gx) var(--gy), #000 25%, transparent 100%);
  mask-image: radial-gradient(circle 300px at var(--gx) var(--gy), #000 25%, transparent 100%);
  opacity: 0;
  transition: opacity 0.6s ease;
}

.ambient-grid.on {
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

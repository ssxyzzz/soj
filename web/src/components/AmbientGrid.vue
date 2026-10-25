<script setup lang="ts">
// 全站网格氛围（2026-10-25 修复⑯）：静态青网格与双光晕退役（挂载时 body 加 ambient-on
// 撤下，冷白渐变底保留），改为「图纸在灯下显影」的网格层——radial mask 圆心跟随光斑
// （rAF 同步 --gx/--gy CSS 变量，mask 走合成层），网格只在鼠标周围显现、越远越淡。
// 全站常驻：MainLayout 挂载，路由切换不卸载。光斑与涟漪仅首页（HomeAmbient）。
// prefers-reduced-motion 时整套不启用（body 类也不加，保留原静态画布）。
import { onBeforeUnmount, onMounted, ref } from 'vue'

const grid = ref<HTMLElement>()

// 目标点（鼠标）/ 当前点（mask 圆心）；0.28 紧跟（2026-10-25 手感定稿）
let tx = -600
let ty = -600
let cx = -600
let cy = -600
let raf = 0
let running = false
let seen = false // 首次移动后才显示（加载时不可见）

function paint() {
  if (grid.value) {
    grid.value.style.setProperty('--gx', `${cx}px`)
    grid.value.style.setProperty('--gy', `${cy}px`)
  }
}

function tick() {
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

function onMove(e: MouseEvent) {
  tx = e.clientX
  ty = e.clientY
  if (!seen) {
    seen = true
    cx = tx
    cy = ty
    paint()
    grid.value?.classList.add('on')
  }
  if (!running) {
    running = true
    raf = requestAnimationFrame(tick)
  }
}

function onLeave() {
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
  document.body.classList.add('ambient-on')
  bind()
})

onBeforeUnmount(() => {
  unbind()
  if (raf) cancelAnimationFrame(raf)
  document.body.classList.remove('ambient-on')
})
</script>

<template>
  <teleport to="body">
    <div aria-hidden="true" class="ambient-grid-host">
      <div ref="grid" class="ambient-grid" />
    </div>
  </teleport>
</template>

<style scoped>
.ambient-grid-host {
  position: fixed;
  inset: 0;
  z-index: -1;
  pointer-events: none;
  overflow: hidden;
}

/* 局部网格：44px 双向线全屏铺底，radial mask 圆心跟随光斑——网格只在鼠标周围显影。
   2026-10-25 用户反馈：多档 alpha 落差做「越远越淡」的距离渐隐（560px 半径） */
.ambient-grid {
  position: absolute;
  inset: 0;
  --gx: 50vw;
  --gy: 40vh;
  background:
    linear-gradient(rgba(122, 184, 196, 0.18) 1px, transparent 1px),
    linear-gradient(90deg, rgba(122, 184, 196, 0.18) 1px, transparent 1px);
  background-size: 44px 44px;
  -webkit-mask-image: radial-gradient(
    circle 560px at var(--gx) var(--gy),
    rgba(0, 0, 0, 0.85) 0%,
    rgba(0, 0, 0, 0.55) 40%,
    rgba(0, 0, 0, 0.25) 70%,
    transparent 100%
  );
  mask-image: radial-gradient(
    circle 560px at var(--gx) var(--gy),
    rgba(0, 0, 0, 0.85) 0%,
    rgba(0, 0, 0, 0.55) 40%,
    rgba(0, 0, 0, 0.25) 70%,
    transparent 100%
  );
  opacity: 0;
  transition: opacity 0.6s ease;
}

.ambient-grid.on {
  opacity: 1;
}
</style>

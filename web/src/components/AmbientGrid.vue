<script setup lang="ts">
// 全站网格氛围（2026-10-25 修复⑯）：静态青网格与双光晕退役（挂载时 body 加 ambient-on
// 撤下，冷白渐变底保留），改为「图纸在灯下显影」的网格层——radial mask 圆心跟随光斑
// （rAF 同步 --gx/--gy CSS 变量，mask 走合成层），网格只在鼠标周围显现、越远越淡。
// 常驻页面挂载：MainLayout 全站 + 登录页（2026-11-02 随「登录特效与首页一致」同挂），
// 路由切换不卸载。光斑与涟漪由 HomeAmbient 提供（首页/登录页）。
// prefers-reduced-motion 时整套不启用（body 类也不加，保留原静态画布）。
import { onBeforeUnmount, onMounted, ref } from 'vue'

import { useListener } from '@/composables/useWindowListener'

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

// 监听走 useListener（v0.64 病根②收口）：setup 顶层声明即绑定、作用域卸载自动摘
const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
if (!reduced) {
  useListener(window, 'mousemove', onMove, { passive: true })
  useListener(document.documentElement, 'mouseleave', onLeave)
}

onMounted(() => {
  if (reduced) return
  document.body.classList.add('ambient-on')
})

onBeforeUnmount(() => {
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
   2026-10-25 用户反馈：多档 alpha 落差做「越远越淡」的距离渐隐（560px 半径）
   2026-10-30 用户反馈：再弱化一档——线 alpha 0.18→0.12，mask 各档同步压低（峰值存在感约减半），半径不动 */
.ambient-grid {
  position: absolute;
  inset: 0;
  --gx: 50vw;
  --gy: 40vh;
  background:
    linear-gradient(rgb(var(--soj-teal-soft-rgb) / 0.12) 1px, transparent 1px),
    linear-gradient(90deg, rgb(var(--soj-teal-soft-rgb) / 0.12) 1px, transparent 1px);
  background-size: 44px 44px;
  -webkit-mask-image: radial-gradient(
    circle 560px at var(--gx) var(--gy),
    rgba(0, 0, 0, 0.6) 0%,
    rgba(0, 0, 0, 0.38) 40%,
    rgba(0, 0, 0, 0.16) 70%,
    transparent 100%
  );
  mask-image: radial-gradient(
    circle 560px at var(--gx) var(--gy),
    rgba(0, 0, 0, 0.6) 0%,
    rgba(0, 0, 0, 0.38) 40%,
    rgba(0, 0, 0, 0.16) 70%,
    transparent 100%
  );
  opacity: 0;
  transition: opacity 0.6s ease;
}

.ambient-grid.on {
  opacity: 1;
}
</style>

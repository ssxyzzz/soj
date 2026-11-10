<script setup lang="ts">
// 题面标注工具条（v0.84）：哑组件——5 色圆点（画板 BoardView colorSlots 同款视觉）
// + 橡皮擦（lucide Eraser，BoardView 已有先例）。fixed 坐标由 useStatementMark 按
// 选区位置下发（组件只管画）；mousedown.prevent 防点击瞬间丢选区，工具条稳定停留
// 到应用/擦除完成。
import { Eraser } from 'lucide-vue-next'

import { MARK_SLOTS, type MarkSlot } from '@/utils/statement-mark'

const emit = defineEmits<{ (e: 'color', slot: MarkSlot): void; (e: 'erase'): void }>()
</script>

<template>
  <div class="md-mark-bar" role="toolbar" aria-label="题面标注">
    <button
      v-for="c in MARK_SLOTS"
      :key="c.slot"
      type="button"
      class="mk-dot"
      :class="`mk-dot-${c.slot}`"
      :title="`标为${c.name}`"
      :aria-label="`标为${c.name}`"
      @mousedown.prevent
      @click="emit('color', c.slot)"
    />
    <span class="sep" aria-hidden="true" />
    <button
      type="button"
      class="eraser"
      title="擦除选中标注"
      aria-label="擦除选中标注"
      @mousedown.prevent
      @click="emit('erase')"
    >
      <Eraser :size="15" />
    </button>
  </div>
</template>

<style scoped>
.md-mark-bar {
  position: fixed;
  z-index: 30; /* 高于 sticky 卡片/工具条、低于 EP 弹层（~2000） */
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 6px 10px; /* 圆点命中块 28×28 后由 6px 竖 padding 维持工具条原高度（原 7px 内高由 26px 橡皮擦主导） */
  background: var(--soj-white);
  border: 1px solid var(--soj-line-2);
  border-radius: var(--soj-radius-pill);
  box-shadow: 0 10px 28px color-mix(in srgb, var(--soj-navy) 14%, transparent);
  user-select: none; /* 拖选扫过工具条本身不产生选区 */
}

/* 圆点：画板 swatch 同款视觉（色圆 18px + 白描边 2px + hairline ring），整体画在
   ::before 上；按钮本体 = 28×28 透明方块命中区——点圆点周围一圈空白都算点到该色
   （v0.90.2 用户拍板）。三个坑实测记档：①命中块不设圆角——border-radius 区域外
   浏览器不做命中，圆角会让方块四角失灵；②类名 mk-dot 避撞 main.css 全局 .dot
   （状态圆点 6px/50%）；③色圆不能画在按钮自身再靠 background-clip: content-box
   缩圆——content-box 裁出来的是方形，方角会从 ::before 圆环外露出来（v0.90.2
   生产实锤「方块套圆」，v0.90.3 改色圆整体入 ::before） */
.mk-dot {
  width: 28px;
  height: 28px;
  flex: none;
  display: block;
  padding: 0;
  border: none;
  background: transparent;
  cursor: pointer;
  position: relative;
  transition: transform 0.15s;
}

.mk-dot::before {
  content: '';
  position: absolute;
  inset: 5px;
  border-radius: var(--soj-radius-pill);
  background: var(--mk-dot);
  border: 2px solid var(--soj-white);
  box-shadow: 0 0 0 1px var(--soj-line-2);
}

.mk-dot:hover {
  transform: scale(1.15);
}

.mk-dot-ink {
  --mk-dot: var(--soj-ink);
}

.mk-dot-blue {
  --mk-dot: var(--soj-blue);
}

.mk-dot-teal {
  --mk-dot: var(--soj-teal);
}

.mk-dot-warning {
  --mk-dot: var(--soj-warning);
}

.mk-dot-danger {
  --mk-dot: var(--soj-danger);
}

.sep {
  width: 1px;
  height: 16px;
  background: var(--soj-line);
}

.eraser {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  flex: none;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--soj-ink-2);
  cursor: pointer;
  transition: all 0.15s;
}

.eraser:hover {
  color: var(--soj-blue);
  background: var(--soj-blue-soft);
}
</style>

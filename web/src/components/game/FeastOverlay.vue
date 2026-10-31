<template>
  <Teleport to="body">
    <Transition name="feast-fade">
      <div v-if="feast.visible" class="feast-root" :class="`feast-${feast.rarity}`" aria-hidden="true">
        <!-- 稀有度背景层 -->
        <div v-if="feast.rarity === 'epic' || feast.rarity === 'ssr'" class="feast-goldrain">
          <span v-for="i in 28" :key="'r' + i" class="gold-drop" :style="dropStyle(i)" />
        </div>
        <div v-if="feast.rarity === 'rare'" class="feast-fireworks">
          <span v-for="i in 12" :key="'f' + i" class="fw" :style="fwStyle(i)" />
        </div>
        <div v-if="feast.rarity === 'ssr'" class="feast-pillar" />

        <!-- AC 印章 -->
        <div class="feast-stamp" :class="{ perfect: isPerfect }">
          <div class="stamp-ring">
            <div class="stamp-text">{{ isPerfect ? 'PERFECT' : 'AC' }}</div>
          </div>
          <div v-if="isPerfect" class="stamp-sub">一次过 · 满分</div>
          <div v-if="feast.jade > 0" class="stamp-jade">
            +{{ feast.jade }} {{ currency }}
            <span v-if="feast.jadeMultFrom" class="mini-tag">{{ feast.jadeMultFrom }}</span>
          </div>
          <div v-if="feast.combo >= 2" class="stamp-combo">连击 ×{{ feast.combo }}</div>
        </div>

        <!-- 飞玉：首 AC 时从中心飞向顶栏余额 -->
        <span v-for="i in flyCount" :key="'j' + i" class="fly-jade" :style="flyStyle(i)" />
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
// AC 开箱特效（v0.41）：稀有度 60/30/9/1 抽取（首 AC 才有玉才开箱，普通 AC 小章）；
// 掉玉飞入顶栏余额、连击章、PERFECT 一次过评级、SSR 极境光柱。尊重 reduced-motion
//（粒子层隐藏，印章仍显示）。自动关闭 2.8s。

import { computed, ref, watch } from 'vue'

import { useGameFeastStore } from '@/stores/gameFeast'
import { playAC, playCoin, playPerfect, playSSR } from '@/utils/gameSounds'

const feast = useGameFeastStore()
const currency = ref('玉')

const isPerfect = computed(() => feast.jade > 0 && feast.score === 100)

const flyCount = computed(() => {
  if (!feast.jade) return 0
  return Math.max(4, Math.min(14, feast.jade))
})

watch(
  () => feast.visible,
  (v) => {
    if (!v) return
    // 音效：SSR/PERFECT 专属，其余随机 AC 琶音（连击变调）；首 AC 追加金币音
    if (feast.rarity === 'ssr') playSSR()
    else if (isPerfect.value) playPerfect()
    else if (feast.jade > 0 || feast.combo >= 2) playAC(feast.combo)
    if (feast.jade > 0) {
      fetch('/api/me/jade')
        .then((r) => r.json())
        .then((d) => {
          currency.value = d.data?.currency ?? '玉'
        })
      setTimeout(() => playCoin(), 900)
    }
    setTimeout(() => feast.close(), 2800)
  },
)

function dropStyle(i: number) {
  return {
    left: (i * 37) % 100 + '%',
    animationDelay: (i % 7) * 0.12 + 's',
    animationDuration: 1.4 + (i % 5) * 0.2 + 's',
  }
}

function fwStyle(i: number) {
  const ang = (i * 30 * Math.PI) / 180
  return {
    '--dx': Math.cos(ang) * 120 + 'px',
    '--dy': Math.sin(ang) * 120 + 'px',
    animationDelay: (i % 4) * 0.1 + 's',
  }
}

function flyStyle(i: number) {
  return {
    left: 40 + Math.random() * 20 + '%',
    top: 45 + Math.random() * 10 + '%',
    animationDelay: i * 0.07 + 's',
  }
}

</script>

<style scoped>
.feast-root {
  position: fixed;
  inset: 0;
  z-index: 3000;
  pointer-events: none;
  display: flex;
  align-items: center;
  justify-content: center;
}

.feast-stamp {
  position: relative;
  z-index: 2;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  animation: stampIn 0.45s cubic-bezier(0.2, 1.6, 0.4, 1) both;
}

.stamp-ring {
  width: 128px;
  height: 128px;
  border: 6px solid var(--soj-blue);
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 0 40px rgba(31, 73, 186, 0.35);
}

.feast-epic .stamp-ring,
.feast-ssr .stamp-ring {
  border-color: #f59e0b;
  box-shadow: 0 0 60px rgba(245, 158, 11, 0.5);
}

.feast-ssr .stamp-ring {
  border-width: 8px;
}

.stamp-text {
  font-size: 40px;
  font-weight: 900;
  color: var(--soj-blue);
  letter-spacing: 4px;
}

.feast-epic .stamp-text,
.feast-ssr .stamp-text {
  color: #b45309;
}

.stamp-sub {
  font-size: 13px;
  color: #b45309;
  font-weight: 700;
}

.stamp-jade {
  font-size: 20px;
  font-weight: 800;
  color: #047857;
  display: flex;
  align-items: center;
  gap: 6px;
  animation: jadePop 0.5s 0.25s both;
}

.mini-tag {
  font-size: 11px;
  padding: 1px 8px;
  border-radius: 999px;
  background: #d1fae5;
  color: #065f46;
  font-weight: 700;
}

.mini-tag.bless {
  background: #fef3c7;
  color: #b45309;
}

.stamp-combo {
  font-size: 22px;
  font-weight: 900;
  color: #dc2626;
  text-shadow: 0 0 12px rgba(220, 38, 38, 0.3);
  animation: jadePop 0.4s 0.35s both;
}

/* 飞玉 */
.fly-jade {
  position: absolute;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: radial-gradient(circle at 35% 35%, #6ee7b7, #059669);
  box-shadow: 0 0 10px rgba(5, 150, 105, 0.6);
  z-index: 3;
  animation: flyToTop 0.9s ease-in forwards;
  opacity: 0;
}

@keyframes flyToTop {
  0% {
    opacity: 1;
    transform: translate(0, 0) scale(1);
  }
  100% {
    opacity: 0.9;
    transform: translate(calc(50vw - 50% - 40px), -42vh) scale(0.5);
  }
}

/* 金雨 */
.feast-goldrain {
  position: absolute;
  inset: 0;
}

.gold-drop {
  position: absolute;
  top: -20px;
  width: 8px;
  height: 14px;
  border-radius: 3px;
  background: linear-gradient(180deg, #fde68a, #f59e0b);
  animation: goldFall linear both;
}

@keyframes goldFall {
  from {
    transform: translateY(-4vh) rotate(0deg);
    opacity: 1;
  }
  to {
    transform: translateY(104vh) rotate(540deg);
    opacity: 0.6;
  }
}

/* 烟花 */
.fw {
  position: absolute;
  left: 50%;
  top: 40%;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #f59e0b;
  animation: fwBurst 0.8s ease-out both;
}

@keyframes fwBurst {
  from {
    transform: translate(0, 0) scale(1.4);
    opacity: 1;
  }
  to {
    transform: translate(var(--dx), var(--dy)) scale(0.2);
    opacity: 0;
  }
}

/* SSR 光柱 */
.feast-pillar {
  position: absolute;
  left: 50%;
  top: 0;
  width: 180px;
  height: 100%;
  transform: translateX(-50%);
  background: linear-gradient(180deg, rgba(253, 230, 138, 0.55), rgba(245, 158, 11, 0.12) 60%, transparent);
  animation: pillarIn 0.6s ease-out both;
}

@keyframes pillarIn {
  from {
    opacity: 0;
    transform: translateX(-50%) scaleY(0.4);
  }
  to {
    opacity: 1;
    transform: translateX(-50%) scaleY(1);
  }
}

@keyframes stampIn {
  from {
    opacity: 0;
    transform: scale(2.2) rotate(-14deg);
  }
  60% {
    transform: scale(0.94) rotate(2deg);
  }
  to {
    opacity: 1;
    transform: scale(1) rotate(-4deg);
  }
}

@keyframes jadePop {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.feast-fade-enter-active,
.feast-fade-leave-active {
  transition: opacity 0.35s;
}

.feast-fade-enter-from,
.feast-fade-leave-to {
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {
  .fly-jade,
  .gold-drop,
  .fw,
  .feast-pillar {
    display: none;
  }
  .feast-stamp {
    animation: none;
  }
}
</style>

// 开箱特效全局状态（v0.41）：提交流程命中 AC 终态 → feast(game, score) 触发全屏
// 开箱（稀有度抽取 + 飞玉 + 连击 + 成就弹窗 + 音效）。MainLayout 挂唯一 FeastOverlay。

import { defineStore } from 'pinia'
import { ref } from 'vue'

import type { SubmissionGame } from '@/types'

export type FeastRarity = 'common' | 'rare' | 'epic' | 'ssr' | 'plain'

export const useGameFeastStore = defineStore('gameFeast', () => {
  const visible = ref(false)
  const rarity = ref<FeastRarity>('plain')
  const jade = ref(0) // 本次首 AC 发玉（0 = 非FirstAC 小 AC 章样式）
  const jadeDaily = ref(false)
  const jadeRevenge = ref(false)
  const combo = ref(0)
  const score = ref(0)
  const title = ref('')

  function feast(game: SubmissionGame | undefined, acScore: number, problemTitle: string) {
    score.value = acScore
    title.value = problemTitle
    jade.value = game?.jade_total ?? 0
    jadeDaily.value = !!game?.jade_daily
    jadeRevenge.value = !!game?.jade_revenge
    combo.value = game?.combo_cur ?? 0
    if (jade.value > 0) {
      // 开箱稀有度：60/30/9/1（SSR 极境时刻）
      const roll = Math.random() * 100
      rarity.value = roll < 1 ? 'ssr' : roll < 10 ? 'epic' : roll < 40 ? 'rare' : 'common'
    } else {
      rarity.value = 'plain' // 非 First AC：小 AC 章
    }
    visible.value = true
  }

  function close() {
    visible.value = false
  }

  return { visible, rarity, jade, jadeDaily, jadeRevenge, combo, score, title, feast, close }
})

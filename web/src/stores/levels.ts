import { ref } from 'vue'
import { defineStore } from 'pinia'

import { levelsApi } from '@/api'
import type { LevelConfigInfo } from '@/types'

// 等级配置默认名（后端不可达时的兜底显示，与 repo.DefaultLevelNames 对应）
export const DEFAULT_LEVEL_NAMES = ['启蒙', '入门', '弟子', '少侠', '侠客', '精英', '宗师', '大师', '传奇', '神话']

// 等级配置（v0.11）：进程内拉一次全局共享；名字/特效开关供徽章渲染，
// 阈值供进度条。拉取失败静默降级——徽章退静态默认名，不阻塞页面。
export const useLevelsStore = defineStore('levels', () => {
  const cfg = ref<LevelConfigInfo | null>(null)
  let fetching: Promise<void> | null = null

  async function ensure() {
    if (cfg.value) return
    if (!fetching) {
      fetching = levelsApi
        .config()
        .then((c) => {
          cfg.value = c
        })
        .catch(() => {})
        .finally(() => {
          fetching = null
        })
    }
    await fetching
  }

  function nameOf(tier: number): string {
    return cfg.value?.names[tier - 1] || DEFAULT_LEVEL_NAMES[tier - 1] || `Lv.${tier}`
  }

  // 特效开关：全局关或逐级关时徽章退静态样式；配置未就绪也先静态（就绪后响应式切回）
  function effectsOn(tier: number): boolean {
    const c = cfg.value
    if (!c) return false
    return c.effects_enabled && (c.effects_tiers[tier - 1] ?? true)
  }

  // 代码自动补全解锁等级（缺省 5）
  function autocompleteMinLevel(): number {
    return cfg.value?.autocomplete_min_level ?? 5
  }

  return { cfg, ensure, nameOf, effectsOn, autocompleteMinLevel }
})

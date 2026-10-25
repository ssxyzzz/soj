import { ref } from 'vue'
import { defineStore } from 'pinia'

import { levelsApi } from '@/api'
import type { LevelConfigInfo } from '@/types'

// 等级配置默认名（后端不可达时的兜底显示，与 repo.DefaultLevelNames 对应——
// 2026-10-22 两字段位版定稿：青铜→极境，取自王者荣耀段位体系）
export const DEFAULT_LEVEL_NAMES = ['青铜', '白银', '黄金', '铂金', '钻石', '星耀', '宗师', '王者', '传奇', '极境']

// v0.21 改版（2026-10-25）：等级徽章回归同轴实色炫彩版（特效链恒开、低速动画，
// reduced-motion 退静态）；难度徽章保持同色静态浅色。特效不再读 effects_* 开关
//（后台字段保留未消费），effectsOn 仅为兼容保留。

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

  // 强制重拉（root 后台保存等级配置后调用，徽章即时跟随新特效开关）
  async function reload() {
    cfg.value = null
    await ensure()
  }

  function nameOf(tier: number): string {
    return cfg.value?.names[tier - 1] || DEFAULT_LEVEL_NAMES[tier - 1] || `Lv.${tier}`
  }

  // 难度档展示名（默认与等级名相同；root 后台独立可改）
  function difficultyNameOf(tier: number): string {
    return cfg.value?.difficulty_names?.[tier - 1] || DEFAULT_LEVEL_NAMES[tier - 1] || String(tier)
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

  return { cfg, ensure, reload, nameOf, difficultyNameOf, effectsOn, autocompleteMinLevel }
})

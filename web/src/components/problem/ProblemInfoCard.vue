<script setup lang="ts">
// 题目信息卡（v3-full 重制，题库详情页与赛事做题页共用）：kv 三格（提交/通过/通过率）+
// 难度档徽章 + 标签 chips 平铺（弃悬停浮层）+ 来源徽章链。
import { computed } from 'vue'

import RailCard from '@/components/RailCard.vue'
import TierBadge from '@/components/TierBadge.vue'
import { useMetaTreeStore } from '@/stores/metaTree'
import { useTagTreeStore } from '@/stores/tagtree'
import type { ProblemDetail } from '@/types'

const props = defineProps<{ problem: ProblemDetail }>()

const tagStore = useTagTreeStore()
tagStore.ensure()

const metaStore = useMetaTreeStore()
metaStore.ensure()

const tagIds = computed(() => props.problem.tags ?? [])

// 来源节点（2026-10-25 双树）：原徽章链退役，来源=标签树节点中性色 chips
const metaIds = computed(() => props.problem.meta_tags ?? [])

const acceptRate = computed(() => {
  const p = props.problem
  if (!p?.submit_count) return '—'
  const pct = ((p.accepted_count ?? 0) / p.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
})
</script>

<template>
  <RailCard ov="概览" title="题目信息" class="aside-card">
    <div class="soj-kv kv3">
      <div class="k"><b>{{ problem.submit_count ?? 0 }}</b><span>提交</span></div>
      <div class="k"><b>{{ problem.accepted_count ?? 0 }}</b><span>通过</span></div>
      <div class="k"><b>{{ acceptRate }}</b><span>通过率</span></div>
    </div>
    <div class="diff-row">
      <TierBadge :tier="problem.difficulty ?? 3" kind="difficulty" />
      <span class="hint">难度第 {{ problem.difficulty ?? 3 }} 档</span>
    </div>
    <div v-if="tagIds.length" class="tag-row">
      <span
        v-for="id in tagIds"
        :key="id"
        class="soj-tagchip chip"
        :class="`lv-${tagStore.levelOf(id)}`"
        :style="{ color: 'var(--lv)', background: 'var(--lv-bg)', borderColor: 'var(--lv-border)' }"
      >{{ tagStore.nameOf(id) }}</span>
    </div>
    <div v-else class="hint">暂无标签</div>
    <!-- 来源（2026-10-25 双树）：标签树节点 chips（单色中性），紧随知识点标签 -->
    <div v-if="metaIds.length" class="tag-row src-row">
      <span
        v-for="id in metaIds"
        :key="id"
        class="soj-tagchip chip src-chip"
      >{{ metaStore.nameOf(id) }}</span>
    </div>
  </RailCard>
</template>

<style scoped>
/* v3：kv 三等分 */
.kv3 {
  grid-template-columns: repeat(3, 1fr);
}

.kv3 .k b {
  font-size: var(--soj-fs-22);
}

.diff-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
}

.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
}

.tag-row {
  display: flex;
  flex-wrap: wrap;
  margin-top: 10px;
}

.chip {
  margin: 2px 4px 2px 0;
  cursor: default;
}

.src-row {
  margin-top: 10px;
}

/* 来源节点：单色中性（石墨蓝），与知识树五级色区分 */
.src-chip {
  color: var(--soj-ink-2);
  background: var(--soj-surface-2);
  border-color: rgba(11, 31, 69, 0.13);
}
</style>

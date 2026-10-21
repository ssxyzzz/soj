<script setup lang="ts">
// 题目信息卡（题库详情页与赛事做题页共用）：提交/通过/通过率统计 + 标签浮层。
// 标签浮层默认只显示「显示标签」入口。悬停即展、移开即收；点击则锁定，
// 移开后保留 5 秒再收起（再点一次立即收起）。
import { computed, onUnmounted, ref } from 'vue'

import TierBadge from '@/components/TierBadge.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import type { ProblemDetail } from '@/types'

const props = defineProps<{ problem: ProblemDetail }>()

const tagStore = useTagTreeStore()

const tagIds = computed(() => props.problem.tags ?? [])

const acceptRate = computed(() => {
  const p = props.problem
  if (!p?.submit_count) return '—'
  const pct = ((p.accepted_count ?? 0) / p.submit_count) * 100
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
})

const tagsVisible = ref(false)
const tagsLatched = ref(false)
let tagsTimer: ReturnType<typeof setTimeout> | undefined

function openTags() {
  if (tagsTimer) {
    clearTimeout(tagsTimer)
    tagsTimer = undefined
  }
  tagsVisible.value = true
}

function scheduleCloseTags() {
  if (tagsTimer) clearTimeout(tagsTimer)
  if (tagsLatched.value) {
    tagsTimer = setTimeout(() => {
      tagsVisible.value = false
      tagsLatched.value = false
    }, 5000)
  } else {
    tagsVisible.value = false
  }
}

function toggleTags() {
  if (tagsLatched.value) {
    tagsLatched.value = false
    if (tagsTimer) clearTimeout(tagsTimer)
    tagsVisible.value = false
  } else {
    tagsLatched.value = true
    openTags()
  }
}

onUnmounted(() => {
  if (tagsTimer) clearTimeout(tagsTimer)
})
</script>

<template>
  <el-card class="aside-card">
    <template #header><span>题目信息</span></template>
    <div class="info-stats">
      <span>提交 <b>{{ problem.submit_count ?? 0 }}</b></span>
      <span>通过 <b>{{ problem.accepted_count ?? 0 }}</b></span>
      <span>通过率 <b>{{ acceptRate }}</b></span>
    </div>
    <!-- 难度（v0.11）：静态十阶色轴徽章，位于标签区上方 -->
    <div class="info-diff">
      <TierBadge :tier="problem.difficulty ?? 3" kind="difficulty" />
    </div>
    <div v-if="tagIds.length" class="info-tags">
      <el-popover :visible="tagsVisible" placement="bottom-start" popper-class="tags-pop">
        <template #reference>
          <span
            class="show-tags"
            @mouseenter="openTags"
            @mouseleave="scheduleCloseTags"
            @click="toggleTags"
          >
            显示标签
          </span>
        </template>
        <div class="tags-pop-body" @mouseenter="openTags" @mouseleave="scheduleCloseTags">
          <el-tag
            v-for="id in tagIds"
            :key="id"
            size="small"
            class="pop-tag"
            :class="`lv-${tagStore.levelOf(id)}`"
          >
            {{ tagStore.nameOf(id) }}
          </el-tag>
        </div>
      </el-popover>
    </div>
    <div v-else class="dim">暂无标签</div>
  </el-card>
</template>

<style scoped>
.info-stats {
  display: flex;
  gap: 14px;
}

.info-stats b {
  font-weight: var(--soj-fw-emphasis);
}

.info-diff {
  margin-top: 10px;
}

.info-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

/* 「显示标签」入口 */
.show-tags {
  color: var(--el-color-primary);
  cursor: pointer;
  font-size: var(--soj-fs-body);
}

.show-tags:hover {
  text-decoration: underline;
}
</style>

<style>
/* 标签浮层（el-popover 挂 body，需非 scoped）：同表格标签栏的五级配色 */
.tags-pop-body {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.tags-pop-body .el-tag {
  background: var(--lv-bg);
  border-color: var(--lv-border);
  color: var(--el-text-color-primary);
}
</style>

<script setup lang="ts">
// 统一面包屑（v0.77 班级链路拍板）：末段 = 当前层，承担原 20px 名称视觉（不可点）；
// 前段 router-link 可点直达（班级页单段即名称；作业/考试两段；做题页末段题目名，
// compact 档全段 16px 只做位置导航不做标题）。分隔符按用户拍板字面「>>>」。
export interface CrumbItem {
  label: string
  to?: string
}

withDefaults(defineProps<{ items: CrumbItem[]; compact?: boolean }>(), { compact: false })
</script>

<template>
  <div class="soj-crumbs" :class="{ compact }">
    <template v-for="(it, i) in items" :key="i">
      <span v-if="i" class="sep" aria-hidden="true">&gt;&gt;&gt;</span>
      <router-link v-if="it.to && i < items.length - 1" :to="it.to" class="c-link">{{ it.label }}</router-link>
      <span v-else class="c-cur">{{ it.label }}</span>
    </template>
  </div>
</template>

<style scoped>
.soj-crumbs {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
}

.c-link {
  font-size: var(--soj-fs-16);
  color: var(--soj-primary);
  white-space: nowrap;
}

.c-link:hover {
  text-decoration: underline;
}

/* 末段 = 当前层名称：多段时承担原 big-title 视觉（20px navy） */
.c-cur {
  font-size: var(--soj-fs-20);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  word-break: break-all;
}

.sep {
  font-size: var(--soj-fs-14);
  color: var(--el-text-color-secondary);
  letter-spacing: 1px;
  flex-shrink: 0;
}

/* compact 档（做题页统一头）：纯位置导航，全段 16px、末段不高亮放大 */
.soj-crumbs.compact .c-link,
.soj-crumbs.compact .c-cur {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-regular);
}

.soj-crumbs.compact .c-cur {
  font-weight: var(--soj-fw-emphasis);
}
</style>

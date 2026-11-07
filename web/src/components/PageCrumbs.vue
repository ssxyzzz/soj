<script setup lang="ts">
// 统一面包屑（v0.77 引入；v0.80 统一字号蓝色；v0.81.1 用户拍板参考经典样式）：可点
// 前段 = 链接蓝，当前层（末段）= 灰色，分隔符 = 「>」（比正文略高、浅灰）。
export interface CrumbItem {
  label: string
  to?: string
}

defineProps<{ items: CrumbItem[] }>()
</script>

<template>
  <div class="soj-crumbs">
    <template v-for="(it, i) in items" :key="i">
      <span v-if="i" class="sep" aria-hidden="true">&gt;</span>
      <router-link v-if="it.to && i < items.length - 1" :to="it.to" class="c-link">{{ it.label }}</router-link>
      <span v-else class="c-cur">{{ it.label }}</span>
    </template>
  </div>
</template>

<style scoped>
.soj-crumbs {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
}

/* v0.81.1 参考经典面包屑：可点前段链接蓝，当前层（末段）灰色，无下划线（hover 加深） */
.c-link {
  font-size: var(--soj-fs-body);
  color: var(--soj-blue);
  text-decoration: none;
  white-space: nowrap;
}

.c-link:hover {
  color: var(--soj-blue-deep);
}

.c-cur {
  font-size: var(--soj-fs-body);
  color: var(--soj-ink-2);
  white-space: nowrap;
}

/* 分隔符「>」比正文略高（fs-16）、浅灰（参考用户示例图） */
.sep {
  font-size: var(--soj-fs-16);
  color: var(--soj-ink-3);
  flex-shrink: 0;
  transform: translateY(-1px);
}
</style>

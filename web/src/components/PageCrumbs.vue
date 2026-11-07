<script setup lang="ts">
// 统一面包屑（v0.77 引入；v0.80 用户拍板重设计）：全段统一 fs-body 链接蓝（默认可点
// 蓝色）、无下划线（hover 加深代替），段间分隔符弱灰；前段 router-link 可点直达、
// 末段 = 当前层不可点。旧「末段 20px 名称视觉 / compact 档」随统一拍板退役。
export interface CrumbItem {
  label: string
  to?: string
}

defineProps<{ items: CrumbItem[] }>()
</script>

<template>
  <div class="soj-crumbs">
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
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
}

/* v0.80 统一口径：全段 fs-body 链接蓝（--soj-blue，站点可点蓝）、无下划线（链接段与
   当前段同款，不靠字重/字号区分层级——用户拍板「字体全部统一，颜色改为可点击的
   蓝色，不加横线」） */
.c-link,
.c-cur {
  font-size: var(--soj-fs-body);
  color: var(--soj-blue);
  text-decoration: none;
  white-space: nowrap;
}

.c-link:hover {
  color: var(--soj-blue-deep);
}

.sep {
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-secondary);
  letter-spacing: 1px;
  flex-shrink: 0;
}
</style>

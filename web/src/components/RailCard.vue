<script setup lang="ts">
// 副列卡（v3-full .card + card-h 结构统一件）：ov eyebrow（11px teal 大写小标）
// + card-t 标题（15/700 藏蓝）+ 卡体（14px 20px 18px）。
// 全站副列卡（题库/详情/比赛/班级/作业 rail）统一换用，替换 el-card 默认头。
withDefaults(
  defineProps<{
    ov?: string // eyebrow 小标（如「管理」「提交」「说明」）
    title?: string // 卡标题（如「题目管理」）；两者都空则不渲染头
  }>(),
  { ov: undefined, title: undefined },
)
</script>

<template>
  <el-card shadow="never" class="rail-card">
    <template v-if="ov || title || $slots.head" #header>
      <slot name="head">
        <div class="rail-ov">{{ ov }}</div>
        <div class="rail-t">{{ title }}</div>
      </slot>
    </template>
    <slot />
  </el-card>
</template>

<style scoped>
.rail-card :deep(.el-card__header) {
  padding: 16px 20px 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

/* v3 .ov：11px/700 teal 大写 eyebrow */
.rail-ov {
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  letter-spacing: 0.16em;
  color: var(--soj-teal);
  text-transform: uppercase;
}

/* v3 .card-t：15/700 藏蓝 */
.rail-t {
  font-size: 15px;
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
}
</style>

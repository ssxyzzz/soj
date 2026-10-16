<script setup lang="ts">
// 全站两列骨架（v0.4.6）：容器 1280px，主列自适应 + 右列 300px 固定；
// 可选「独立行」插槽位于两列之上、顶栏之下（比赛/考试做题页的赛程与切题行）。
// 窄屏（<992px）右列折到主内容下方，信息不丢。未迁移的旧页面继续用各自的 .page 布局。
</script>

<template>
  <div class="page-shell">
    <div v-if="$slots.standalone" class="standalone">
      <slot name="standalone" />
    </div>
    <div class="cols">
      <div class="main-col">
        <slot />
      </div>
      <aside v-if="$slots.aside" class="side-col">
        <slot name="aside" />
      </aside>
    </div>
  </div>
</template>

<style scoped>
.page-shell {
  max-width: 1280px;
  margin: 0 auto;
}

.standalone {
  margin-bottom: 16px;
}

.cols {
  display: flex;
  gap: 24px;
  align-items: flex-start;
}

.main-col {
  flex: 1 1 auto;
  min-width: 0;
}

.side-col {
  flex: 0 0 300px;
  width: 300px;
  position: sticky;
  top: 76px;
}

@media (max-width: 992px) {
  .cols {
    flex-direction: column;
  }

  .side-col {
    position: static;
    width: auto;
    flex: 1 1 auto;
  }
}
</style>

<script setup lang="ts">
// 全站两列骨架：容器 1440px（v0.19 曾收窄 1280，2026-10-25 回调——题库等表格页主列
// 装不下最小列宽和，与 .page 同步），主列自适应 + 右列 320px 固定（全站统一，无宽副列特例）；
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
  max-width: 1440px;
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
  /* 同 .page：el-row gutter 负边距防外溢（clip 不建滚动容器，弹层均 teleport） */
  overflow-x: clip;
}

.side-col {
  flex: 0 0 320px;
  width: 320px;
}

/* 右列纵向堆叠的按钮组居中；重置 Element Plus 相邻按钮的默认左移
   （.el-button + .el-button 的 margin-left 会让纵向排列的第二个按钮右错位） */
.side-col :deep(.el-button + .el-button) {
  margin-left: 0;
}

@media (max-width: 992px) {
  .cols {
    flex-direction: column;
    /* 纵向折叠后恢复交叉轴拉伸：flex-start 会让主列宽度变成内容自适应，
       被宽表格/不换行标签行撑出视口（横向滚动条落在视口外不可用） */
    align-items: stretch;
  }

  .side-col {
    width: auto;
    flex: 1 1 auto;
  }
}
</style>

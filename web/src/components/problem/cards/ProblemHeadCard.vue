<script setup lang="ts">
// 做题页统一头卡（v0.90 统一批③收编）：面包屑 + 切题序号，作业做题头（题库页 assignment
// 分支）与赛事做题头（比赛/考试）共用——原两份实现 + `.sw-btn` 样式整段复制，此处唯一实现。
// 结构：首行 = 面包屑（左）+ meta 插槽（右，赛事放时间段+状态）；次行 = sub 插槽（赛事放
// OI 赛制提示）+ 切题序号。纯壳组件不拉数据，切题导航由页面处理（两个场景路由形态不同）。
import PageCrumbs from '@/components/PageCrumbs.vue'

defineProps<{
  /** 面包屑段（PageCrumbs items 形状） */
  crumbs: { label: string; to?: string }[]
  /** 切题按钮组：题目 id 列表 */
  problemIds: number[]
  /** 当前题 id（高亮） */
  currentPid: number
}>()

const emit = defineEmits<{ (e: 'select', pid: number): void }>()

function go(pid: number) {
  emit('select', pid)
}
</script>

<template>
  <el-card class="problem-head" :body-style="{ padding: 'var(--soj-sp-14) var(--soj-sp-20)' }">
    <div class="rh-top">
      <PageCrumbs :items="crumbs" />
      <div v-if="$slots.meta" class="rh-meta"><slot name="meta" /></div>
    </div>
    <div v-if="$slots.sub || problemIds.length" class="rh-sub">
      <slot name="sub" />
      <span v-if="problemIds.length" class="sw-btns">
        <span class="sw-lab">切题</span>
        <button
          v-for="(pid, i) in problemIds"
          :key="pid"
          type="button"
          class="sw-btn"
          :class="{ on: pid === currentPid }"
          @click="go(pid)"
        >
          {{ i + 1 }}
        </button>
      </span>
    </div>
  </el-card>
</template>

<style scoped>
.problem-head {
  margin-bottom: var(--soj-sp-16);
}

/* 首行 = 面包屑（左）+ meta 插槽（右）；v0.81 赛事拍板重排口径收编 */
.rh-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-12);
  flex-wrap: wrap;
}

.rh-meta {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  flex-wrap: wrap;
}

/* 次行 = sub 插槽（OI 赛制提示等）+ 切题序号 */
.rh-sub {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  flex-wrap: wrap;
  margin-top: var(--soj-sp-10);
}

/* 切题方块钮（v3 .sw-btn：34x32 圆角 10；原两页各复制一份，此处唯一实现） */
.sw-btns {
  display: inline-flex;
  gap: var(--soj-sp-6);
  margin-left: var(--soj-sp-12);
  align-items: center;
  flex-wrap: wrap;
}

.sw-lab {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  margin-right: var(--soj-sp-2);
}

.sw-btn {
  width: 34px;
  height: 32px;
  border-radius: 10px;
  border: 1px solid var(--soj-line-2);
  background: var(--el-fill-color-blank);
  font: inherit;
  font-weight: var(--soj-fw-bold);
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  cursor: pointer;
  transition: all 0.12s;
}

.sw-btn:hover {
  border-color: var(--soj-blue-line);
  color: var(--soj-blue);
}

.sw-btn.on {
  background: var(--soj-blue);
  border-color: var(--soj-blue);
  color: var(--soj-white);
}
</style>

<script setup lang="ts">
// 昵称 + 等级一体渲染（v0.11）：整个「ssxyz · Lv.10 极境」连名字一起装进一个等级胶囊——
// 排名页与各成绩表的统一入口。胶囊复用 TierBadge（kind=level，v3-full 静态浅色形制），
// 昵称与「·」经默认插槽进入胶囊内部；无等级时退纯昵称（不带胶囊）。
// v0.17：传 uid 时整颗渲染为路由链接（跳 /users/:id 公开主页）——成绩表等消费端
// 行内自带点击的需自行 .stop（见 ScoreSummaryCard）。
import { computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'

import TierBadge from '@/components/TierBadge.vue'
import { useLevelsStore } from '@/stores/levels'

const props = withDefaults(
  defineProps<{
    name: string
    level?: number | null // 学生等级 1–10；空则只渲染昵称
    me?: boolean // 本人行：昵称主色加粗（Lv.10 渐变实底上改继承白字）
    uid?: number | null // 用户 ID：传入则可点进公开主页（0/null = 纯展示）
  }>(),
  { level: null, me: false, uid: null },
)

const levels = useLevelsStore()
onMounted(() => levels.ensure()) // 等级名配置就绪后自动补全

const tier = computed(() => Math.min(10, Math.max(1, Math.round(props.level ?? 1))))
const tierText = computed(() => `Lv.${tier.value} ${levels.nameOf(tier.value)}`)
const fullText = computed(() => (props.level ? `${props.name} · ${tierText.value}` : props.name))
</script>

<template>
  <!-- 单根动态件：有 uid 渲染 RouterLink（可点进主页），否则纯 span（样式/语义不变） -->
  <component
    :is="uid ? RouterLink : 'span'"
    v-bind="uid ? { to: `/users/${uid}` } : {}"
    class="unl-root"
    :title="fullText"
  >
    <TierBadge v-if="level" :tier="tier" kind="level" size="sm" class="unl">
      <span class="unl-name" :class="{ 'unl-me': me, 'unl-me-capsule': me }">{{ name }}</span>
      <span class="unl-sep">·</span>
      <span class="unl-tier-text">{{ tierText }}</span>
    </TierBadge>
    <span v-else class="unl-plain" :class="{ 'unl-me': me }">{{ name }}</span>
  </component>
</template>

<style scoped>
/* 可点根：去链接下划线，悬停轻微降不透明度示意可进主页；限宽防表格溢出 */
.unl-root {
  max-width: 100%;
  display: inline-block;
  vertical-align: middle;
  text-decoration: none;
}

a.unl-root {
  cursor: pointer;
  transition: opacity 0.15s;
}

a.unl-root:hover {
  opacity: 0.82;
}

/* 胶囊分支：限宽防溢出；名字列 overflow hidden 使 flex 项最小宽度归零，窄列可收缩出省略号 */
.unl {
  max-width: 100%;
  vertical-align: middle;
  justify-self: start;
}
.unl-name {
  overflow: hidden;
  text-overflow: ellipsis;
}
.unl-sep {
  margin: 0 4px;
  opacity: 0.6;
}

/* 无等级：纯昵称，保持原截断行为 */
.unl-plain {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
}

.unl-me {
  color: var(--el-color-primary);
  font-weight: var(--soj-fw-emphasis);
}

/* 等级胶囊（v0.21 实色炫彩版）内 me 高亮：主色蓝在实色底上不可读，改白字细下划线 */
.unl-me-capsule {
  color: #fff;
  font-weight: var(--soj-fw-bold);
  text-decoration: underline;
  text-underline-offset: 2px;
  text-decoration-color: rgba(255, 255, 255, 0.7);
}
</style>

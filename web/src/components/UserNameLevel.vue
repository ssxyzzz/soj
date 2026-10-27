<script setup lang="ts">
// 昵称 + 等级拆分渲染（2026-10-27 用户拍板，弃「名字装进等级胶囊」的聚合形态）：
// 昵称平铺在外（纯文本/可点链接），等级单独一颗 TierBadge（kind=level，与顶栏/个人主页
// 等处的独立等级徽章同款形制）；无等级时只渲染昵称。
// 传 uid 时昵称渲染为路由链接（跳 /users/:id 公开主页）——成绩表等消费端
// 行内自带点击的需自行 .stop（见 ScoreSummaryCard）。
import { computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'

import TierBadge from '@/components/TierBadge.vue'
import { useLevelsStore } from '@/stores/levels'

const props = withDefaults(
  defineProps<{
    name: string
    level?: number | null // 学生等级 1–10；空则只渲染昵称
    me?: boolean // 本人行：昵称主色加粗
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
    <span class="unl-name" :class="{ 'unl-me': me }">{{ name }}</span>
    <TierBadge v-if="level" :tier="tier" kind="level" size="sm" class="unl">{{ tierText }}</TierBadge>
  </component>
</template>

<style scoped>
/* 根：内联弹性 + 限宽防表格溢出；可点根去链接下划线，悬停轻微降不透明度示意可进主页 */
.unl-root {
  max-width: 100%;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
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

/* 昵称列：overflow hidden 使 flex 项最小宽度归零，窄列可收缩出省略号 */
.unl-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.unl-me {
  color: var(--el-color-primary);
  font-weight: var(--soj-fw-emphasis);
}

/* 等级徽章独立于昵称：不随昵称收缩 */
.unl {
  flex: none;
  max-width: 100%;
  vertical-align: middle;
  justify-self: start;
}
</style>

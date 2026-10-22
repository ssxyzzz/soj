<script setup lang="ts">
// 昵称 + 等级一体渲染（v0.11）：整个「ssxyz · Lv.10 极境」连名字一起装进一个等级胶囊——
// 排名页与各成绩表的统一入口。胶囊复用 TierBadge（kind=level，特效/呼吸星跟随后台开关），
// 昵称与「·」经默认插槽进入胶囊内部；无等级时退纯昵称（不带胶囊）。
import { computed, onMounted } from 'vue'

import TierBadge from '@/components/TierBadge.vue'
import { useLevelsStore } from '@/stores/levels'

const props = withDefaults(
  defineProps<{
    name: string
    level?: number | null // 学生等级 1–10；空则只渲染昵称
    me?: boolean // 本人行：昵称主色加粗
  }>(),
  { level: null, me: false },
)

const levels = useLevelsStore()
onMounted(() => levels.ensure()) // 等级名配置就绪后自动补全

const tier = computed(() => Math.min(10, Math.max(1, Math.round(props.level ?? 1))))
const tierText = computed(() => `Lv.${tier.value} ${levels.nameOf(tier.value)}`)
const fullText = computed(() => (props.level ? `${props.name} · ${tierText.value}` : props.name))
</script>

<template>
  <TierBadge v-if="level" :tier="tier" kind="level" size="sm" class="unl" :title="fullText">
    <span
      class="unl-name"
      :class="{ 'unl-me': me, 'unl-me-underline': me && tier === 10 && levels.effectsOn(tier) }"
    >{{ name }}</span>
    <span class="unl-sep">·</span>
    <span class="unl-tier-text">{{ tierText }}</span>
  </TierBadge>
  <span v-else class="unl-plain" :class="{ 'unl-me': me }" :title="name">{{ name }}</span>
</template>

<style scoped>
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

/* 极境彩色渐变胶囊上主色蓝对比度不足：me 高亮退回白字，改下划线标记 */
.unl-me-underline {
  color: inherit;
  text-decoration: underline;
}
</style>

<script setup lang="ts">
// 我的提交 mini 行卡（v0.90 统一批③收编自题库做题页副列）：近 3 条，点行打开完整弹窗
// （「查看全部」行随用户拍板去掉——行本身即入口）；数据拉取与竞态守卫留在页面
// （同实例切题跟随重载是页面职责）。赛时遮罩（masked=未揭榜）与我的提交弹窗同口径：
// 状态位只报「已记录」不出分。
import RailCard from '@/components/RailCard.vue'
import SubStatus from '@/components/SubStatus.vue'
import { fmtRel, fmtShort, isQueueStatus } from '@/utils/format'
import type { Submission } from '@/types'

defineProps<{
  rows: Submission[]
  /** OI 赛时未揭榜：状态位显示「已记录」，耗时列退回提交时间 */
  masked?: boolean
}>()
const emit = defineEmits<{ (e: 'open'): void }>()
</script>

<template>
  <RailCard ov="记录" title="我的提交" class="aside-card">
    <template v-if="rows.length">
      <div v-for="s in rows.slice(0, 3)" :key="s.id" class="soj-mini-li" @click="emit('open')">
        <span v-if="masked" class="dim recorded">已记录</span>
        <SubStatus v-else :status="s.status" :score="s.score" />
        <span class="grow mono">#{{ s.id }}</span>
        <!-- 相对时间列（2026-10-25 用户反馈）：N 秒/分钟/小时前；含中文不挂 mono（修复⑫） -->
        <span class="t">{{ fmtRel(s.created_at) }}</span>
        <!-- 未揭榜/排队中回退提交时间；已判定即显示（0 总耗时也出 "0ms"，v0.96.2 同列表口径） -->
        <span class="t mono">{{ !masked && !isQueueStatus(s.status) ? `${s.time_ms ?? 0}ms` : fmtShort(s.created_at).split(' ')[1] }}</span>
      </div>
    </template>
    <p v-else class="hint">还没有提交过本题。</p>
  </RailCard>
</template>

<style scoped>
.soj-mini-li {
  cursor: pointer;
}

/* 赛时「已记录」占位：与 SubStatus 胶囊同高的弱化文本 */
.recorded {
  flex: none;
}
</style>

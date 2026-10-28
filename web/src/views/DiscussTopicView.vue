<script setup lang="ts">
// 话题详情页（v0.24）：三区话题的统一落地页（通知深链直达）。
// 题内话题给「返回本题」，比赛话题给「返回比赛」，全站话题给「返回讨论区」。
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import DiscussTopicDetail from '@/components/discuss/DiscussTopicDetail.vue'
import { discussApi } from '@/api'
import type { DiscussTopic } from '@/types'

const route = useRoute()
const router = useRouter()
const topic = ref<DiscussTopic | null>(null)

const tid = computed(() => Number(route.params.tid))

const backRoute = computed(() => {
  const t = topic.value
  if (!t) return '/discuss'
  if (t.scope === 'problem') return `/problems/${t.scope_id}`
  if (t.scope === 'contest') return `/contests/${t.scope_id}`
  return '/discuss'
})
const backLabel = computed(() => {
  const t = topic.value
  if (t?.scope === 'problem') return '返回本题'
  if (t?.scope === 'contest') return '返回比赛'
  return '返回讨论区'
})

watch(
  tid,
  async (v) => {
    if (!Number.isInteger(v) || v <= 0) {
      router.replace('/discuss')
      return
    }
    try {
      const r = await discussApi.detail(v)
      topic.value = r.topic
    } catch {
      topic.value = null
    }
  },
  { immediate: true },
)
</script>

<template>
  <PageShell>
    <div class="page-head">
      <div class="kicker">DISCUSS</div>
      <h1 class="page-title">
        话题
        <small v-if="topic">
          {{
            topic.scope === 'problem'
              ? `题目讨论 · 来自 P${topic.scope_id}`
              : topic.scope === 'contest'
                ? `比赛讨论 · 来自 C${topic.scope_id}`
                : '学习分享区'
          }}
        </small>
        <small v-else>加载中…</small>
      </h1>
    </div>
    <DiscussTopicDetail :key="tid" :tid="tid" />
    <div class="dtv-back">
      <el-button text @click="router.push(backRoute)">← {{ backLabel }}</el-button>
    </div>
  </PageShell>
</template>

<style scoped>
.dtv-back {
  margin-top: 16px;
}
</style>

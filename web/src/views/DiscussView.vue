<script setup lang="ts">
// 导航栏讨论区（v0.24）：全站学习分享（心得/路线/讲解/反馈）。
// 版式对齐题单/比赛列表页：page-head（kicker + 大标题）+ soj-seg 分段筛选 + 副列动作卡。
// 纯学习导向不做水区：发帖必选类型 + 新号冷静期 + 限速（服务端闸门）。
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { useSiteStore } from '@/stores/site'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import DiscussTopicList from '@/components/discuss/DiscussTopicList.vue'
import DiscussPostDialog from '@/components/discuss/DiscussPostDialog.vue'
import { discussApi } from '@/api'
import type { DiscussTopic } from '@/types'

const router = useRouter()
const category = ref('')
const q = ref('')
const site = useSiteStore()
const siteReady = ref(false)
// 总开关关闭：导航项已隐藏，直达 URL 也只显示空态（接口本身另有闸门）
const closed = computed(() => siteReady.value && !site.discussGlobalEnabled)
const refreshToken = ref(0)
const postVisible = ref(false)
const disabled = ref(false)
const disabledReason = ref('')

const CATS: [string, string][] = [
  ['', '全部'],
  ['experience', '学习心得'],
  ['route', '刷题路线'],
  ['tutorial', '知识讲解'],
  ['feedback', '站点反馈'],
]

function fetcher(page: number) {
  return discussApi.listGlobal({ category: category.value || undefined, q: q.value || undefined, page }).then((r) => {
    disabled.value = r.enabled === false
    disabledReason.value = r.disabled_reason || ''
    return r
  })
}

function openTopic(t: DiscussTopic) {
  router.push(`/discuss/${t.id}`)
}

function onPosted(id: number) {
  refreshToken.value++
  router.push(`/discuss/${id}`)
}

watch([category], () => {
  refreshToken.value++
})
onMounted(async () => {
  await site.ensure()
  siteReady.value = true
  refreshToken.value++
})
</script>

<template>
  <el-empty v-if="closed" description="讨论区未开放" />
  <PageShell v-else>
    <div class="page-head">
      <div class="kicker">DISCUSS</div>
      <h1 class="page-title">
        讨论
        <small>学习心得 · 刷题路线 · 知识讲解 · 站点反馈</small>
      </h1>
    </div>

    <el-alert v-if="disabled" :title="disabledReason || '讨论区已关闭'" type="warning" :closable="false" />

    <template v-else>
      <div class="seg-row">
        <div class="soj-seg page-seg">
          <button
            v-for="[value, label] in CATS"
            :key="value || 'all'"
            :class="{ on: category === value }"
            @click="category = value"
          >
            {{ label }}
          </button>
        </div>
        <el-input
          v-model="q"
          placeholder="搜索标题…"
          clearable
          style="width: 240px"
          @keyup.enter="refreshToken++"
          @clear="refreshToken++"
        >
          <template #append>
            <el-button @click="refreshToken++">搜索</el-button>
          </template>
        </el-input>
      </div>

      <DiscussTopicList scope="global" :fetcher="fetcher" :refresh-token="refreshToken" @open="openTopic" />
    </template>

    <template #aside>
      <RailCard ov="分享" title="发起讨论" class="aside-card">
        <el-button type="primary" style="width: 100%" :disabled="disabled" @click="postVisible = true">
          ✏️ 发帖
        </el-button>
        <p class="hint">
          分享学习经验、刷题路线或知识讲解；发帖必选类型，新号有冷静期，请勿灌水。
        </p>
      </RailCard>
      <RailCard v-if="disabled" ov="提示" title="讨论区已锁定" class="aside-card">
        <p class="hint">{{ disabledReason }}</p>
      </RailCard>
    </template>
  </PageShell>

  <DiscussPostDialog v-model="postVisible" scope="global" @done="onPosted" />
</template>

<style scoped>
.seg-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
</style>

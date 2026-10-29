<script setup lang="ts">
// 讨论弹窗容器（v0.24）：题内/比赛讨论共用的「列表 ↔ 话题详情」导航壳。
// 发帖按钮按 scope 走对应表单；列表与详情数据各自拉取，互不依赖路由。
// 内部三件按需加载（加载优化批）：题面/比赛详情页不点开讨论弹窗就不下载这串
// chunk（含 MdEditor 样式链），题面首开闭包显著瘦身；首次打开多一次懒加载往返。
import { computed, defineAsyncComponent, ref } from 'vue'

import { discussApi } from '@/api'
import type { DiscussScope, DiscussTopic } from '@/types'
const DiscussTopicList = defineAsyncComponent(() => import('./DiscussTopicList.vue'))
const DiscussTopicDetail = defineAsyncComponent(() => import('./DiscussTopicDetail.vue'))
const DiscussPostDialog = defineAsyncComponent(() => import('./DiscussPostDialog.vue'))

const props = defineProps<{
  scope: Exclude<DiscussScope, 'global'>
  scopeId: number
  /** 弹窗标题（如「P12 两数之和 · 讨论」） */
  title?: string
  /** 比赛赛时/组织者（发帖提示用；列表接口也会下发权威值） */
  contestRunning?: boolean
}>()

const visible = defineModel<boolean>({ default: false })

type Mode = { name: 'list' } | { name: 'topic'; tid: number }
const mode = ref<Mode>({ name: 'list' })
const postVisible = ref(false)
const refreshToken = ref(0)
const contestEnded = ref(false)
const isOrganizer = ref(false)

const heading = computed(() => {
  const base = props.title || (props.scope === 'problem' ? '题目讨论' : '比赛讨论')
  if (mode.value.name === 'topic') return `${base} · 话题`
  return base
})

function fetcher(page: number) {
  if (props.scope === 'problem') return discussApi.listProblem(props.scopeId, page)
  return discussApi.listContest(props.scopeId, page).then((r) => {
    contestEnded.value = !!r.ended
    isOrganizer.value = !!r.is_organizer
    return r
  })
}

function openTopic(t: DiscussTopic) {
  mode.value = { name: 'topic', tid: t.id }
}

function backToList() {
  mode.value = { name: 'list' }
  refreshToken.value++ // 回列表顺带刷新（回复/采纳后的计数同步）
}

function onPosted(id: number) {
  refreshToken.value++
  mode.value = { name: 'topic', tid: id }
}
</script>

<template>
  <el-dialog v-model="visible" :title="heading" width="780px" top="4vh" destroy-on-close>
    <DiscussTopicList
      v-if="mode.name === 'list'"
      :scope="scope"
      :fetcher="fetcher"
      :refresh-token="refreshToken"
      @open="openTopic"
    />
    <DiscussTopicDetail
      v-else
      :key="mode.tid"
      :tid="mode.tid"
      embedded
      @changed="refreshToken++"
    />
    <div v-if="mode.name === 'topic'" class="dlg-back">
      <el-button text @click="backToList">← 返回列表</el-button>
    </div>
    <template #footer>
      <el-button v-if="mode.name === 'topic'" @click="backToList">返回列表</el-button>
      <el-button v-else type="primary" @click="postVisible = true">发起讨论</el-button>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>

  <DiscussPostDialog
    v-model="postVisible"
    :scope="scope"
    :scope-id="scopeId"
    :contest-running="contestRunning"
    :contest-organizer="isOrganizer"
    @done="onPosted"
  />
</template>

<style scoped>
.dlg-back {
  margin-top: 8px;
}
</style>

<script setup lang="ts">
// 系统设置拆分（2026-10-28 用户拍板：单页过长，参考等级设置双页签/题目管理子页签拆分）：
// 本壳只管四个子页的页签切换与 ?tab= 深链，各子面板独立 getSettings 加载、
// updateSettings 部分字段保存（后端 patch 语义天然支持），互不阻塞。
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import SettingsAccountPanel from '@/components/admin/settings/SettingsAccountPanel.vue'
import SettingsBasicPanel from '@/components/admin/settings/SettingsBasicPanel.vue'
import SettingsDiscussPanel from '@/components/admin/settings/SettingsDiscussPanel.vue'
import SettingsMailPanel from '@/components/admin/settings/SettingsMailPanel.vue'

const route = useRoute()
const router = useRouter()

const tabs = ['basic', 'mail', 'discuss', 'account'] as const
type Tab = (typeof tabs)[number]

const TAB_LABELS: Record<Tab, string> = {
  basic: '基础',
  mail: '注册与邮箱',
  discuss: '讨论区',
  account: '账号与通知',
}

const raw = (Array.isArray(route.query.tab) ? route.query.tab[0] : route.query.tab) as string | undefined
const tab = ref<Tab>(tabs.includes(raw as Tab) ? (raw as Tab) : 'basic')

function switchTab(t: string | number) {
  tab.value = t as Tab
  // 深链同步（不触发导航守卫；保持 section=settings 不变）
  router.replace({ query: { ...route.query, tab: tab.value } })
}

onMounted(() => {
  router.replace({ query: { ...route.query, tab: tab.value } })
})
</script>

<template>
  <el-tabs :model-value="tab" class="settings-tabs" @tab-change="switchTab">
    <el-tab-pane v-for="t in tabs" :key="t" :name="t" :label="TAB_LABELS[t]" />
  </el-tabs>

  <SettingsBasicPanel v-if="tab === 'basic'" />
  <SettingsMailPanel v-else-if="tab === 'mail'" />
  <SettingsDiscussPanel v-else-if="tab === 'discuss'" />
  <SettingsAccountPanel v-else-if="tab === 'account'" />
</template>

<style scoped>
/* 与等级设置双页签同款形制（LevelPanel .levels-tabs） */
.settings-tabs {
  margin-bottom: 14px;
}
</style>

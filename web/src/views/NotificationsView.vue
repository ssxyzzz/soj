<script setup lang="ts">
// 消息中心（v0.24）：全部/未读 + 类型筛选 + 单条/全部已读；点击深链跳转并标已读。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import { notifyApi } from '@/api'
import { notifyTypeMeta } from '@/components/discuss/discussMeta'
import { useUserStore } from '@/stores/user'
import { fmtRel } from '@/utils/format'
import type { NotificationItem } from '@/types'

const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)
const filter = ref<'all' | 'unread'>('all')
const type = ref('')
const page = ref(1)
const total = ref(0)
const unread = ref(0)
const list = ref<NotificationItem[]>([])

const TYPE_OPTIONS: [string, string][] = [
  ['reply', '讨论回复'],
  ['mention', '提到我'],
  ['like', '收到点赞'],
  ['accept', '回答被采纳'],
  ['assignment', '作业'],
  ['exam', '考试'],
  ['contest', '比赛'],
  ['broadcast', '公告'],
  ['warn', '管理通知'],
]

async function load(p = 1) {
  loading.value = true
  try {
    const r = await notifyApi.list({
      filter: filter.value === 'unread' ? 'unread' : undefined,
      type: type.value || undefined,
      page: p,
    })
    list.value = r.list
    total.value = r.total
    unread.value = r.unread
    page.value = p
  } finally {
    loading.value = false
  }
}

async function openItem(n: NotificationItem) {
  if (!n.read) {
    // 本地先行置灰，失败不打回
    n.read = true
    unread.value = Math.max(0, unread.value - 1)
    try {
      await notifyApi.read([n.id])
    } catch {
      /* 静默 */
    }
  }
  if (n.link) router.push(n.link)
}

async function markAll() {
  const r = await notifyApi.read()
  unread.value = r.unread
  list.value.forEach((n) => (n.read = true))
  ElMessage.success('已全部标为已读')
}

onMounted(() => {
  if (!userStore.token) {
    router.replace('/login?redirect=/notifications')
    return
  }
  load(1)
})
</script>

<template>
  <PageShell>
    <div class="page-head">
      <div class="kicker">NOTIFICATIONS</div>
      <h1 class="page-title">
        消息中心
        <small>讨论互动 · 作业考试比赛提醒 · 全站公告</small>
      </h1>
    </div>

    <div class="nc-toolbar">
      <div class="nc-toolbar-left">
        <div class="soj-seg page-seg">
          <button :class="{ on: filter === 'all' }" @click="filter = 'all'; load(1)">全部</button>
          <button :class="{ on: filter === 'unread' }" @click="filter = 'unread'; load(1)">
            未读{{ unread ? `（${unread}）` : '' }}
          </button>
        </div>
        <el-select v-model="type" placeholder="全部类型" clearable style="width: 150px" @change="load(1)">
          <el-option v-for="[t, label] in TYPE_OPTIONS" :key="t" :value="t" :label="label" />
        </el-select>
      </div>
      <el-button v-if="unread > 0" text type="primary" @click="markAll">全部已读（{{ unread }}）</el-button>
    </div>

    <div v-loading="loading" class="nc-list">
      <el-empty v-if="!loading && !list.length" description="暂无消息" :image-size="80" />
      <div
        v-for="n in list"
        :key="n.id"
        class="nc-item"
        :class="{ unread: !n.read }"
        @click="openItem(n)"
      >
        <span class="nc-icon">{{ notifyTypeMeta(n.type).icon }}</span>
        <div class="nc-body">
          <div class="nc-row1">
            <span class="nc-title-text">{{ n.title }}</span>
            <el-tag v-if="n.agg_count > 1" size="small" round effect="plain">{{ n.agg_count }} 条</el-tag>
            <span v-if="!n.read" class="nc-dot" title="未读"></span>
          </div>
          <div class="nc-text">{{ n.body }}</div>
        </div>
        <span class="nc-time">{{ fmtRel(n.created_at) }}</span>
      </div>
      <div v-if="total > list.length" class="nc-more">
        <el-pagination
          layout="prev, pager, next"
          :total="total"
          :page-size="20"
          :current-page="page"
          @current-change="(p: number) => load(p)"
        />
      </div>
    </div>
  </PageShell>
</template>

<style scoped>
.nc-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.nc-toolbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.nc-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--soj-line);
  border-radius: 10px;
  margin-bottom: 8px;
  cursor: pointer;
  background: var(--el-bg-color, #fff);
  transition: border-color 0.15s;
}
.nc-item:hover {
  border-color: var(--soj-blue);
}
.nc-item.unread {
  border-left: 3px solid var(--soj-blue);
}
.nc-icon {
  font-size: 18px;
  line-height: 1.4;
}
.nc-body {
  flex: 1;
  min-width: 0;
}
.nc-row1 {
  display: flex;
  align-items: center;
  gap: 8px;
}
.nc-title-text {
  font-weight: var(--soj-fw-emphasis, 600);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nc-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--soj-blue);
  flex-shrink: 0;
}
.nc-text {
  font-size: 13px;
  color: var(--soj-ink-3);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nc-time {
  font-size: 12px;
  color: var(--soj-ink-3);
  white-space: nowrap;
}
.nc-more {
  display: flex;
  justify-content: center;
  margin-top: 10px;
}
</style>

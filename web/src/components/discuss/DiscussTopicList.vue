<script setup lang="ts">
// 讨论区话题列表（v0.24）：题内弹窗 / 比赛弹窗 / 导航栏讨论页共用的列表渲染。
// 数据获取由父级传入 fetcher（三区的列表接口各不相同，渲染同构），点击话题上抛。
import { onMounted, ref, watch } from 'vue'

import { fmtRel } from '@/utils/format'
import type { DiscussListResult, DiscussScope, DiscussTopic } from '@/types'
import { discussKind, discussKindLabel } from './discussMeta'

const props = defineProps<{
  scope: DiscussScope
  fetcher: (page: number) => Promise<DiscussListResult>
  /** 外部触发的刷新令牌（父级发帖成功后 +1） */
  refreshToken?: number
}>()

const emit = defineEmits<{ (e: 'open', topic: DiscussTopic): void }>()

const loading = ref(false)
const page = ref(1)
const total = ref(0)
const list = ref<DiscussTopic[]>([])
const banner = ref('')

async function load(p = 1) {
  loading.value = true
  try {
    const r = await props.fetcher(p)
    list.value = r.list
    total.value = r.total
    page.value = p
    banner.value = r.disabled_reason || ''
    // 比赛讨论区：赛时提示 + 组织者视角
    if (props.scope === 'contest') {
      if (r.running) banner.value = r.is_organizer ? '比赛进行中：学生提问仅你可见，可回复或升格公开' : '比赛进行中：你的提问仅组织者可见'
      else if (!r.ended) banner.value = r.is_organizer ? '比赛尚未开始：可发布赛前公告' : '比赛尚未开始'
    }
  } catch {
    list.value = []
  } finally {
    loading.value = false
  }
}

watch(
  () => props.refreshToken,
  () => load(1),
)
onMounted(() => load(1))
</script>

<template>
  <div v-loading="loading" class="dt-list">
    <el-alert
      v-if="banner"
      :title="banner"
      type="info"
      :closable="false"
      class="dt-banner"
    />
    <el-empty
      v-if="!loading && !list.length"
      description="还没有讨论，来发第一帖"
      :image-size="72"
    />
    <div
      v-for="t in list"
      :key="t.id"
      class="dt-row"
      @click="emit('open', t)"
    >
      <div class="dt-tags">
        <el-tag v-if="t.pinned" type="danger" size="small" effect="dark" round>置顶</el-tag>
        <el-tag v-if="t.featured" type="warning" size="small" effect="plain" round>精华</el-tag>
        <el-tag v-if="t.private" type="info" size="small" effect="plain" round>仅组织者可见</el-tag>
        <el-tag :type="discussKind(scope, t.kind).tag" size="small" effect="plain" round>
          {{ discussKindLabel(scope, t.kind) }}
        </el-tag>
        <el-tag v-if="t.official" type="success" size="small" effect="plain" round>已官方解答</el-tag>
        <el-tag v-if="t.locked" type="info" size="small" effect="plain" round>已锁定</el-tag>
      </div>
      <div class="dt-title">
        <span class="t">{{ t.title }}</span>
        <span v-if="t.accepted_reply_id" class="solved" title="楼主已采纳答案">✓已解决</span>
      </div>
      <div class="dt-meta">
        <span class="author">{{ t.nickname || `用户${t.uid}` }}</span>
        <span>最后活跃 {{ fmtRel(t.last_active_at) }}</span>
        <span class="grow"></span>
        <span class="stat">💬 {{ t.reply_count }}</span>
        <span class="stat">👍 {{ t.like_count }}</span>
      </div>
    </div>
    <div v-if="total > list.length" class="dt-more">
      <el-pagination
        layout="prev, pager, next"
        :total="total"
        :page-size="20"
        :current-page="page"
        @current-change="(p: number) => load(p)"
      />
    </div>
  </div>
</template>

<style scoped>
.dt-list {
  min-height: 200px;
}
.dt-banner {
  margin-bottom: 10px;
}
.dt-row {
  padding: 11px 12px;
  border: 1px solid var(--soj-line);
  border-radius: 10px;
  margin-bottom: 8px;
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s;
  background: var(--el-bg-color, #fff);
}
.dt-row:hover {
  border-color: var(--soj-blue);
  box-shadow: 0 2px 10px rgb(31 73 186 / 8%);
}
.dt-tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 6px;
}
.dt-title {
  font-size: 15px;
  font-weight: var(--soj-fw-emphasis, 600);
  color: var(--soj-ink);
  display: flex;
  align-items: center;
  gap: 8px;
}
.dt-title .t {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dt-title .solved {
  font-size: 12px;
  font-weight: 600;
  color: var(--soj-ok, #16a34a);
  white-space: nowrap;
}
.dt-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 6px;
  font-size: 12px;
  color: var(--soj-ink-3);
}
.dt-meta .grow {
  flex: 1;
}
.dt-meta .author {
  font-weight: 500;
}
.dt-more {
  display: flex;
  justify-content: center;
  margin-top: 10px;
}
</style>

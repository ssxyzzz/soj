<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { contestApi } from '@/api'
import { useUserStore } from '@/stores/user'
import type { ContestDetail, LeaderRow } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const contest = ref<ContestDetail | null>(null)
const loading = ref(true)
const tab = ref('problems')
const leaders = ref<LeaderRow[]>([])
const leadersLoading = ref(false)
const leadersLoaded = false
let timer: ReturnType<typeof setInterval> | undefined

const now = ref(Date.now())
const countdown = computed(() => {
  if (!contest.value) return ''
  const start = new Date(contest.value.start).getTime()
  const end = new Date(contest.value.end).getTime()
  if (contest.value.status === 'scheduled') return fmtDur(start - now.value)
  if (contest.value.status === 'running') return fmtDur(end - now.value)
  return '已结束'
})

function fmtDur(ms: number): string {
  if (ms <= 0) return '0'
  const s = Math.floor(ms / 1000)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  return (d > 0 ? `${d}天` : '') + `${h}小时${m}分${sec}秒`
}

const statusLabel = computed(() => {
  const map: Record<string, string> = { running: '进行中', scheduled: '未开始', ended: '已结束' }
  return contest.value ? map[contest.value.status] : ''
})

async function load() {
  try {
    contest.value = await contestApi.detail(Number(route.params.id))
    if (contest.value.status === 'ended' || contest.value.is_organizer) {
      loadLeaders()
    }
  } catch {
    router.replace('/contests')
  } finally {
    loading.value = false
  }
}

async function loadLeaders() {
  leadersLoading.value = true
  try {
    leaders.value = await contestApi.leaderboard(Number(route.params.id))
  } catch {
    leaders.value = []
  } finally {
    leadersLoading.value = false
  }
}

async function remove() {
  if (!contest.value) return
  await contestApi.remove(contest.value.id)
  ElMessage.success('已删除')
  router.push('/contests')
}

onMounted(() => {
  load()
  timer = setInterval(() => {
    now.value = Date.now()
  }, 1000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div v-loading="loading" class="page">
    <template v-if="contest">
      <el-card>
        <template #header>
          <div class="head">
            <div>
              <span class="title">{{ contest.title }}</span>
              <el-tag :type="contest.status === 'running' ? 'success' : contest.status === 'scheduled' ? 'warning' : 'info'" class="ml8">
                {{ statusLabel }}
              </el-tag>
              <el-tag v-if="contest.class_id === 0" size="small" class="ml8">主题库 · 全员</el-tag>
            </div>
            <el-button v-if="contest.deletable" size="small" type="danger" plain @click="remove">
              删除比赛
            </el-button>
          </div>
        </template>
        <p v-if="contest.intro" class="intro">{{ contest.intro }}</p>
        <div class="meta">
          <span>
            {{ contest.status === 'scheduled' ? '开始倒计时：' : contest.status === 'running' ? '剩余时间：' : '' }}
            <b>{{ countdown }}</b>
          </span>
          <span class="dim">
            {{ new Date(contest.start).toLocaleString('zh-CN', { hour12: false }) }} ~
            {{ new Date(contest.end).toLocaleString('zh-CN', { hour12: false }) }}
          </span>
          <span v-if="contest.hidden_until" class="dim">
            （赛后隐藏至 {{ new Date(contest.hidden_until).toLocaleString('zh-CN', { hour12: false }) }}）
          </span>
        </div>
        <el-alert
          v-if="contest.class_id !== 0 && !contest.participant && !userStore.token"
          title="登录并加入对应班级后才能参加本场比赛"
          type="info"
          :closable="false"
          class="ml8 mt8"
        />
      </el-card>

      <el-card class="block">
        <el-tabs v-model="tab">
          <el-tab-pane label="题目" name="problems">
            <el-table v-if="contest.problems" :data="contest.problems">
              <el-table-column prop="id" label="ID" width="100" />
              <el-table-column label="题目">
                <template #default="{ row }">
                  <el-link
                    type="primary"
                    @click="router.push(`/contests/${contest!.id}/problems/${row.id}`)"
                  >
                    {{ row.title }}
                  </el-link>
                </template>
              </el-table-column>
              <el-table-column prop="score" label="满分" width="100" />
            </el-table>
            <el-alert
              v-else
              :title="contest.status === 'scheduled' ? '比赛开始前题目隐藏，开始后可见' : '仅本班成员可见题目'"
              type="info"
              :closable="false"
            />
          </el-tab-pane>
          <el-tab-pane label="榜单" name="board">
            <el-button v-if="contest.is_organizer || contest.status === 'ended'" size="small" @click="loadLeaders">
              刷新榜单
            </el-button>
            <el-alert
              v-if="contest.status === 'running' && !contest.is_organizer"
              title="OI 赛制：比赛进行中不显示榜单与判题结果"
              type="info"
              :closable="false"
            />
            <el-table v-if="leaders.length" :data="leaders" class="mt8">
              <el-table-column prop="rank" label="排名" width="80" />
              <el-table-column prop="username" label="用户名" min-width="140" />
              <el-table-column prop="nickname" label="昵称" min-width="120" />
              <el-table-column label="各题得分">
                <template #default="{ row }">
                  <span v-for="cell in row.problems" :key="cell.pid" class="cell">
                    P{{ cell.pid }}: {{ cell.score }}（{{ cell.tries }} 次）
                  </span>
                </template>
              </el-table-column>
              <el-table-column prop="total" label="总分" width="90" />
              <el-table-column label="最后得分提交" width="200">
                <template #default="{ row }">
                  {{ row.last_scoring_at ? new Date(row.last_scoring_at).toLocaleString('zh-CN', { hour12: false }) : '-' }}
                </template>
              </el-table-column>
            </el-table>
            <el-empty v-else-if="!leadersLoading" description="暂无榜单数据" :image-size="60" />
          </el-tab-pane>
        </el-tabs>
      </el-card>
    </template>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.title {
  font-weight: 600;
  font-size: 17px;
}

.ml8 {
  margin-left: 8px;
}

.mt8 {
  margin-top: 8px;
}

.intro {
  margin: 0 0 8px;
  color: #606266;
}

.meta {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  align-items: center;
}

.dim {
  color: #909399;
  font-size: 13px;
}

.block {
  margin-top: 16px;
}

.cell {
  margin-right: 12px;
  font-size: 13px;
}
</style>

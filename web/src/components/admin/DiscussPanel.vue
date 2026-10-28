<script setup lang="ts">
// 讨论管理面板（v0.24）：举报队列（结案：驳回/删除/警告/禁言）+ 禁言列表（解除）。
// 权限：discussion.manage（路由面由 AdminView 的 tab 显隐控制，接口侧 RequirePerm 兜底）。
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { discussAdminApi } from '@/api'
import { fmtRel } from '@/utils/format'
import type { DiscussMuteItem, DiscussReportItem } from '@/types'

const loading = ref(false)
const status = ref<'open' | 'resolved'>('open')
const page = ref(1)
const total = ref(0)
const list = ref<DiscussReportItem[]>([])
const selected = ref<number[]>([])

const mutes = ref<DiscussMuteItem[]>([])
const mutesLoading = ref(false)

const muteOpen = ref(false)
const muteForm = ref({ uid: undefined as number | undefined, days: 7, reason: '' })

async function load() {
  loading.value = true
  try {
    const r = await discussAdminApi.reports({ status: status.value, page: page.value })
    list.value = r.list
    total.value = r.total
    selected.value = []
  } finally {
    loading.value = false
  }
}

async function loadMutes() {
  mutesLoading.value = true
  try {
    const r = await discussAdminApi.mutes()
    mutes.value = r.list
  } finally {
    mutesLoading.value = false
  }
}

async function resolve(action: 'dismiss' | 'delete' | 'warn' | 'mute') {
  if (!selected.value.length) {
    ElMessage.warning('请先勾选举报记录')
    return
  }
  let days = 7
  let message = ''
  if (action === 'mute') {
    try {
      const { value } = await ElMessageBox.prompt('禁言天数（0 = 永久）', '禁言处置', {
        inputValue: '7',
        confirmButtonText: '确定',
        cancelButtonText: '取消',
      })
      days = Number(value) || 0
    } catch {
      return
    }
  }
  if (action === 'warn' || action === 'delete') {
    try {
      const { value } = await ElMessageBox.prompt('附言（将通知当事人，可留空）', action === 'warn' ? '警告' : '删除内容', {
        inputPlaceholder: '如：请勿在讨论区发布无关内容',
        confirmButtonText: '确定',
        cancelButtonText: '取消',
      })
      message = value || ''
    } catch {
      return
    }
  }
  if (action === 'dismiss') {
    try {
      await ElMessageBox.confirm('按举报不成立结案并解除折叠？', '驳回', {
        type: 'warning',
        confirmButtonText: '驳回',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  await discussAdminApi.resolve({ ids: selected.value, action, days, message })
  ElMessage.success('已结案')
  await load()
}

async function addMute() {
  if (!muteForm.value.uid) {
    ElMessage.warning('请填写用户 ID')
    return
  }
  await discussAdminApi.addMute({
    uid: muteForm.value.uid,
    days: muteForm.value.days,
    reason: muteForm.value.reason.trim(),
  })
  ElMessage.success('已禁言')
  muteOpen.value = false
  muteForm.value = { uid: undefined, days: 7, reason: '' }
  await loadMutes()
}

async function lift(id: number) {
  await discussAdminApi.liftMute(id)
  ElMessage.success('已解除')
  await loadMutes()
}

onMounted(() => {
  load()
  loadMutes()
})
</script>

<template>
  <el-card>
    <template #header>
      <div class="head">
        <span>举报队列</span>
        <div class="head-actions">
          <el-radio-group v-model="status" size="small" @change="load()">
            <el-radio-button value="open">未处理</el-radio-button>
            <el-radio-button value="resolved">已结案</el-radio-button>
          </el-radio-group>
          <el-button size="small" :disabled="!selected.length" @click="resolve('dismiss')">驳回</el-button>
          <el-button size="small" type="warning" :disabled="!selected.length" @click="resolve('warn')">警告</el-button>
          <el-button size="small" type="danger" :disabled="!selected.length" @click="resolve('delete')">删除内容</el-button>
          <el-button size="small" type="danger" plain :disabled="!selected.length" @click="resolve('mute')">禁言</el-button>
        </div>
      </div>
    </template>
    <el-table
      v-loading="loading"
      :data="list"
      stripe
      @selection-change="(rows: DiscussReportItem[]) => (selected = rows.map((r) => r.id))"
    >
      <el-table-column type="selection" width="42" />
      <el-table-column label="被举报内容" min-width="220">
        <template #default="{ row }">
          <el-link v-if="row.tid" type="primary" :href="`/discuss/${row.tid}`" target="_blank">
            {{ row.snippet || `#${row.target_id}` }}
          </el-link>
          <span v-else>{{ row.snippet || `#${row.target_id}（已删除）` }}</span>
          <el-tag size="small" style="margin-left: 6px">{{ row.target_type === 'topic' ? '主楼' : '回复' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="理由" min-width="160" show-overflow-tooltip prop="reason" />
      <el-table-column label="举报人" width="140">
        <template #default="{ row }">
          {{ row.reporter?.nickname || `用户${row.reporter?.uid ?? '?'}` }}
        </template>
      </el-table-column>
      <el-table-column label="时间" width="110">
        <template #default="{ row }">{{ fmtRel(row.at) }}</template>
      </el-table-column>
      <el-table-column v-if="status === 'resolved'" label="处置" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.action" size="small" type="info">
            {{ { dismiss: '驳回', delete: '删除', warn: '警告', mute: '禁言' }[row.action as string] || row.action }}
          </el-tag>
        </template>
      </el-table-column>
      <el-empty v-if="!loading && !list.length" description="没有举报记录" />
    </el-table>
    <div v-if="total > list.length" style="display: flex; justify-content: center; margin-top: 10px">
      <el-pagination layout="prev, pager, next" :total="total" :page-size="20" :current-page="page" @current-change="(p: number) => { page = p; load() }" />
    </div>
  </el-card>

  <el-card style="margin-top: 16px">
    <template #header>
      <div class="head">
        <span>禁言管理</span>
        <el-button size="small" type="danger" plain @click="muteOpen = true">新增禁言</el-button>
      </div>
    </template>
    <el-table v-loading="mutesLoading" :data="mutes" stripe>
      <el-table-column label="用户" width="180">
        <template #default="{ row }">{{ row.nickname || row.username || `用户${row.uid}` }}（uid {{ row.uid }}）</template>
      </el-table-column>
      <el-table-column label="截止" min-width="160">
        <template #default="{ row }">
          <el-tag v-if="row.permanent" type="danger" size="small">永久</el-tag>
          <span v-else>{{ row.until.slice(0, 16).replace('T', ' ') }}</span>
        </template>
      </el-table-column>
      <el-table-column label="理由" min-width="180" prop="reason" show-overflow-tooltip />
      <el-table-column label="操作人" width="120">
        <template #default="{ row }">{{ row.by || '—' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="90">
        <template #default="{ row }">
          <el-button text size="small" type="primary" @click="lift(row.id)">解除</el-button>
        </template>
      </el-table-column>
      <el-empty v-if="!mutesLoading && !mutes.length" description="当前无生效中的禁言" />
    </el-table>
  </el-card>

  <el-dialog v-model="muteOpen" title="新增禁言" width="420px">
    <el-form label-width="90px">
      <el-form-item label="用户 ID">
        <el-input-number v-model="muteForm.uid" :min="1" style="width: 100%" />
      </el-form-item>
      <el-form-item label="天数">
        <el-input-number v-model="muteForm.days" :min="0" :max="3650" style="width: 100%" />
        <span class="dim" style="margin-left: 8px">0 = 永久</span>
      </el-form-item>
      <el-form-item label="理由">
        <el-input v-model="muteForm.reason" type="textarea" :rows="2" placeholder="将通知当事人" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="muteOpen = false">取消</el-button>
      <el-button type="primary" @click="addMute">确定</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
}
.head-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
</style>

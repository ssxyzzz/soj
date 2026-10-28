<script setup lang="ts">
// 安全日志面板（v0.14，root）：登录成功/失败、锁定触发、改密、管理员重置、限流事件。
// 数据 TTL 90 天自动滚动清理（后端 Mongo TTL 索引），此处只读。
import { onMounted, reactive, ref } from 'vue'

import { adminApi } from '@/api'
import type { SecurityLogItem } from '@/types'

const typeMeta: Record<string, { label: string; kind: 'success' | 'danger' | 'warning' | 'primary' | 'info' }> = {
  login_success: { label: '登录成功', kind: 'success' },
  login_failure: { label: '登录失败', kind: 'danger' },
  login_locked: { label: '账号锁定', kind: 'warning' },
  password_change: { label: '修改密码', kind: 'primary' },
  password_reset_admin: { label: '重置密码', kind: 'warning' },
  password_reset: { label: '找回重置', kind: 'warning' },
  email_bind: { label: '绑定邮箱', kind: 'primary' },
  email_unbind: { label: '解绑邮箱', kind: 'warning' },
  rate_limited: { label: '限流拦截', kind: 'info' },
  discuss_delete: { label: '删除讨论', kind: 'danger' },
  discuss_mute: { label: '讨论禁言', kind: 'warning' },
  broadcast: { label: '全站广播', kind: 'primary' },
  user_import: { label: '批量建号', kind: 'primary' },
}

const list = ref<SecurityLogItem[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive({ page: 1, pageSize: 30, type: '', q: '' })

async function load() {
  loading.value = true
  try {
    const r = await adminApi.securityLogs(query.page, query.pageSize, query.type || undefined, query.q || undefined)
    list.value = r.list
    total.value = r.total
  } finally {
    loading.value = false
  }
}

function search() {
  query.page = 1
  load()
}

function fmtTime(iso: string) {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

onMounted(load)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="head">
        <span>安全日志</span>
        <span class="dim">登录与账号安全事件流水，保留 90 天</span>
      </div>
    </template>

    <div class="filters">
      <el-select v-model="query.type" clearable placeholder="全部类型" style="width: 140px" @change="search">
        <el-option v-for="(m, t) in typeMeta" :key="t" :value="t" :label="m.label" />
      </el-select>
      <el-input
        v-model="query.q"
        placeholder="用户名 / IP 搜索"
        clearable
        style="width: 220px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button type="primary" @click="search">查询</el-button>
    </div>

    <el-table v-loading="loading" :data="list" size="small">
      <el-table-column label="时间" width="170">
        <template #default="{ row }">{{ fmtTime(row.at) }}</template>
      </el-table-column>
      <el-table-column label="类型" width="110">
        <template #default="{ row }">
          <el-tag v-if="typeMeta[row.type]" :type="typeMeta[row.type]!.kind" size="small">
            {{ typeMeta[row.type]!.label }}
          </el-tag>
          <span v-else>{{ row.type }}</span>
        </template>
      </el-table-column>
      <el-table-column label="用户" min-width="140">
        <template #default="{ row }">
          {{ row.username || '—' }}
          <span v-if="row.uid" class="dim">#{{ row.uid }}</span>
        </template>
      </el-table-column>
      <el-table-column label="IP" prop="ip" width="140" />
      <el-table-column label="详情" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">{{ row.detail || '—' }}</template>
      </el-table-column>
      <el-table-column label="UA" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">{{ row.ua || '—' }}</template>
      </el-table-column>
    </el-table>

    <div class="pager">
      <el-pagination
        v-model:current-page="query.page"
        background
        layout="total, prev, pager, next"
        :total="total"
        :page-size="query.pageSize"
        @current-change="load"
      />
    </div>
  </el-card>
</template>

<style scoped>
.head {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.filters {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
</style>

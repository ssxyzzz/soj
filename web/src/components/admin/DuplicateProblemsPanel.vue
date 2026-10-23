<script setup lang="ts">
// 重复题扫描面板（v0.18.1 验收追加）：按「标题 + 题面」双键一键扫描全库，
// 疑似重复分组列举——只列出不自动删，处置（编辑/隐藏）由管理员自行完成。
import { onMounted, ref } from 'vue'

import { adminApi } from '@/api'
import type { DuplicateGroup } from '@/types'

const groups = ref<DuplicateGroup[]>([])
const loading = ref(false)
const scanned = ref(false)

async function scan() {
  loading.value = true
  try {
    const r = await adminApi.duplicateProblems()
    groups.value = r.groups
    scanned.value = true
  } finally {
    loading.value = false
  }
}

function typeText(t: string): string {
  if (t === 'objective') return '客观题'
  if (t === 'function') return '函数题'
  if (t === 'interactive') return '交互题'
  return '传统题'
}

function fmtDate(iso: string): string {
  return iso ? iso.replace('T', ' ').slice(0, 16) : '—'
}

onMounted(scan)
</script>

<template>
  <el-card v-loading="loading">
    <template #header>
      <div class="panel-header">
        <span>重复题扫描</span>
        <el-button size="small" :loading="loading" @click="scan">重新扫描</el-button>
      </div>
    </template>

    <el-alert
      v-if="scanned && !groups.length"
      type="success"
      :closable="false"
      title="未发现疑似重复题"
      description="扫描口径：标题与题面（markdown）完全相同的题目视为一组疑似重复。"
    />
    <template v-else>
      <div class="dim scan-hint">
        按「标题 + 题面」完全相同判重，共 {{ groups.length }} 组疑似重复；系统不会自动删除——点题号进编辑页自行处理（改题或设隐藏）。
      </div>
      <div v-for="(g, i) in groups" :key="i" class="dup-group">
        <div class="dup-title">
          <el-tag type="danger" size="small">{{ g.problems.length }} 题重复</el-tag>
          <span class="dup-name">{{ g.title || '（无标题）' }}</span>
        </div>
        <div v-if="g.preview" class="dup-preview dim">{{ g.preview }}…</div>
        <el-table :data="g.problems" size="small">
          <el-table-column label="题号" width="120">
            <template #default="{ row }">
              <router-link :to="`/problems/${row.id}/edit`" target="_blank" class="pid-link">#{{ row.id }}</router-link>
            </template>
          </el-table-column>
          <el-table-column label="题型" width="90">
            <template #default="{ row }">{{ typeText(row.type) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag v-if="row.hidden" type="warning" size="small">隐藏题</el-tag>
              <span v-else class="dim">公开</span>
            </template>
          </el-table-column>
          <el-table-column prop="created_by" label="创建者 UID" width="110" />
          <el-table-column label="创建时间">
            <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
          </el-table-column>
        </el-table>
      </div>
    </template>
  </el-card>
</template>

<style scoped>
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.scan-hint {
  margin-bottom: 12px;
}

.dup-group {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
  padding: 12px;
  margin-bottom: 12px;
}

.dup-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.dup-name {
  font-weight: var(--soj-fw-emphasis);
}

.dup-preview {
  font-size: var(--soj-fs-small);
  margin-bottom: 8px;
  word-break: break-all;
}

.pid-link {
  color: var(--el-color-primary);
}
</style>

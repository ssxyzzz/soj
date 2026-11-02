<script setup lang="ts">
// 重复题扫描面板（v0.18.1 立项；v0.47 换血相似度判重）：按「标题 + 题面」归一化
// 相似度（≥80%）分组一键扫描全库，组内一键标重复/取消（学生端立即不可见）——
// 只列不自动删，改题/隐藏等处置仍由管理员在题目编辑页自行完成。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi, problemApi } from '@/api'
import type { DuplicateGroup } from '@/types'

const groups = ref<DuplicateGroup[]>([])
const loading = ref(false)
const scanned = ref(false)
// 行级标记进行中（一键标重复按钮的 loading）
const marking = ref<number>(0)

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

// 一键标重复/取消（v0.47）：行内状态即时更新；无题目编辑权限的视角由服务端 403 拦下
async function toggleMark(row: DuplicateGroup['problems'][number]) {
  if (marking.value) return
  marking.value = row.id
  try {
    const target = !row.duplicate
    await problemApi.setDuplicate(row.id, target)
    row.duplicate = target
    ElMessage.success(target ? `#${row.id} 已标重复，学生端立即不可见` : `#${row.id} 已取消重复标记`)
  } finally {
    marking.value = 0
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
      description="扫描口径：标题与题面（markdown）归一化后相似度 ≥80% 的题目视为一组疑似重复。"
    />
    <template v-else>
      <div class="dim scan-hint">
        按「标题 + 题面」归一化相似度 ≥80% 判重，共 {{ groups.length }} 组疑似重复；行内一键标重复/取消（学生端立即不可见），也可点题号进编辑页改题——系统不会自动删除。
      </div>
      <div v-for="(g, i) in groups" :key="i" class="dup-group">
        <div class="dup-title">
          <el-tag type="danger" size="small">{{ g.problems.length }} 题重复</el-tag>
          <el-tag type="warning" size="small">相似度 {{ Math.round((g.similarity ?? 0) * 100) }}%</el-tag>
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
          <el-table-column label="状态" width="130">
            <template #default="{ row }">
              <el-tag v-if="row.duplicate" type="info" size="small">已标重复</el-tag>
              <el-tag v-else-if="row.hidden" type="warning" size="small">隐藏题</el-tag>
              <span v-else class="dim">公开</span>
            </template>
          </el-table-column>
          <el-table-column prop="created_by" label="创建者 UID" width="110" />
          <el-table-column label="创建时间">
            <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="110">
            <template #default="{ row }">
              <el-button
                size="small"
                :type="row.duplicate ? 'default' : 'warning'"
                :loading="marking === row.id"
                link
                @click="toggleMark(row as DuplicateGroup['problems'][number])"
              >
                {{ row.duplicate ? '取消重复' : '标重复' }}
              </el-button>
            </template>
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
  margin-bottom: var(--soj-sp-12);
}

.dup-group {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
  padding: var(--soj-sp-12);
  margin-bottom: var(--soj-sp-12);
}

.dup-title {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-bottom: var(--soj-sp-6);
}

.dup-name {
  font-weight: var(--soj-fw-emphasis);
}

.dup-preview {
  font-size: var(--soj-fs-small);
  margin-bottom: var(--soj-sp-8);
  word-break: break-all;
}

.pid-link {
  color: var(--el-color-primary);
}
</style>

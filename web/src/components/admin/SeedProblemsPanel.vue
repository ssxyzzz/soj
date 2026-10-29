<script setup lang="ts">
// 种子题管理面板：种子目录（SOJ_SEED_DIR）↔ 库内对账 + 一键清除 / 一键重新部署。
// 清除 = 题目+数据卷+提交+挑战+题解+题目讨论一并删（提交是等级/榜单统计源，随题删
// 不留孤儿统计）；重部署 = 与启动播种同规则补缺不覆盖，已存在的题跳过保护线上修改。
// root 专属（部署级操作）。
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi } from '@/api'
import type { SeedOutcome, SeedStatus } from '@/types'

const status = ref<SeedStatus | null>(null)
const loading = ref(false)
const deploying = ref(false)
const clearing = ref(false)

const rows = computed(() => status.value?.rows ?? [])
const deployedCount = computed(() => rows.value.filter((r) => r.exists).length)

async function load() {
  loading.value = true
  try {
    status.value = await adminApi.seedStatus()
  } finally {
    loading.value = false
  }
}

function summarize(outcomes: SeedOutcome[]) {
  const ok = outcomes.filter((o) => o.status === 'imported')
  const skipped = outcomes.filter((o) => o.status === 'skipped')
  const failed = outcomes.filter((o) => o.status === 'failed')
  if (failed.length) {
    ElMessage.warning(
      `部署完成：导入 ${ok.length} · 跳过 ${skipped.length} · 失败 ${failed.length}（${failed.map((f) => f.dir).join('、')}，详见服务日志）`,
    )
  } else if (ok.length) {
    ElMessage.success(`部署完成：导入 ${ok.length} 题${skipped.length ? ` · 跳过 ${skipped.length}（已存在）` : ''}`)
  } else {
    ElMessage.info('无需部署：种子题均已存在')
  }
}

async function deploy() {
  try {
    await ElMessageBox.confirm(
      '与启动播种同规则：已存在的题跳过（保护线上修改），只补缺失的题。确定部署？',
      '重新部署种子题',
      { type: 'info', confirmButtonText: '部署', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  deploying.value = true
  try {
    const r = await adminApi.seedDeploy()
    summarize(r.outcomes)
    await load()
  } finally {
    deploying.value = false
  }
}

async function clear() {
  try {
    await ElMessageBox.confirm(
      '将删除全部种子题及其数据卷、提交、挑战、题解与题目讨论，不可恢复；等级/榜单随提交删除同步回落。题单/比赛/作业中的引用会自动剔除展示。确定清除？',
      '清除种子题',
      { type: 'warning', confirmButtonText: '清除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  clearing.value = true
  try {
    const r = await adminApi.seedClear()
    const s = r.stats
    ElMessage.success(
      `已清除 ${s.problems} 题（${s.pids.length ? '#' + s.pids.join(' #') : '无'}）：提交 ${s.submissions} · 挑战 ${s.challenges} · 题解 ${s.solutions} · 讨论 ${s.discussions}`,
    )
    await load()
  } finally {
    clearing.value = false
  }
}

onMounted(load)
</script>

<template>
  <el-card v-loading="loading">
    <template #header>
      <div class="panel-header">
        <span>种子题管理</span>
        <div class="panel-actions">
          <el-button size="small" :loading="deploying" :disabled="!rows.length" @click="deploy">
            重新部署种子题
          </el-button>
          <el-button size="small" type="danger" plain :loading="clearing" :disabled="!deployedCount" @click="clear">
            清除种子题
          </el-button>
        </div>
      </div>
    </template>

    <el-alert
      v-if="status && !status.seed_dir"
      type="warning"
      :closable="false"
      title="未配置种子目录"
      description="未设置 SOJ_SEED_DIR 环境变量，种子题功能不可用。"
    />
    <template v-else>
      <div class="dim seed-hint">
        种子目录 <code>{{ status?.seed_dir }}</code>
        <el-tag :type="status?.marked ? 'success' : 'info'" size="small" class="seed-mark">
          {{ status?.marked ? '启动自动播种已标记' : '启动自动播种未标记' }}
        </el-tag>
        ——重部署与启动播种同口径（数字前缀按题号、无前缀按标题；补缺不覆盖）；清除将连带删除提交/挑战/题解/题目讨论。
      </div>

      <el-alert
        v-if="!loading && !rows.length"
        type="info"
        :closable="false"
        title="种子目录下没有题包"
        description="目录里放 SOJ 格式题包（problem.md + problem.yaml + data/）后刷新此处，即可部署。"
      />
      <el-table v-else :data="rows" size="small">
        <el-table-column prop="dir" label="目录" min-width="180" />
        <el-table-column label="题号" width="90">
          <template #default="{ row }">
            <router-link v-if="row.exists" :to="`/problems/${row.pid}/edit`" target="_blank" class="pid-link">
              #{{ row.pid }}
            </router-link>
            <span v-else class="dim">—</span>
          </template>
        </el-table-column>
        <el-table-column label="标题" min-width="200">
          <template #default="{ row }">
            <span v-if="row.exists">{{ row.db_title }}</span>
            <span v-else class="dim">{{ row.title }}（未部署）</span>
          </template>
        </el-table-column>
        <el-table-column label="提交数" width="90">
          <template #default="{ row }">
            <span :class="{ dim: !row.exists }">{{ row.exists ? row.submissions : '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.exists" type="success" size="small">已部署</el-tag>
            <el-tag v-else type="info" size="small">未部署</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </template>
  </el-card>
</template>

<style scoped>
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.panel-actions {
  display: flex;
  gap: 8px;
}

.seed-hint {
  margin-bottom: 12px;
}

.seed-mark {
  margin: 0 4px;
}

.pid-link {
  color: var(--el-color-primary);
}
</style>


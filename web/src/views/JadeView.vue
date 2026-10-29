<script setup lang="ts">
// 我的玉（v0.33-①）：独立页（2026-10-30 拍板：从个人设置迁出，路由 /jade）——
// 余额 + 流水分页；进入即刷新，并同步顶栏用户菜单（userStore.setJade）。
// 获取与消费自动入账：首次通过、比赛奖池、题解解锁、悬赏、兑换、AI 功能等。
import { onMounted, ref } from 'vue'
import { jadeApi } from '@/api'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const balance = ref(0)
const currency = ref('')
const list = ref<import('@/types').JadeLedgerEntry[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)

async function load(pageNo = 1) {
  page.value = pageNo
  loading.value = true
  try {
    const r = await jadeApi.ledger(pageNo)
    list.value = r.list
    total.value = r.total
    balance.value = r.balance
    const m = await jadeApi.mine()
    currency.value = m.currency
    userStore.setJade(m.balance, m.currency)
  } finally {
    loading.value = false
  }
}

// 流水类型中文（与后端 jadeLedger kind 一一对应；未知类型透传原词）
function kindText(kind: string): string {
  const map: Record<string, string> = {
    award: '首次通过',
    contest_rank: '比赛名次奖',
    contest_participate: '比赛参与奖',
    solution_unlock: '题解解锁',
    bounty_freeze: '悬赏托管',
    bounty_pay: '悬赏收入',
    bounty_refund: '悬赏退回',
    shop_order: '实物兑换',
    ai_hint: 'AI 提示',
    ai_debug: 'AI 找 bug',
    admin_adjust: '管理员调整',
  }
  return map[kind] ?? kind
}

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

onMounted(() => load())
</script>

<template>
  <div class="jade-wrap page" v-loading="loading">
    <div class="page-head">
      <div class="kicker">JADE</div>
      <h1 class="page-title">
        我的{{ currency || '玉' }}<small>余额与收支流水</small>
      </h1>
    </div>

    <el-card shadow="never" class="jcard">
      <div class="panel-h">
        <span class="pt">当前余额</span>
        <span class="dim jtip">首次通过题目得玉（难度定额 + 稀有掉落），比赛奖池随成绩发布结算</span>
      </div>
      <div class="jbody">
        <div class="jade-balance">
          <b>{{ balance }}</b>
          <span class="unit">{{ currency || '玉' }}</span>
        </div>
        <el-table :data="list" size="small" v-loading="loading" empty-text="还没有流水——去做道题吧">
          <el-table-column label="时间" width="170">
            <template #default="{ row }">{{ fmtTime(row.at) }}</template>
          </el-table-column>
          <el-table-column label="类型" width="110">
            <template #default="{ row }">{{ kindText(row.kind) }}</template>
          </el-table-column>
          <el-table-column label="变动" width="90">
            <template #default="{ row }">
              <span :class="row.delta > 0 ? 'jade-in' : 'jade-out'">{{ row.delta > 0 ? '+' : '' }}{{ row.delta }}</span>
            </template>
          </el-table-column>
          <el-table-column label="说明" min-width="220" show-overflow-tooltip>
            <template #default="{ row }">{{ row.reason }}</template>
          </el-table-column>
        </el-table>
        <div v-if="total > pageSize" class="jade-pager">
          <el-pagination
            background
            layout="prev, pager, next"
            :total="total"
            :page-size="pageSize"
            :current-page="page"
            @current-change="load"
          />
        </div>
      </div>
    </el-card>
  </div>
</template>

<style scoped>
.jade-wrap {
  max-width: 980px;
}

/* 余额为页面主视觉：玉青大字（沿用原设置页玉段配色） */
.jade-balance {
  margin: 6px 0 16px;
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.jade-balance b {
  color: #0f9b8e;
  font-size: 34px;
  font-weight: var(--soj-fw-bold);
  font-variant-numeric: tabular-nums;
  line-height: 1;
}

.jade-balance .unit {
  font-size: 15px;
  color: var(--soj-ink-2, #22334d);
}

.jcard {
  margin-bottom: 16px;
}

.jcard :deep(.el-card__body) {
  padding: 8px 26px 14px;
}

.panel-h {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 8px 0 0;
}

.panel-h .pt {
  font-size: 15px;
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy, #0b1f45);
}

.jtip {
  font-size: var(--soj-fs-13);
}

.jade-in {
  color: #0f9b8e;
  font-weight: 600;
}

.jade-out {
  color: var(--el-color-danger);
  font-weight: 600;
}

.jade-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 10px;
}
</style>

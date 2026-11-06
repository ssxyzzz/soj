<script setup lang="ts">
// 我的玉（v0.33-①，v0.41 改两列）：主列余额 + 流水分页；右列玩法介绍 + 实物兑换
// （v0.41 调整：兑换不再占导航栏，入口并入本页右列——「钩住不施压」，浏览流水时
// 顺手看到兑换目录）。URL 即状态：页码进 query，返回不丢页。
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { jadeApi, shopApi, type ShopItem, type ShopOrder } from '@/api'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import { useListQuery } from '@/composables/useListQuery'
import { useSiteStore } from '@/stores/site'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const site = useSiteStore()

// URL 即状态（v0.39）：页码进 query，返回不丢页
const lq = useListQuery()

const balance = ref(0)
const currency = ref('')
const list = ref<import('@/types').JadeLedgerEntry[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)

async function load(pageNo = 1) {
  page.value = pageNo
  lq.sync({ page: pageNo > 1 ? pageNo : undefined })
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

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

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

// ---- 实物兑换（v0.41 并入右列；模块默认关，site.jadeShopEnabled 控制显隐）----
const shopItems = ref<ShopItem[]>([])
const shopOrders = ref<ShopOrder[]>([])
const ordering = ref(false)

async function loadShop() {
  if (!site.jadeShopEnabled) return
  try {
    const r = await shopApi.list()
    shopItems.value = r.list
    balance.value = r.balance
    userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
    const o = await shopApi.myOrders()
    shopOrders.value = o.list
  } catch {
    /* 未开放/失败静默（403 兜底） */
  }
}

function stockText(it: ShopItem): string {
  if (it.stock < 0) return '不限量'
  return it.stock > 0 ? `剩 ${it.stock}` : '已兑完'
}

const orderStatusText: Record<string, string> = {
  pending: '待发放',
  fulfilled: '已发放',
  rejected: '已拒绝（退玉）',
}

async function order(it: ShopItem) {
  if (it.price > balance.value) {
    ElMessage.warning('余额不足，先去做题攒玉吧')
    return
  }
  if (it.stock === 0) {
    ElMessage.warning('已兑完')
    return
  }
  try {
    const { value: note } = await ElMessageBox.prompt(
      `兑换「${it.name}」将花费 ${it.price} ${currency.value || '玉'}线下找老师领取，确认？（可留言备注，如尺码/自取时间）`,
      '确认兑换',
      {
        confirmButtonText: `兑换（${it.price} ${currency.value || '玉'}）`,
        cancelButtonText: '再想想',
        inputPlaceholder: '留言备注（可选，上限 100 字）',
      },
    )
    ordering.value = true
    try {
      const r = await shopApi.order(it.id, (note ?? '').slice(0, 100))
      userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
      balance.value = r.balance
      ElMessage.success('兑换成功，等待老师发放')
      await Promise.all([load(page.value), loadShop()])
    } finally {
      ordering.value = false
    }
  } catch {
    /* 取消或拦截器已提示 */
  }
}

onMounted(() => {
  site.ensure()
  load(lq.num('page'))
  loadShop()
})
</script>

<template>
  <PageShell>
    <div class="page-head">
      <div class="kicker">JADE</div>
      <h1 class="page-title">
        我的{{ currency || '玉' }}<small>余额与收支流水</small>
      </h1>
    </div>

    <!-- 主列：余额 + 流水 -->
    <div class="card jcard">
      <div class="panel-h">
        <TipLabel label="当前余额" tip="首次通过题目得玉（难度定额 + 稀有掉落），比赛奖池随成绩发布结算" />
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
    </div>

    <template #aside>
      <!-- 玩法介绍（v0.41）：玉是什么 / 怎么获得 / 怎么消费 / 怎么兑换 -->
      <RailCard ov="玩法" title="什么是玉" class="aside-card">
        <div class="howto">
          <p class="how-line"><b>怎么获得</b></p>
          <p class="how-item">✦ 首次通过题目：难度定额 + 稀有掉落（越难的题越值钱）</p>
          <p class="how-item">✦ 每日挑战：当日题首 AC 掉落 ×2</p>
          <p class="how-item">✦ 复仇成功：错题重战掉落 ×1.5</p>
          <p class="how-item">✦ 比赛奖池：名次奖 + 参与奖随成绩发放</p>
          <p class="how-item">✦ 升级祝福：升等级后 48h 内收入 ×2</p>
          <p class="how-line"><b>怎么消费</b></p>
          <p class="how-item">◆ 解锁题解（难度 × 系数，等级折扣）</p>
          <p class="how-item">◆ AI 助教：理解题目 / 算法思路 / 找 bug</p>
          <p class="how-item">◆ 讨论区发悬赏，采纳回复</p>
          <p class="how-line"><b>怎么兑换</b></p>
          <p class="how-item">◆ 右侧实物兑换：原价兑换，线下找老师领取</p>
        </div>
      </RailCard>

      <!-- 实物兑换（v0.41 并入右列） -->
      <RailCard v-if="site.jadeShopEnabled" ov="兑换" title="实物兑换" class="aside-card">
        <template v-if="shopItems.length">
          <div class="shop-list">
            <div v-for="it in shopItems" :key="it.id" class="shop-row" :class="{ out: it.stock === 0 }">
              <img v-if="it.image" :src="it.image" :alt="it.name" class="shop-thumb" loading="lazy" />
              <span v-else class="shop-glyph">🎁</span>
              <div class="shop-row-main">
                <b class="shop-name">{{ it.name }}</b>
                <span class="shop-meta">{{ stockText(it) }}<template v-if="it.per_user_limit"> · 限 {{ it.per_user_limit }} 份</template></span>
                <span class="shop-price"><b>{{ it.price }}</b> {{ currency || '玉' }}</span>
              </div>
              <el-button
                size="small"
                type="primary"
                :loading="ordering"
                :disabled="it.stock === 0 || it.price > balance"
                @click="order(it)"
              >
                兑换
              </el-button>
            </div>
          </div>
          <template v-if="shopOrders.length">
            <p class="shop-orders-cap">我的兑换记录</p>
            <div class="my-orders">
              <div v-for="o in shopOrders" :key="o.id" class="my-order-row">
                <span class="mo-name">{{ o.item_name }}</span>
                <el-tag
                  :type="o.status === 'fulfilled' ? 'success' : o.status === 'rejected' ? 'danger' : 'info'"
                  size="small"
                  effect="plain"
                >
                  {{ orderStatusText[o.status] ?? o.status }}
                </el-tag>
              </div>
            </div>
          </template>
        </template>
        <el-empty v-else description="暂时没有可兑换的物品" :image-size="56" />
      </RailCard>
    </template>
  </PageShell>
</template>

<style scoped>
/* 余额为页面主视觉：玉青大字（沿用原设置页玉段配色） */
.jade-balance {
  display: flex;
  align-items: baseline;
  gap: var(--soj-sp-10);
  margin-bottom: var(--soj-sp-16);
}

.jade-balance b {
  font-size: 44px; /* 装饰档豁免：≥40px 主视觉大数字 */
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-teal-deep);
  line-height: 1;
}

.jade-balance .unit {
  color: var(--soj-teal-deep);
  font-size: var(--soj-fs-16);
}

.jcard {
  padding: var(--soj-sp-20) var(--soj-sp-24);
}

.jade-pager {
  margin-top: var(--soj-sp-14);
  display: flex;
  justify-content: center;
}

.jade-in {
  color: var(--soj-success-deep);
  font-weight: var(--soj-fw-bold);
}

.jade-out {
  color: var(--soj-danger-deep);
  font-weight: var(--soj-fw-bold);
}

/* 右列：玩法介绍 */
.howto {
  font-size: var(--soj-fs-13);
  line-height: 1.7;
  color: var(--el-text-color-regular);
}

.how-line {
  margin: var(--soj-sp-10) 0 var(--soj-sp-2);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-blue);
}

.how-line:first-child {
  margin-top: 0;
}

.how-item {
  margin: 0;
}

/* 右列：实物兑换竖列 */
.shop-list {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-8);
}

.shop-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
  padding: var(--soj-sp-8);
  border-radius: 10px;
  border: 1px solid var(--soj-line);
}

.shop-row.out {
  opacity: 0.55;
}

.shop-thumb {
  width: 44px;
  height: 44px;
  border-radius: 8px;
  object-fit: cover;
}

.shop-glyph {
  width: 44px;
  height: 44px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--soj-fs-22);
  background: var(--soj-blue-soft);
  border-radius: 8px;
}

.shop-row-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-2);
}

.shop-name {
  font-size: var(--soj-fs-13);
}

.shop-meta {
  font-size: var(--soj-fs-12);
  color: var(--el-text-color-secondary);
}

.shop-price {
  font-size: var(--soj-fs-12);
  color: var(--soj-teal-deep);
  font-weight: var(--soj-fw-emphasis);
}

.shop-price b {
  font-size: var(--soj-fs-14);
}

.shop-orders-cap {
  margin: var(--soj-sp-12) 0 var(--soj-sp-4);
  font-size: var(--soj-fs-12);
  color: var(--el-text-color-secondary);
}

.my-orders {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-4);
}

.my-order-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-8);
  font-size: var(--soj-fs-12);
  padding: var(--soj-sp-4) 0;
  border-bottom: 1px dashed var(--el-border-color-lighter);
}

.mo-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

<script setup lang="ts">
// 实物兑换（v0.33-②）：物品网格 + 下单（扣玉扣库存）+ 我的订单。
// 模块默认关（后台 root 开启），路由守卫侧已挡未开放（接口 403 兜底）。
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { shopApi, type ShopItem, type ShopOrder } from '@/api'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const items = ref<ShopItem[]>([])
const balance = ref(0)
const loading = ref(false)
const orders = ref<ShopOrder[]>([])

async function load() {
  loading.value = true
  try {
    const r = await shopApi.list()
    items.value = r.list
    balance.value = r.balance
    userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
  } finally {
    loading.value = false
  }
}

async function loadOrders() {
  try {
    const r = await shopApi.myOrders()
    orders.value = r.list
  } catch {
    /* 静默 */
  }
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
      `兑换「${it.name}」将花费 ${it.price} 玉线下找老师领取，确认？（可留言备注，如尺码/自取时间）`,
      '确认兑换',
      {
        confirmButtonText: `兑换（${it.price} 玉）`,
        cancelButtonText: '再想想',
        inputPlaceholder: '留言备注（可选，上限 100 字）',
      },
    )
    const r = await shopApi.order(it.id, (note ?? '').slice(0, 100))
    userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
    balance.value = r.balance
    ElMessage.success('兑换成功，等待老师发放')
    await Promise.all([load(), loadOrders()])
  } catch {
    /* 取消或拦截器已提示 */
  }
}

function stockText(it: ShopItem): string {
  if (it.stock < 0) return '不限量'
  return it.stock > 0 ? `剩 ${it.stock}` : '已兑完'
}

const statusText: Record<string, string> = { pending: '待发放', fulfilled: '已发放', rejected: '已拒绝（退玉）' }

onMounted(() => {
  load()
  loadOrders()
})
</script>

<template>
  <div class="shop-wrap page" v-loading="loading">
    <div class="page-head">
      <div class="kicker">SHOP</div>
      <h1 class="page-title">实物兑换<small>当前余额 {{ balance }} {{ userStore.jadeCurrency || '玉' }}</small></h1>
    </div>

    <div v-if="items.length" class="shop-grid">
      <div v-for="it in items" :key="it.id" class="shop-card" :class="{ out: it.stock === 0 }">
        <div class="shop-img">
          <img v-if="it.image" :src="it.image" :alt="it.name" loading="lazy" />
          <span v-else class="shop-glyph">🎁</span>
        </div>
        <div class="shop-info">
          <b class="shop-name">{{ it.name }}</b>
          <span class="shop-meta">{{ stockText(it) }}<template v-if="it.per_user_limit"> · 每人限 {{ it.per_user_limit }} 份</template></span>
          <div class="shop-foot">
            <span class="shop-price"><b>{{ it.price }}</b> {{ userStore.jadeCurrency || '玉' }}</span>
            <el-button size="small" type="primary" :disabled="it.stock === 0 || it.price > balance" @click="order(it)">
              兑换
            </el-button>
          </div>
        </div>
      </div>
    </div>
    <el-empty v-else-if="!loading" description="暂时没有可兑换的物品" :image-size="70" />

    <div class="shop-orders">
      <h3 class="so-title">我的兑换记录</h3>
      <el-table :data="orders" size="small" empty-text="还没有兑换记录">
        <el-table-column prop="id" label="订单" width="80" />
        <el-table-column prop="item_name" label="物品" min-width="160" />
        <el-table-column label="花费" width="90">
          <template #default="{ row }">{{ row.price }} 玉</template>
        </el-table-column>
        <el-table-column label="状态" width="130">
          <template #default="{ row }">
            <el-tag :type="row.status === 'fulfilled' ? 'success' : row.status === 'rejected' ? 'danger' : 'info'" size="small" effect="plain">
              {{ statusText[row.status] ?? row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="下单时间" width="150" />
        <el-table-column prop="note" label="留言" min-width="140" show-overflow-tooltip />
      </el-table>
    </div>
  </div>
</template>

<style scoped>
.shop-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 16px;
  margin-bottom: 28px;
}

.shop-card {
  display: flex;
  gap: 14px;
  padding: 14px;
  border: 1px solid var(--soj-line);
  border-radius: 12px;
  background: var(--el-fill-color-blank);
}

.shop-card.out {
  opacity: 0.55;
}

.shop-img {
  width: 64px;
  height: 64px;
  border-radius: 10px;
  background: var(--soj-ink-06, #f2f5fa);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  overflow: hidden;
}

.shop-img img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.shop-glyph {
  font-size: 28px;
}

.shop-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.shop-name {
  font-size: 15px;
}

.shop-meta {
  font-size: 12px;
  color: var(--soj-ink-3);
}

.shop-foot {
  margin-top: auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.shop-price b {
  color: #0f9b8e;
  font-size: 17px;
}

.so-title {
  font-size: 15px;
  margin: 0 0 10px;
}
</style>

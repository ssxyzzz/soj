<script setup lang="ts">
// 实物兑换管理面板（v0.33-②，root）：物品目录 CRUD + 订单履约（发放/拒绝退玉）。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { adminApi, type ShopItem, type ShopOrder } from '@/api'

const items = ref<ShopItem[]>([])
const orders = ref<ShopOrder[]>([])
const orderTotal = ref(0)
const orderStatus = ref('')
const orderPage = ref(1)
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const [a, b] = await Promise.all([
      adminApi.shopItems(),
      adminApi.shopOrders(orderStatus.value, orderPage.value),
    ])
    items.value = a.list
    orders.value = b.list
    orderTotal.value = b.total
  } finally {
    loading.value = false
  }
}

// ---- 物品 CRUD ----
const showItem = ref(false)
const saving = ref(false)
const itemForm = reactive({
  id: 0, name: '', image: '', price: 10, stock: 10, per_user_limit: 0, active: true,
})

function newItem() {
  Object.assign(itemForm, { id: 0, name: '', image: '', price: 10, stock: 10, per_user_limit: 0, active: true })
  showItem.value = true
}

function editItem(it: ShopItem) {
  Object.assign(itemForm, {
    id: it.id, name: it.name, image: it.image, price: it.price,
    stock: it.stock, per_user_limit: it.per_user_limit, active: it.active,
  })
  showItem.value = true
}

async function saveItem() {
  if (!itemForm.name.trim()) {
    ElMessage.warning('请填写物品名称')
    return
  }
  saving.value = true
  try {
    await adminApi.shopUpsertItem({ ...itemForm, name: itemForm.name.trim() })
    ElMessage.success('已保存')
    showItem.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function removeItem(it: ShopItem) {
  try {
    await ElMessageBox.confirm(`删除物品「${it.name}」？（历史订单不受影响）`, '删除物品', {
      type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消',
    })
  } catch {
    return
  }
  await adminApi.shopDeleteItem(it.id)
  ElMessage.success('已删除')
  await load()
}

// ---- 订单履约 ----
async function resolve(o: ShopOrder, action: 'fulfill' | 'reject') {
  const verb = action === 'fulfill' ? '标记已发放' : '拒绝并退玉'
  try {
    await ElMessageBox.confirm(
      `订单 #${o.id}「${o.item_name}」（${o.uname}，${o.price} 玉）${verb}？`,
      '订单处理',
      { type: action === 'reject' ? 'warning' : 'info', confirmButtonText: verb, cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  await adminApi.shopResolveOrder(o.id, action)
  ElMessage.success('已处理')
  await load()
}

const statusMeta: Record<string, { label: string; type: 'success' | 'danger' | 'info' }> = {
  pending: { label: '待处理', type: 'info' },
  fulfilled: { label: '已发放', type: 'success' },
  rejected: { label: '已拒绝', type: 'danger' },
}

onMounted(load)
</script>

<template>
  <el-card v-loading="loading">
    <div class="panel-h panel-head">
      <span class="pt">实物兑换</span>
      <span class="dim count">物品 {{ items.length }} · 待处理 {{ statusMeta ? '' : '' }}{{ orders.filter((o) => o.status === 'pending').length }}</span>
      <div class="acts">
        <el-button type="primary" @click="newItem">新建物品</el-button>
      </div>
    </div>

    <el-table :data="items" size="small">
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column label="物品" min-width="160">
        <template #default="{ row }"><b>{{ row.name }}</b></template>
      </el-table-column>
      <el-table-column label="价（玉）" width="90">
        <template #default="{ row }">{{ row.price }}</template>
      </el-table-column>
      <el-table-column label="库存" width="90">
        <template #default="{ row }">{{ row.stock < 0 ? '不限' : row.stock }}</template>
      </el-table-column>
      <el-table-column label="限购" width="80">
        <template #default="{ row }">{{ row.per_user_limit || '不限' }}</template>
      </el-table-column>
      <el-table-column label="上架" width="80">
        <template #default="{ row }">
          <el-tag :type="row.active ? 'success' : 'info'" size="small" effect="plain">{{ row.active ? '上架' : '下架' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="130">
        <template #default="{ row }">
          <el-button size="small" link type="primary" @click="editItem(row as ShopItem)">编辑</el-button>
          <el-button size="small" link type="danger" @click="removeItem(row as ShopItem)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-divider content-position="left">兑换订单</el-divider>
    <div class="order-filter">
      <el-radio-group v-model="orderStatus" size="small" @change="orderPage = 1; load()">
        <el-radio-button value="">全部</el-radio-button>
        <el-radio-button value="pending">待处理</el-radio-button>
        <el-radio-button value="fulfilled">已发放</el-radio-button>
        <el-radio-button value="rejected">已拒绝</el-radio-button>
      </el-radio-group>
    </div>
    <el-table :data="orders" size="small" empty-text="暂无订单">
      <el-table-column prop="id" label="订单" width="70" />
      <el-table-column prop="uname" label="学生" width="110" />
      <el-table-column prop="item_name" label="物品" min-width="140" />
      <el-table-column label="花费" width="80">
        <template #default="{ row }">{{ row.price }} 玉</template>
      </el-table-column>
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="statusMeta[row.status]?.type ?? 'info'" size="small" effect="plain">
            {{ statusMeta[row.status]?.label ?? row.status }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="下单时间" width="140" />
      <el-table-column prop="note" label="留言" min-width="120" show-overflow-tooltip />
      <el-table-column label="操作" width="130">
        <template #default="{ row }">
          <template v-if="row.status === 'pending'">
            <el-button size="small" link type="success" @click="resolve(row as ShopOrder, 'fulfill')">已发放</el-button>
            <el-button size="small" link type="danger" @click="resolve(row as ShopOrder, 'reject')">拒绝退玉</el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>
    <div v-if="orderTotal > 20" class="order-pager">
      <el-pagination layout="prev, pager, next" :total="orderTotal" :page-size="20" :current-page="orderPage" @current-change="(p: number) => { orderPage = p; load() }" />
    </div>

    <!-- 物品编辑弹窗 -->
    <el-dialog v-model="showItem" :title="itemForm.id ? '编辑物品' : '新建物品'" width="520">
      <el-form label-width="100px">
        <el-form-item label="名称" required>
          <el-input v-model="itemForm.name" maxlength="40" />
        </el-form-item>
        <el-form-item label="图片 URL">
          <el-input v-model="itemForm.image" placeholder="可空；建议站内 /uploads 或题面资产直链" maxlength="512" />
        </el-form-item>
        <el-form-item label="价格（玉）">
          <el-input-number v-model="itemForm.price" :min="1" :max="1000000" />
        </el-form-item>
        <el-form-item label="库存">
          <el-input-number v-model="itemForm.stock" :min="-1" :max="1000000" />
          <span class="dim" style="margin-left: var(--soj-sp-8)">-1 = 不限量</span>
        </el-form-item>
        <el-form-item label="每人限购">
          <el-input-number v-model="itemForm.per_user_limit" :min="0" :max="1000" />
          <span class="dim" style="margin-left: var(--soj-sp-8)">0 = 不限购</span>
        </el-form-item>
        <el-form-item label="上架">
          <el-switch v-model="itemForm.active" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showItem = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveItem">保存</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<style scoped>
.panel-head {
  padding: var(--soj-sp-16) var(--soj-sp-20);
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
}
.acts {
  margin-left: auto;
}
.order-filter {
  margin-bottom: var(--soj-sp-10);
}
.order-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--soj-sp-10);
}
</style>

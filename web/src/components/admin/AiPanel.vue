<script setup lang="ts">
// AI 助教配置 + 调用审计面板（v0.33-③/④，root）：供应商多档（GLM/DeepSeek 预设快捷
// 填充 + 自定义 OpenAI 兼容）、key 只写不回显（留空 = 不改）、启用档切换、计费参数
//（每日上限/基准价/难度倍率说明/等级折扣两档）、调用审计表（tokens/费用/成败）。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'

interface ProviderRow {
  name: string
  base_url: string
  api_key: string // 发送时空串 = 不改；回显侧只有 has_key
  model: string
  has_key?: boolean
}

const loading = ref(false)
const saving = ref(false)
const providers = ref<ProviderRow[]>([])
const active = ref('')
const dailyLimit = ref(5)
const prices = reactive({ hint1: 2, hint2: 4, hint3: 6, debug: 8 })
const discount = reactive({ lv1: 5, pct1: 90, lv2: 8, pct2: 80 })

// 预设快捷填充（点击填入 BaseURL + 推荐模型；key 仍需手填）
const presets: Record<string, { base_url: string; model: string }> = {
  GLM: { base_url: 'https://open.bigmodel.cn/api/paas/v4', model: 'glm-4.6' },
  DeepSeek: { base_url: 'https://api.deepseek.com', model: 'deepseek-chat' },
}

function addProvider(preset?: string) {
  const p = preset ? presets[preset] : { base_url: '', model: '' }
  providers.value.push({
    name: preset ?? '',
    base_url: p.base_url,
    api_key: '',
    model: p.model,
    has_key: false,
  })
}

function removeProvider(i: number) {
  if (providers.value[i]?.name === active.value) active.value = ''
  providers.value.splice(i, 1)
}

async function load() {
  loading.value = true
  try {
    const r = await adminApi.aiConfig()
    providers.value = (r.providers ?? []).map((p) => ({ ...p, api_key: '' }))
    active.value = r.active ?? ''
    dailyLimit.value = r.daily_limit ?? 5
    Object.assign(prices, r.prices ?? {})
    Object.assign(discount, r.discount ?? {})
  } finally {
    loading.value = false
  }
}

async function save() {
  if (active.value && !providers.value.some((p) => p.name === active.value)) {
    ElMessage.warning('启用档不存在')
    return
  }
  saving.value = true
  try {
    await adminApi.aiSaveConfig({
      providers: providers.value.map((p) => ({
        name: p.name, base_url: p.base_url, api_key: p.api_key, model: p.model,
      })),
      active: active.value,
      daily_limit: dailyLimit.value,
      prices: { ...prices },
      discount: { ...discount },
    })
    ElMessage.success('AI 配置已保存')
    await load()
    await loadCalls()
  } finally {
    saving.value = false
  }
}

// ---- 调用审计 ----
interface AiCallRow {
  id: number; uid: number; pid: number; kind: string; tokens: number
  prompt_chars: number; cost: number; provider: string; model: string
  ok: boolean; err: string; at: string
}
const calls = ref<AiCallRow[]>([])
const callTotal = ref(0)
const callPage = ref(1)

async function loadCalls(page = 1) {
  callPage.value = page
  try {
    const r = await adminApi.aiCalls(page)
    calls.value = r.list
    callTotal.value = r.total
  } catch {
    /* 静默 */
  }
}

const kindText: Record<string, string> = { hint1: '提示①', hint2: '提示②', hint3: '提示③', debug: '找 bug' }

onMounted(() => {
  load()
  loadCalls()
})
</script>

<template>
  <el-card v-loading="loading">
    <div class="panel-h panel-head">
      <span class="pt">AI 助教</span>
      <div class="acts" style="margin-left: auto; display: flex; gap: 8px">
        <el-button @click="addProvider('GLM')">+ GLM</el-button>
        <el-button @click="addProvider('DeepSeek')">+ DeepSeek</el-button>
        <el-button @click="addProvider()">+ 自定义</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </div>

    <el-alert
      title="未配置可用的供应商时，学生端 AI 入口整体隐藏。Key 只写不回显：已配置的档位留空 Key 即保留原值。"
      type="info" :closable="false" style="margin-bottom: 14px"
    />

    <el-table :data="providers" size="small" empty-text="还没有供应商——用上方 GLM / DeepSeek 预设快速添加">
      <el-table-column label="启用" width="60">
        <template #default="{ row }">
          <el-radio v-model="active" :value="row.name"><span /></el-radio>
        </template>
      </el-table-column>
      <el-table-column label="档名" width="140">
        <template #default="{ row }"><el-input v-model="row.name" maxlength="32" placeholder="如 glm / ds" /></template>
      </el-table-column>
      <el-table-column label="Base URL" min-width="230">
        <template #default="{ row }"><el-input v-model="row.base_url" placeholder="https://…（OpenAI 兼容根地址）" /></template>
      </el-table-column>
      <el-table-column label="API Key" width="190">
        <template #default="{ row }">
          <el-input v-model="row.api_key" type="password" show-password
            :placeholder="row.has_key ? '已配置（留空不改）' : 'sk-…'" />
        </template>
      </el-table-column>
      <el-table-column label="模型" width="160">
        <template #default="{ row }"><el-input v-model="row.model" placeholder="glm-4.6 / deepseek-chat" /></template>
      </el-table-column>
      <el-table-column label="" width="70">
        <template #default="{ $index }">
          <el-button size="small" link type="danger" @click="removeProvider($index)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-divider content-position="left">计费</el-divider>
    <el-form label-position="left" label-width="190px" style="max-width: 720px">
      <el-form-item label="每日每人调用上限（次）">
        <el-input-number v-model="dailyLimit" :min="1" :max="100" />
        <span class="dim" style="margin-left: 8px">提示 + 找 bug 合计；教师/root 不扣玉不计次</span>
      </el-form-item>
      <el-form-item label="基准价（提示①/②/③、找 bug）">
        <div class="price-row">
          <el-input-number v-model="prices.hint1" :min="0" :max="1000" size="small" />
          <el-input-number v-model="prices.hint2" :min="0" :max="1000" size="small" />
          <el-input-number v-model="prices.hint3" :min="0" :max="1000" size="small" />
          <el-input-number v-model="prices.debug" :min="0" :max="1000" size="small" />
        </div>
      </el-form-item>
      <el-form-item label="难度倍率">
        <span class="dim">服务端固定：单价 = 基准 × ⌈难度/3⌉（D1–3 ×1 / D4–6 ×2 / D7–10 ×3）</span>
      </el-form-item>
      <el-form-item label="等级折扣（两档）">
        <div class="price-row">
          Lv ≥ <el-input-number v-model="discount.lv1" :min="1" :max="10" size="small" />
          <el-input-number v-model="discount.pct1" :min="10" :max="100" size="small" />%
          ｜ Lv ≥
          <el-input-number v-model="discount.lv2" :min="1" :max="10" size="small" />
          <el-input-number v-model="discount.pct2" :min="10" :max="100" size="small" />%
        </div>
        <span class="dim" style="display: block">默认 Lv.5–7 九折、Lv.8+ 八折（难度价折后向上取整）</span>
      </el-form-item>
    </el-form>

    <el-divider content-position="left">调用审计</el-divider>
    <el-table :data="calls" size="small" empty-text="还没有 AI 调用">
      <el-table-column prop="at" label="时间" width="140" />
      <el-table-column prop="uid" label="用户" width="70" />
      <el-table-column prop="pid" label="题目" width="70" />
      <el-table-column label="类型" width="80">
        <template #default="{ row }">{{ kindText[row.kind] ?? row.kind }}</template>
      </el-table-column>
      <el-table-column prop="tokens" label="tokens" width="90" />
      <el-table-column prop="prompt_chars" label="素材字符" width="90" />
      <el-table-column label="费用" width="70">
        <template #default="{ row }">{{ row.cost }} 玉</template>
      </el-table-column>
      <el-table-column prop="provider" label="供应商" width="90" />
      <el-table-column label="结果" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">
          <el-tag v-if="row.ok" type="success" size="small" effect="plain">成功</el-tag>
          <el-tag v-else type="danger" size="small" effect="plain">失败</el-tag>
          <span v-if="row.err" class="dim" style="margin-left: 6px">{{ row.err }}</span>
        </template>
      </el-table-column>
    </el-table>
    <div v-if="callTotal > 20" style="display: flex; justify-content: flex-end; margin-top: 10px">
      <el-pagination layout="prev, pager, next" :total="callTotal" :page-size="20" :current-page="callPage"
        @current-change="loadCalls" />
    </div>
  </el-card>
</template>

<style scoped>
.panel-head {
  padding: 16px 20px;
  display: flex;
  align-items: center;
  gap: 10px;
}
.price-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.price-row .el-input-number {
  width: 90px;
}
</style>

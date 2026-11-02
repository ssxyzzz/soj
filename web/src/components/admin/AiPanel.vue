<script setup lang="ts">
// AI 助教配置 + 调用审计面板（v0.33-③/④，root）：供应商多档（GLM/DeepSeek 预设快捷
// 填充 + 自定义 OpenAI 兼容）、key 只写不回显（留空 = 不改）、启用档切换、计费参数
//（每日上限/定价锚说明/等级折扣两档）、调用审计表（tokens/费用/成败）。
import { onMounted, ref } from 'vue'
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
const staffDailyLimit = ref(20) // 特权层（教师/管理员）每日上限，0 = 不限；root 恒不限
const tutorName = ref('小玉')

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
    staffDailyLimit.value = r.staff_daily_limit ?? 20
    tutorName.value = r.tutor_name ?? '小玉'
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
      staff_daily_limit: staffDailyLimit.value,
      tutor_name: tutorName.value.trim(),
    })
    ElMessage.success('AI 配置已保存')
    await load()
    await loadCalls()
  } finally {
    saving.value = false
  }
}

// ---- 调用审计（v0.51 游标分页：aiCalls 只增不减，页号 skip 深翻页线性放大弃用）----
interface AiCallRow {
  id: number; uid: number; pid: number; kind: string; tokens: number
  prompt_chars: number; cost: number; provider: string; model: string
  ok: boolean; err: string; at: string
}
const calls = ref<AiCallRow[]>([])
const callTotal = ref(0)
const pageCursors = ref<string[]>(['']) // 各页起点游标，pageCursors[0]='' 即首页
const pageIdx = ref(0)
const nextCursor = ref('') // 当前页响应的下一页游标；空 = 末页

async function loadCalls(idx = 0) {
  if (idx < 0 || pageCursors.value[idx] === undefined) return
  pageIdx.value = idx
  try {
    const r = await adminApi.aiCalls(pageCursors.value[idx])
    calls.value = r.list
    callTotal.value = r.total
    nextCursor.value = r.next_cursor
    // 首次翻到的页登记其下一页起点（回翻不覆盖已登记游标）
    if (pageCursors.value[idx + 1] === undefined && r.next_cursor) {
      pageCursors.value[idx + 1] = r.next_cursor
    }
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
      <div class="acts" style="margin-left: auto; display: flex; gap: var(--soj-sp-8)">
        <el-button @click="addProvider('GLM')">+ GLM</el-button>
        <el-button @click="addProvider('DeepSeek')">+ DeepSeek</el-button>
        <el-button @click="addProvider()">+ 自定义</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </div>

    <el-alert
      title="未配置可用的供应商时，学生端 AI 入口整体隐藏。Key 只写不回显：已配置的档位留空 Key 即保留原值。"
      type="info" :closable="false" style="margin-bottom: var(--soj-sp-14)"
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
          <el-input
v-model="row.api_key" type="password" show-password
            :placeholder="row.has_key ? '已配置（留空不改）' : 'sk-…'"
/>
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
      <el-form-item label="AI 名字">
        <el-input v-model="tutorName" maxlength="12" style="width: 180px" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">
          学生端 AI 助教的称呼（1–12 字，默认「小玉」；面板、找 bug 按钮等文案统一显示）
        </span>
      </el-form-item>
      <el-form-item label="每日每人调用上限（次）">
        <el-input-number v-model="dailyLimit" :min="1" :max="100" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">提示 + 找 bug 合计；学生档（默认 5）</span>
      </el-form-item>
      <el-form-item label="特权层每日上限（次）">
        <el-input-number v-model="staffDailyLimit" :min="0" :max="1000" />
        <span class="dim" style="margin-left: var(--soj-sp-8)">
          教师 / 管理员免费但计次（默认 20）；0 = 不限；root 恒不限次
        </span>
      </el-form-item>
      <el-form-item label="单价（写死锚定题解解锁价）">
        <span class="dim">
          两级提示合计 = 题解价 × 70%（理解:思路 = 1:2 拆分）；找 bug = 题解价 × 90%；均向下取整、最低 1 玉。
          题解价 = 难度 × 解锁系数（玉经济设置）
        </span>
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
          <span v-if="row.err" class="dim" style="margin-left: var(--soj-sp-6)">{{ row.err }}</span>
        </template>
      </el-table-column>
    </el-table>
    <div v-if="callTotal > 20" style="display: flex; justify-content: flex-end; align-items: center; gap: var(--soj-sp-8); margin-top: var(--soj-sp-10)">
      <span style="color: var(--soj-ink-2); font-size: var(--soj-fs-13)">共 {{ callTotal }} 条</span>
      <el-button size="small" :disabled="pageIdx === 0" @click="loadCalls(pageIdx - 1)">上一页</el-button>
      <el-button size="small" :disabled="!nextCursor" @click="loadCalls(pageIdx + 1)">下一页</el-button>
    </div>
  </el-card>
</template>

<style scoped>
.panel-head {
  padding: var(--soj-sp-16) var(--soj-sp-20);
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
}
</style>

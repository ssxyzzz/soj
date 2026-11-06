<script setup lang="ts">
// 按题号导入（v0.73）：粘贴一串题号一键导入，统一选题器（ProblemPicker）的内嵌能力。
// 解析提取全部数字串（逗号/空格/换行/顿号/#1001 混排均可）去重保序，单次最多 100 题；
// 服务端回查摘要（problems/batch）：可见题出 id+title，缺失/隐藏/重复统一计为无效
// 「不存在或未公开」（与详情页无特权视角 404 口径一致）。合并去重在宿主（emit import）。
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { problemApi } from '@/api'

const emit = defineEmits<{ (e: 'import', items: { id: number; title: string }[]): void }>()

// 单次导入题号上限（与服务端 parsePositiveIDList 上限双闸一致）
const MAX_IDS = 100

const show = ref(false)
const raw = ref('')
const loading = ref(false)

// 解析：提取全部数字串 → 正整数 → 去重保序
const parsed = computed<number[]>(() => {
  const nums = (raw.value.match(/\d+/g) ?? []).map(Number).filter((n) => n > 0)
  return [...new Set(nums)]
})

const overLimit = computed(() => parsed.value.length > MAX_IDS)

async function confirm() {
  if (!parsed.value.length) {
    ElMessage.warning('没有解析到题号')
    return
  }
  if (overLimit.value) ElMessage.warning(`一次最多导入 ${MAX_IDS} 题，已截取前 ${MAX_IDS} 个`)
  loading.value = true
  try {
    const r = await problemApi.batch(parsed.value.slice(0, MAX_IDS))
    if (!r.items.length) {
      ElMessage.warning(`题号 ${r.invalid.join('、')} 均无效（不存在或未公开）`)
      return
    }
    if (r.invalid.length) ElMessage.warning(`题号 ${r.invalid.join('、')} 不存在或未公开，已忽略`)
    emit('import', r.items)
    raw.value = ''
    show.value = false
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <span class="import-wrap">
    <el-button size="small" @click="show = true">按题号导入</el-button>
    <el-dialog v-model="show" width="520" append-to-body>
      <template #header>
        <TipLabel
          label="按题号导入题目"
          tip="粘贴或输入题号后一键导入：逗号 / 空格 / 换行 / 顿号等分隔均可（可带 # 号），按输入顺序并入「已选」尾部；同一题只保留一次；一次最多 100 题；不存在或未公开（隐藏/重复）的题号会提示并忽略"
        />
      </template>
      <el-input
        v-model="raw"
        type="textarea"
        :rows="5"
        placeholder="如：1001, 1002, 1005（逗号 / 空格 / 换行 / 顿号分隔，可带 # 号）"
      />
      <div class="parse-hint" :class="{ over: overLimit }">
        {{
          parsed.length
            ? `已解析 ${parsed.length} 个题号${overLimit ? `，超出 ${MAX_IDS} 题上限，将只导入前 ${MAX_IDS} 个` : ''}`
            : '支持逗号 / 空格 / 换行 / 顿号分隔，自动提取数字'
        }}
      </div>
      <template #footer>
        <el-button @click="show = false">取消</el-button>
        <el-button type="primary" :loading="loading" @click="confirm">导入</el-button>
      </template>
    </el-dialog>
  </span>
</template>

<style scoped>
.parse-hint {
  margin-top: var(--soj-sp-6);
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-small);
}

.parse-hint.over {
  color: var(--soj-warning);
}
</style>

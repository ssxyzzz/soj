<script setup lang="ts">
// AI 助教面板（v0.33-④）：三级提示（可跳级直购、解锁永久）+ 找 bug 入口提示。
// 挂在题目页右列与在线编程 workbench 侧栏；AI 未配置时整体隐藏（status.enabled=false）。
// 赛时封锁由服务端把关（403 带指引文案，拦截器 toast）。
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { aiTutorApi, jadeApi, type AiHintLevel } from '@/api'
import { useUserStore } from '@/stores/user'
import MdView from '@/components/MdView.vue'

const props = defineProps<{ pid: number }>()

const userStore = useUserStore()
const enabled = ref(false)
const levels = ref<AiHintLevel[]>([])
const debugPrice = ref(0)
const dailyLeft = ref(0)
const hintContent = ref<Record<number, string>>({})
const buying = ref(0)
const loading = ref(false)

const levelMeta = [
  { n: 1, label: '方向提示', desc: '考察什么、往哪想' },
  { n: 2, label: '算法选型', desc: '具体算法与关键性质' },
  { n: 3, label: '实现要点', desc: '步骤级拆解（不含代码）' },
]

async function load() {
  if (!userStore.token) return
  loading.value = true
  try {
    const r = await aiTutorApi.status(props.pid)
    enabled.value = r.enabled
    levels.value = r.levels ?? []
    debugPrice.value = r.debug_price ?? 0
    dailyLeft.value = r.daily_left ?? 0
    // 已解锁级带出内容
    for (const lv of levels.value) {
      if (lv.unlocked && lv.has_content && !hintContent.value[lv.level]) {
        aiTutorApi
          .getHint(props.pid, lv.level)
          .then((h) => (hintContent.value[lv.level] = h.hint.content))
          .catch(() => {})
      }
    }
  } catch {
    enabled.value = false
  } finally {
    loading.value = false
  }
}

async function buy(lv: AiHintLevel) {
  try {
    await ElMessageBox.confirm(
      lv.has_content
        ? `花 ${lv.price} 玉解锁「${levelMeta[lv.level - 1].label}」？解锁永久可见。`
        : `花 ${lv.price} 玉生成并解锁「${levelMeta[lv.level - 1].label}」？首购触发 AI 生成（稍等片刻），解锁永久可见。`,
      'AI 助教',
      { confirmButtonText: `${lv.price} 玉解锁`, cancelButtonText: '再想想', type: 'info' },
    )
  } catch {
    return
  }
  buying.value = lv.level
  try {
    const r = await aiTutorApi.buyHint(props.pid, lv.level)
    hintContent.value[lv.level] = r.hint.content
    lv.unlocked = true
    if (r.balance !== undefined) {
      userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
      jadeApi.mine().then((m) => userStore.setJade(m.balance, m.currency)).catch(() => {})
    }
    if (dailyLeft.value > 0) dailyLeft.value--
    ElMessage.success(r.charged ? `已解锁（花费 ${r.price} 玉）` : '已解锁（教师/满分免费）')
  } catch {
    // 拦截器统一提示（余额不足/每日上限/赛时封锁）
  } finally {
    buying.value = 0
  }
}

const canBuy = computed(() => userStore.token && dailyLeft.value > 0)

watch(() => props.pid, load)
onMounted(load)
defineExpose({ refresh: load })
</script>

<template>
  <div v-if="enabled" class="ai-tutor" v-loading="loading">
    <div class="ai-head">
      <span class="ai-badge">✦ AI 助教</span>
      <span class="dim ai-daily">今日剩余 {{ dailyLeft }} 次</span>
    </div>
    <div v-for="meta in levelMeta" :key="meta.n" class="ai-level">
      <div class="ai-level-h">
        <b>{{ meta.n }}. {{ meta.label }}</b>
        <span class="dim">{{ meta.desc }}</span>
        <el-button
          v-if="levels[meta.n - 1] && !levels[meta.n - 1].unlocked"
          size="small"
          type="primary"
          plain
          :disabled="!canBuy"
          :loading="buying === meta.n"
          @click="buy(levels[meta.n - 1])"
        >
          {{ levels[meta.n - 1].price }} 玉解锁
        </el-button>
        <el-tag v-else-if="levels[meta.n - 1]" type="success" size="small" effect="plain">已解锁</el-tag>
      </div>
      <MdView v-if="hintContent[meta.n]" :content="hintContent[meta.n]" variant="compact" auto-height class="ai-hint-body" />
    </div>
    <p class="dim ai-foot">
      找 bug（{{ debugPrice }} 玉起，按难度计价）：在「我的提交」未通过记录上点「AI 找 bug」。
    </p>
  </div>
</template>

<style scoped>
.ai-tutor {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ai-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ai-badge {
  font-weight: 600;
  color: #6a5acd;
}

.ai-daily {
  font-size: 12px;
}

.ai-level {
  border: 1px dashed var(--soj-line);
  border-radius: 8px;
  padding: 10px 12px;
}

.ai-level-h {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.ai-level-h .dim {
  font-size: 12px;
  flex: 1;
  min-width: 0;
}

.ai-hint-body {
  margin-top: 8px;
  font-size: 13px;
}

.ai-foot {
  font-size: 12px;
  margin: 0;
}
</style>

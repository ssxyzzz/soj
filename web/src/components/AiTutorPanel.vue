<script setup lang="ts">
// AI 助教面板（v0.33-④，验收调整后两级）：①理解题目 ②算法思路（可跳级直购、解锁
// 永久）。挂在题目页「答案解析」卡内（视频/文字题解入口上方）；AI 未配置整体隐藏，
// 赛时封锁由服务端把关（403 带指引文案，拦截器 toast）。名字后台可配（默认「小玉」）。
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { aiTutorApi, jadeApi, type AiHintLevel } from '@/api'
import { useUserStore } from '@/stores/user'
import MdView from '@/components/MdView.vue'

const props = defineProps<{ pid: number }>()

const emit = defineEmits<{ (e: 'enabled', v: boolean): void }>()

const userStore = useUserStore()
const enabled = ref(false)
const tutorName = ref('小玉')
const levels = ref<AiHintLevel[]>([])
const debugPrice = ref(0)
const dailyLeft = ref(0)
const hintContent = ref<Record<number, string>>({})
const buying = ref(0)
const loading = ref(false)

const levelMeta = [
  { n: 1, label: '理解题目', desc: '输入输出、约束与易忽略细节' },
  { n: 2, label: '算法思路', desc: '算法选型、关键步骤与易错点' },
]

async function load() {
  if (!userStore.token) return
  loading.value = true
  try {
    const r = await aiTutorApi.status(props.pid)
    enabled.value = r.enabled
    emit('enabled', r.enabled)
    tutorName.value = r.name ?? '小玉'
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
    emit('enabled', false)
  } finally {
    loading.value = false
  }
}

async function buy(lv: AiHintLevel) {
  try {
    await ElMessageBox.confirm(
      lv.has_content
        ? `花 ${lv.price} 玉解锁「${levelMeta[lv.level - 1].label}」？解锁永久可见。`
        : `花 ${lv.price} 玉让 ${tutorName.value} 生成「${levelMeta[lv.level - 1].label}」？解锁永久可见。`,
      tutorName.value,
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
      <span class="ai-badge">✦ {{ tutorName }}（AI 助教）</span>
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
      找 bug（{{ debugPrice }} 玉起，按难度计价）：在「我的提交」里点开未通过的提交，用「{{ tutorName }} 找 bug」。
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

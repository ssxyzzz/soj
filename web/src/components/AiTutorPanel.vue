<script setup lang="ts">
// AI 助教面板（v0.33-④，2026-10-30 弹窗化）：卡内两个按钮不再就地展开——点击打开
// 结果弹窗（动作在弹窗右上角：重新生成 / 关闭）。未解锁先扣玉确认（免费层免确认）；
// 已解锁查看免费；「重新生成」每次计费、绕过共享缓存重算一份自己的（个人档覆盖）。
// 挂在题目页「答案解析」卡内；AI 未配置整体隐藏。名字后台可配（默认「小玉」）。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Lock, Unlock } from '@element-plus/icons-vue'

import { aiTutorApi, jadeApi, type AiHintLevel } from '@/api'
import { useUserStore } from '@/stores/user'
import MdView from '@/components/MdView.vue'

const props = defineProps<{ pid: number }>()

const emit = defineEmits<{ (e: 'enabled', v: boolean): void }>()

const userStore = useUserStore()
const enabled = ref(false)
const free = ref(false) // root/特权层：默认已解锁、免确认、不限次（后端 0 扣费）
const tutorName = ref('小玉')
const levels = ref<AiHintLevel[]>([])
const dailyLeft = ref(0)
const hintContent = ref<Record<number, string>>({})
const hintHtml = ref<Record<number, string>>({}) // 写入时渲染缓存（v0.53）：MdView 直渲
const buying = ref(0)
const loading = ref(false)

// label 用于确认弹窗文案；action 用于按钮文字（2026-10-30 拍板：理解题目 / 整理思路）
const levelMeta = [
  { n: 1, action: '理解题目', label: '理解题目' },
  { n: 2, action: '整理思路', label: '算法思路' },
]

// 结果弹窗（动作在右上角，拍板 C）
const dlg = reactive({ show: false, level: 1, label: '', content: '', html: '', loading: false })

async function load() {
  if (!userStore.token) return
  loading.value = true
  try {
    const r = await aiTutorApi.status(props.pid)
    enabled.value = r.enabled
    emit('enabled', r.enabled)
    free.value = r.free ?? false
    tutorName.value = r.name ?? '小玉'
    levels.value = r.levels ?? []
    dailyLeft.value = r.daily_left ?? 0
    // 已解锁级预取内容（弹窗秒开）
    for (const lv of levels.value) {
      if (lv.unlocked && lv.has_content && !hintContent.value[lv.level]) {
        aiTutorApi
          .getHint(props.pid, lv.level)
          .then((h) => {
            hintContent.value[lv.level] = h.hint.content
            hintHtml.value[lv.level] = h.hint.content_html
          })
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

async function confirmCharge(text: string, price: number) {
  if (free.value) return // 免费层免确认（2026-10-30）
  try {
    await ElMessageBox.confirm(text, tutorName.value, {
      confirmButtonText: `${price} 玉解锁`,
      cancelButtonText: '再想想',
      type: 'info',
    })
  } catch {
    throw new Error('cancelled')
  }
}

// 生成 / 重新生成（计费口径后端定）：成功后写个人缓存并刷新弹窗
async function genLevel(n: number) {
  buying.value = n
  try {
    const r = await aiTutorApi.buyHint(props.pid, n)
    hintContent.value[n] = r.hint.content
    hintHtml.value[n] = r.hint.content_html
    const lv = levels.value[n - 1]
    if (lv) {
      lv.unlocked = true
      lv.has_content = true
    }
    if (r.balance !== undefined) {
      userStore.setJade(r.balance, userStore.jadeCurrency || '玉')
      jadeApi.mine().then((m) => userStore.setJade(m.balance, m.currency)).catch(() => {})
    }
    if (dailyLeft.value > 0) dailyLeft.value--
    if (dlg.show && dlg.level === n) {
      dlg.content = r.hint.content
      dlg.html = r.hint.content_html
    }
    ElMessage.success(r.charged ? `已生成（花费 ${r.price} 玉）` : '已生成（教师/满分免费）')
  } catch {
    // 拦截器统一提示（余额不足/每日上限/赛时封锁/生成失败已退费）
  } finally {
    buying.value = 0
  }
}

// 未解锁：扣玉确认后生成并打开弹窗
async function buy(lv: AiHintLevel) {
  try {
    await confirmCharge(
      `花 ${lv.price} 玉让 ${tutorName.value} 生成「${levelMeta[lv.level - 1].label}」？解锁永久可见。`,
      lv.price,
    )
  } catch {
    return
  }
  dlg.level = lv.level
  dlg.label = levelMeta[lv.level - 1].label
  dlg.content = ''
  dlg.html = ''
  dlg.show = true
  await genLevel(lv.level)
}

// 打开结果弹窗：缓存命中秒开，否则拉取（后端个人档优先、存量共享缓存兜底）
async function openDlg(n: number) {
  dlg.level = n
  dlg.label = levelMeta[n - 1].label
  dlg.show = true
  if (hintHtml.value[n]) {
    dlg.content = hintContent.value[n]
    dlg.html = hintHtml.value[n]
    return
  }
  dlg.content = ''
  dlg.html = ''
  dlg.loading = true
  try {
    const r = await aiTutorApi.getHint(props.pid, n)
    if (r.hint.content) {
      hintContent.value[n] = r.hint.content
      hintHtml.value[n] = r.hint.content_html
      if (dlg.show && dlg.level === n) {
        dlg.content = r.hint.content
        dlg.html = r.hint.content_html
      }
    }
  } catch {
    // 拦截器统一提示
  } finally {
    dlg.loading = false
  }
}

// 弹窗内「重新生成」：学生每次扣玉确认，每点一次都重新生成（拍板）
async function onRegen() {
  const lv = levels.value[dlg.level - 1]
  if (!lv || buying.value) return
  try {
    await confirmCharge(`花 ${lv.price} 玉重新生成「${dlg.label}」？将替换当前内容。`, lv.price)
  } catch {
    return
  }
  await genLevel(dlg.level)
}

// 按钮点击：已解锁 → 打开弹窗（查看免费）；未解锁 → 扣玉流程
function onLevel(n: number) {
  const lv = levels.value[n - 1]
  if (!lv) return
  if (lv.unlocked) {
    openDlg(n)
  } else {
    buy(lv)
  }
}

const regenLabel = computed(() => {
  if (free.value) return '重新生成'
  const lv = levels.value[dlg.level - 1]
  return lv ? `重新生成（${lv.price} 玉）` : '重新生成'
})

// dailyLeft -1 = 不限次（root / 后台特权层配 0），按钮不禁用
const canBuy = computed(() => userStore.token && dailyLeft.value !== 0)

watch(() => props.pid, load)
onMounted(load)
defineExpose({ refresh: load })
</script>

<template>
  <div v-if="enabled" class="ai-tutor" v-loading="loading">
    <div class="ai-head">
      <span class="ai-head-l">
        <span class="ai-badge">✦ {{ tutorName }}（AI 助教）</span>
        <TipLabel tip="「AI 找 bug」入口在「我的提交」里点开未通过的提交" />
      </span>
      <!-- daily_left -1 = 不限次（root / 后台配 0），2026-10-30 分层口径 -->
      <span class="dim ai-daily">{{ dailyLeft < 0 ? '不限次' : `今日剩余 ${dailyLeft} 次` }}</span>
    </div>
    <div v-for="meta in levelMeta" :key="meta.n" class="ai-level-block">
      <el-button
        class="ai-btn"
        :title="levels[meta.n - 1] && !levels[meta.n - 1].unlocked ? `${levels[meta.n - 1].price} 玉解锁` : ''"
        :disabled="!levels[meta.n - 1] || (!levels[meta.n - 1].unlocked && !canBuy)"
        :loading="buying === meta.n && !dlg.show"
        @click="onLevel(meta.n)"
      >
        <span class="ai-btn-label">{{ tutorName }}帮你{{ meta.action }}</span>
        <el-icon v-if="levels[meta.n - 1]?.unlocked" class="ai-btn-state ok" :size="16"><Unlock /></el-icon>
        <el-icon v-else-if="levels[meta.n - 1]" class="ai-btn-state" :size="16"><Lock /></el-icon>
      </el-button>
    </div>

    <!-- 结果弹窗（2026-10-30 拍板）：内容不铺在卡内；查看免费、重新生成每次计费；
         动作在右上角（重新生成 / 关闭） -->
    <el-dialog v-model="dlg.show" :show-close="false" width="960" top="6vh">
      <template #header>
        <div class="dlg-head">
          <span class="dlg-title">✦ {{ tutorName }} · {{ dlg.label }}</span>
          <span class="dlg-acts">
            <el-button size="small" type="primary" :loading="buying === dlg.level" @click="onRegen">
              {{ regenLabel }}
            </el-button>
            <el-button size="small" @click="dlg.show = false">关闭</el-button>
          </span>
        </div>
      </template>
      <div v-loading="dlg.loading" class="dlg-body">
        <MdView v-if="dlg.html" :html="dlg.html" variant="compact" />
        <p v-else class="dim dlg-empty">
          还没有生成过——点击右上角「{{ regenLabel }}」让 {{ tutorName }} 生成一份。
        </p>
      </div>
    </el-dialog>
  </div>
</template>

<style scoped>
.ai-tutor {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-12);
}

/* 头行（徽章 + 今日剩余）：细线画法对齐卡头（提交本题/题目维护那种）——
   左右负边距贴到卡缘、线色 EP 卡头同款；上提 12px 让徽章紧贴 kicker
   （对齐卡头 kicker→标题 2px 间距，2026-10-30 反馈压高度） */
.ai-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: -12px -20px 0;
  padding: 0 var(--soj-sp-20) var(--soj-sp-4);
  border-bottom: 1px solid var(--el-card-border-color, var(--el-border-color-light));
}

.ai-head-l {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-4);
}

.ai-badge {
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-d6); /* AI 徽标紫归并难度轴紫档（v0.57，原野生石板紫 #6a5acd） */
}

.ai-daily {
  font-size: var(--soj-fs-12);
}

/* 两级大胶囊钮（2026-10-30 草案）：白底细边、藏蓝粗字居中，状态图标浮右 */
.ai-level-block {
  display: flex;
  flex-direction: column;
}

.ai-btn {
  position: relative;
  width: 100%;
  height: 46px;
  border-radius: 999px;
  color: var(--soj-navy);
  --el-button-bg-color: var(--el-fill-color-blank);
  --el-button-border-color: var(--soj-line);
  --el-button-hover-text-color: var(--soj-navy);
  --el-button-hover-bg-color: var(--el-fill-color-blank);
  --el-button-hover-border-color: var(--soj-blue-line, var(--soj-line));
}

.ai-btn .ai-btn-label {
  flex: 1;
  text-align: center;
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-bold);
}

.ai-btn .ai-btn-state {
  position: absolute;
  right: 18px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--soj-ink-3);
}

.ai-btn .ai-btn-state.ok {
  color: var(--el-color-success);
}

/* 结果弹窗头：标题居左、动作（重新生成/关闭）居右上角（拍板 C） */
.dlg-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-12);
}

.dlg-title {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
}

.dlg-acts {
  display: inline-flex;
  gap: var(--soj-sp-8);
}

.dlg-body {
  min-height: 160px;
  max-height: 64vh;
  overflow: auto;
}

.dlg-empty {
  margin: 0;
  text-align: center;
  padding: var(--soj-sp-32) 0;
}
</style>

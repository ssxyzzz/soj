<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { usePolling } from '@/composables/usePolling'
import { RouterLink, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { contestApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import ProblemPicker from '@/components/ProblemPicker.vue'
import SwitchRow from '@/components/SwitchRow.vue'
import { useUserStore } from '@/stores/user'
import { fmtShort } from '@/utils/format'
import type { ContestListItem, PickedProblem } from '@/types'

const router = useRouter()
const userStore = useUserStore()

const list = ref<ContestListItem[]>([])
const loading = ref(false)
const showCreate = ref(false)
const creating = ref(false)
const form = ref({
  title: '',
  intro: '',
  range: [] as string[],
  hiddenDays: 0,
  picked: [] as PickedProblem[],
  fileIO: false,
  scoreboardOff: true, // 「关闭成绩表」开关（默认开 = 成绩表关闭）；提交时反转为 scoreboard
  hideDuring: true,
  hideMeta: true,
})

const running = computed(() => list.value.filter((c) => c.status === 'running'))
const scheduled = computed(() => list.value.filter((c) => c.status === 'scheduled'))

// v0.19 三期（v3-full 比赛页）：seg 分段过滤 + 横条卡片行（替换双表格）
const seg = ref<'all' | 'running' | 'scheduled' | 'ended'>('all')
const segLabel: Record<typeof seg.value, string> = {
  all: '全部',
  running: '进行中',
  scheduled: '即将开始',
  ended: '已结束',
}
const filtered = computed(() =>
  seg.value === 'all' ? list.value : list.value.filter((c) => c.status === seg.value),
)

const contestLabel: Record<ContestListItem['status'], string> = {
  scheduled: '报名中',
  running: '进行中',
  ended: '已结束',
}

// v3 副列「我的赛程 · 下一场」：最近一场未开赛比赛的倒计时（每秒刷新；无待开赛事不渲染卡）
// usePolling（v0.29）：后台标签页自动暂停、卸载自清
const now = ref(Date.now())
const clock = usePolling(() => {
  now.value = Date.now()
}, 1000)
const nextContest = computed(() =>
  [...scheduled.value].sort((a, b) => +new Date(a.start) - +new Date(b.start))[0] ?? null,
)
const countdown = computed(() => {
  const c = nextContest.value
  if (!c) return ''
  const ms = +new Date(c.start) - now.value
  if (ms <= 0) return '即将开赛'
  const sec = Math.floor(ms / 1000)
  const days = Math.floor(sec / 86400)
  const hh = String(Math.floor((sec % 86400) / 3600)).padStart(2, '0')
  const mm = String(Math.floor((sec % 3600) / 60)).padStart(2, '0')
  const ss = String(sec % 60).padStart(2, '0')
  return days > 0 ? `${days} 天 ${hh} : ${mm} : ${ss}` : `${hh} : ${mm} : ${ss}`
})
onMounted(() => {
  load()
  clock.start()
})


// 行右时间：running 显示结束时间、scheduled 显示开赛时间、ended 显示结束时间
function whenLine(c: ContestListItem): { main: string; sub: string } {
  if (c.status === 'running') return { main: `${fmtShort(c.end)} 结束`, sub: '进行中' }
  if (c.status === 'scheduled') return { main: `${fmtShort(c.start)} 开赛`, sub: '待开赛' }
  return { main: `${fmtShort(c.end)} 结束`, sub: '已结束' }
}

async function load() {
  loading.value = true
  try {
    list.value = await contestApi.list()
  } finally {
    loading.value = false
  }
}

async function create() {
  if (!form.value.title.trim() || form.value.range.length !== 2) {
    ElMessage.warning('请填写比赛名称与起止时间')
    return
  }
  const problemIds = form.value.picked.map((p) => p.id)
  if (!problemIds.length) {
    ElMessage.warning('请从选题器选择至少一道题目')
    return
  }
  creating.value = true
  try {
    const r = await contestApi.create({
      title: form.value.title.trim(),
      intro: form.value.intro.trim(),
      start: new Date(form.value.range[0]).toISOString(),
      end: new Date(form.value.range[1]).toISOString(),
      hidden_days: form.value.hiddenDays,
      problem_ids: problemIds,
      file_io: form.value.fileIO,
      scoreboard: !form.value.scoreboardOff,
      hide_during: form.value.hideDuring,
      hide_meta: form.value.hideMeta,
    })
    ElMessage.success('比赛创建成功')
    showCreate.value = false
    router.push(`/contests/${r.id}`)
  } finally {
    creating.value = false
  }
}

function openContest(c: ContestListItem) {
  router.push(`/contests/${c.id}`)
}

</script>

<template>
  <PageShell>
      <div class="page-head">
        <div class="kicker">CONTESTS</div>
        <h1 class="page-title">比赛<small>{{ running.length }} 场进行中 · {{ scheduled.length }} 场待开赛</small></h1>
      </div>

      <!-- v3-full：全局 .soj-seg 分段 + 横条卡片行 -->
      <div class="soj-seg page-seg">
        <button
          v-for="(label, key) in segLabel"
          :key="key"
          :class="{ on: seg === key }"
          @click="seg = key as typeof seg"
        >
          {{ label }}
        </button>
      </div>

      <div v-loading="loading">
        <el-empty v-if="!filtered.length" description="这里还没有比赛" :image-size="60" />
        <div v-else class="crowd">
          <div v-for="c in filtered" :key="c.id" class="card crow" @click="openContest(c)">
            <span class="mini-tag" :class="c.status">{{ contestLabel[c.status] }}</span>
            <div class="grow">
              <h3><RouterLink :to="`/contests/${c.id}`" class="clink" @click.stop>{{ c.title }}</RouterLink></h3>
              <div class="meta">{{ fmtShort(c.start) }} ~ {{ fmtShort(c.end) }} · {{ c.problem_count }} 题</div>
            </div>
            <div class="when">
              <b>{{ whenLine(c).main }}</b>
              <span>{{ whenLine(c).sub }}</span>
            </div>
            <el-button
              :type="c.status === 'running' ? 'primary' : 'default'"
              style="flex: none"
              @click.stop="openContest(c)"
            >
              {{ c.status === 'ended' ? '查看榜单' : c.status === 'scheduled' ? '查看详情' : '进入赛场' }}
            </el-button>
          </div>
        </div>
      </div>

    <template #aside>
      <RailCard v-if="userStore.can('contest.bank.create')" ov="管理" class="aside-card">
        <template #head>
          <div class="rail-ov">管理</div>
          <div class="rail-t"><TipLabel label="赛事管理" tip="从题库挑选赛题成场；创建后可在详情页调整题目与各项设置" /></div>
        </template>
        <el-button type="primary" style="width: 100%" @click="showCreate = true">新建比赛</el-button>
      </RailCard>

      <RailCard v-if="nextContest" ov="我的赛程" :title="`下一场 · ${nextContest.title}`" class="aside-card">
        <div class="soj-countdown">{{ countdown }}</div>
        <p class="hint center">{{ fmtShort(nextContest.start) }} 开赛</p>
      </RailCard>

      <RailCard ov="说明" title="赛制与锁题" class="aside-card">
        <div class="dim rule-text">
          <p>· OI 赛制：赛时正常评测但不出分，只记录提交次数；</p>
          <p>· 每题满分 100，编程题按测试点 / 子任务计分，客观题按题给分；</p>
          <p>· 赛后统一出榜，按总分排名；</p>
          <p>· 比赛期间锁题：请进入比赛详情页提交。</p>
        </div>
      </RailCard>
    </template>
  </PageShell>

  <el-dialog v-model="showCreate" title="创建主题库比赛" width="960" top="5vh">
    <el-form label-position="top">
      <el-form-item label="比赛名称" required>
        <el-input v-model="form.title" maxlength="64" />
      </el-form-item>
      <el-form-item label="简介">
        <el-input v-model="form.intro" type="textarea" :rows="2" />
      </el-form-item>
      <el-form-item label="起止时间（最长 7 天）" required>
        <el-date-picker
          v-model="form.range"
          type="datetimerange"
          start-placeholder="开始"
          end-placeholder="结束"
          style="width: 100%"
        />
      </el-form-item>
      <el-form-item>
        <template #label>
          <TipLabel label="赛后隐藏期（天）" tip="0 为不隐藏；隐藏期内榜单与提交记录不公开，期满自动放开" />
        </template>
        <el-input-number v-model="form.hiddenDays" :min="0" :max="7" />
      </el-form-item>
      <el-form-item label="比赛题目" required>
        <ProblemPicker v-model="form.picked" />
      </el-form-item>
      <el-form-item>
        <SwitchRow
          v-model="form.fileIO"
          label="本场启用文件 IO"
          tip="场内所有题按各题文件名对以文件读写判题，题面下会展示文件 IO 说明；创建后可在比赛详情管理卡随时开关，改动只影响之后的提交"
        />
      </el-form-item>
      <el-form-item>
        <SwitchRow
          v-model="form.scoreboardOff"
          label="关闭成绩表"
          tip="默认开启：参与者看不到成绩表（组织者不受影响）；关闭此开关则本场向参与者开放成绩表"
        />
      </el-form-item>
      <el-form-item>
        <SwitchRow
          v-model="form.hideDuring"
          label="赛时隐藏成绩"
          tip="隐藏状态/标签/通过率，赛中只显示「已提交」；关闭后赛中即可查看结果"
        />
      </el-form-item>
      <el-form-item>
        <SwitchRow
          v-model="form.hideMeta"
          label="赛时隐藏难度与知识点"
          tip="比赛期间对参赛者隐藏题目难度与知识点（服务端不下发，悬停不可见），题目表与做题页显示横杠；发布后恢复显示，组织者与管理员不受影响"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showCreate = false">取消</el-button>
      <el-button type="primary" :loading="creating" @click="create">创建</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
/* 全局 .soj-seg 已接管形态，这里只补页面下边距 */
.page-seg {
  margin-bottom: var(--soj-sp-16);
}

/* v3 横条卡片行 */
.crowd {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-12);
}

.crow {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-16);
  padding: var(--soj-sp-16) var(--soj-sp-20);
  cursor: pointer;
  transition:
    transform 0.18s,
    box-shadow 0.18s;
}

.crow:hover {
  transform: translateY(-2px);
  box-shadow: var(--soj-shadow-card-hover);
}

.crow h3 {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-navy);
  letter-spacing: -0.2px;
}

.crow .meta {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  margin-top: var(--soj-sp-4);
}

.crow .grow {
  flex: 1;
  min-width: 0;
}

.crow .when {
  text-align: right;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  flex: none;
}

.crow .when b {
  display: block;
  font-size: var(--soj-fs-14);
  color: var(--soj-navy);
  font-weight: var(--soj-fw-bold);
}

/* 迷你状态点标（v3 mini-tag）：圆点 + 文字浅胶囊 */
.mini-tag {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-6);
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  padding: var(--soj-sp-4) var(--soj-sp-12);
  border-radius: 999px;
  white-space: nowrap;
  flex: none;
}

.mini-tag::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  flex: none;
}

.mini-tag.running {
  background: var(--soj-run-bg);
  color: var(--soj-run);
}

.mini-tag.scheduled {
  background: var(--soj-tle-bg);
  color: var(--soj-tle);
}

.mini-tag.ended {
  background: var(--soj-mut-bg);
  color: var(--soj-mut);
}

.hint {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-3);
  line-height: 1.7;
  margin: var(--soj-sp-10) 0 0;
}

.hint.center {
  text-align: center;
  margin-top: var(--soj-sp-8);
}

.rule-text p {
  margin: 0 0 var(--soj-sp-6);
  line-height: 1.6;
}

@media (max-width: 768px) {
  .crow {
    flex-wrap: wrap;
  }

  .crow .when {
    text-align: left;
    width: 100%;
  }
}
</style>

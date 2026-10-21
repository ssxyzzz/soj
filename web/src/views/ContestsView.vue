<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { contestApi } from '@/api'
import PageShell from '@/components/PageShell.vue'
import ProblemPicker from '@/components/ProblemPicker.vue'
import SwitchRow from '@/components/SwitchRow.vue'
import { useUserStore } from '@/stores/user'
import { fmtDate } from '@/utils/format'
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
  scoreboard: true,
  hideDuring: true,
})

const running = computed(() => list.value.filter((c) => c.status === 'running'))
const ended = computed(() => list.value.filter((c) => c.status === 'ended'))

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
      scoreboard: form.value.scoreboard,
      hide_during: form.value.hideDuring,
    })
    ElMessage.success('比赛创建成功')
    showCreate.value = false
    router.push(`/contests/${r.id}`)
  } finally {
    creating.value = false
  }
}

function timeRange(c: ContestListItem): string {
  return `${fmtDate(c.start)} ~ ${fmtDate(c.end)}`
}

function openContest(c: ContestListItem) {
  router.push(`/contests/${c.id}`)
}

onMounted(load)
</script>

<template>
  <PageShell>
      <el-card v-loading="loading" class="group">
        <template #header>
          <div class="group-head running-head">
            <span class="dot running-dot" /> 进行中的比赛
          </div>
        </template>
        <el-empty v-if="!running.length" description="当前没有进行中的比赛" :image-size="50" />
        <el-table stripe v-else :data="running" style="cursor: pointer" @row-click="openContest">
          <el-table-column prop="title" label="比赛" min-width="220" />
          <el-table-column label="时间" min-width="280">
            <template #default="{ row }">{{ timeRange(row as ContestListItem) }}</template>
          </el-table-column>
          <el-table-column prop="problem_count" label="题数" width="80" />
        </el-table>
      </el-card>

      <el-card v-loading="loading" class="group">
        <template #header>
          <div class="group-head"><span class="dot ended-dot" /> 已结束的比赛</div>
        </template>
        <el-empty v-if="!ended.length" description="还没有已结束的比赛" :image-size="50" />
        <el-table stripe v-else :data="ended" style="cursor: pointer" @row-click="openContest">
          <el-table-column prop="title" label="比赛" min-width="220" />
          <el-table-column label="时间" min-width="280">
            <template #default="{ row }">{{ timeRange(row as ContestListItem) }}</template>
          </el-table-column>
          <el-table-column prop="problem_count" label="题数" width="80" />
        </el-table>
      </el-card>

    <template #aside>
      <el-card v-if="userStore.can('contest.bank.create')" class="aside-card">
        <template #header><span>管理</span></template>
        <el-button type="primary" style="width: 100%" @click="showCreate = true">创建主题库比赛</el-button>
      </el-card>

      <el-card class="aside-card">
        <template #header><span>赛制说明</span></template>
        <div class="dim rule-text">
          <p>· OI 赛制：赛时正常评测但不出分，只记录提交次数；</p>
          <p>· 每题满分 100，按测试点 / 子任务计分；</p>
          <p>· 赛后统一出榜，按总分排名；</p>
          <p>· 比赛期间锁题：请从比赛页提交。</p>
        </div>
      </el-card>
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
      <el-form-item label="赛后隐藏期（天，0 为不隐藏；隐藏期内榜单与提交记录不公开）">
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
          v-model="form.scoreboard"
          label="开启成绩表"
          tip="关闭后参与者看不到成绩表，组织者仍可见"
        />
      </el-form-item>
      <el-form-item>
        <SwitchRow
          v-model="form.hideDuring"
          label="赛时隐藏成绩"
          tip="隐藏状态/标签/通过率，赛中只显示「已提交」；关闭后赛中即可查看结果"
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
.group {
  margin-bottom: 16px;
}

.group-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: var(--soj-fw-emphasis);
}

.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  display: inline-block;
}

.running-dot {
  background: var(--el-color-success);
}

.ended-dot {
  background: var(--el-text-color-placeholder);
}

.running-head {
  color: var(--el-color-success-dark-2);
}

.rule-text p {
  margin: 0 0 6px;
  line-height: 1.6;
}
</style>

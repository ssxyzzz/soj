<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { contestApi, problemApi } from '@/api'
import { fmtMem, statusStyle } from '@/utils/format'
import MdView from '@/components/MdView.vue'
import type { ContestDetail, ContestMySubmission, ProblemDetail } from '@/types'

const route = useRoute()
const router = useRouter()

const contest = ref<ContestDetail | null>(null)
const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
const language = ref('cpp')
const code = ref('')
const submitting = ref(false)
const mine = ref<ContestMySubmission[]>([])
const revealed = ref(false)

const languageOptions = [
  { value: 'cpp', label: 'C++ 14' },
  { value: 'c', label: 'C' },
  { value: 'python3', label: 'Python 3' },
]

const codeTemplates: Record<string, string> = {
  cpp: '#include <iostream>\n\nint main() {\n    std::ios::sync_with_stdio(false);\n    int a, b;\n    std::cin >> a >> b;\n    std::cout << a + b << std::endl;\n    return 0;\n}\n',
  c: '#include <stdio.h>\n\nint main(void) {\n    int a, b;\n    scanf("%d %d", &a, &b);\n    printf("%d\\n", a + b);\n    return 0;\n}\n',
  python3: 'a, b = map(int, input().split())\nprint(a + b)\n',
}

async function load() {
  loading.value = true
  try {
    const contestId = Number(route.params.id)
    contest.value = await contestApi.detail(contestId)
    problem.value = await problemApi.detail(Number(route.params.pid))
    await loadMine()
  } catch {
    router.replace(`/contests/${route.params.id}`)
  } finally {
    loading.value = false
  }
}

async function loadMine() {
  try {
    const r = await contestApi.mySubmissions(Number(route.params.id))
    mine.value = r.list.filter((s) => s.pid === Number(route.params.pid))
    revealed.value = r.revealed
  } catch {
    mine.value = []
  }
}

async function submit() {
  if (!code.value.trim()) {
    ElMessage.warning('代码不能为空')
    return
  }
  submitting.value = true
  try {
    const r = await contestApi.submit(
      Number(route.params.id),
      Number(route.params.pid),
      language.value,
      code.value,
    )
    ElMessage.success('已提交')
    await loadMine()
    // 赛时不轮询结果（不出分）；已公开时轮询刷新
    if (revealed.value) {
      pollRevealed(r.id)
    }
  } finally {
    submitting.value = false
  }
}

let pollTimer: ReturnType<typeof setInterval> | undefined
function pollRevealed(sid: number) {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = setInterval(async () => {
    await loadMine()
    const s = mine.value.find((x) => x.id === sid)
    if (s && s.status && s.status !== 'Pending' && s.status !== 'Judging') {
      if (pollTimer) clearInterval(pollTimer)
    }
  }, 1500)
}

onMounted(() => {
  code.value = codeTemplates.cpp
  load()
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<template>
  <div v-loading="loading" class="page">
    <template v-if="contest && problem">
      <el-page-header class="mb8" @back="router.push(`/contests/${contest.id}`)">
        <template #content>
          <span>{{ contest.title }} · #{{ problem.id }} {{ problem.title }}</span>
        </template>
      </el-page-header>

      <el-alert
        v-if="contest.status === 'running' && !revealed"
        title="OI 赛制进行中：正常评测但看不到得分与判定结果，只记录提交次数"
        type="warning"
        :closable="false"
      />

      <el-card class="block">
        <template #header>
          <div class="head">
            <span>题目信息</span>
            <span class="dim">{{ problem.time_limit_ms }} ms · {{ fmtMem(problem.memory_limit_kb) }}</span>
          </div>
        </template>
        <MdView :content="problem.description" />
        <el-table v-if="problem.samples.length" :data="problem.samples" class="mt8">
          <el-table-column label="输入">
            <template #default="{ row }"><pre class="mono">{{ row.input || '（空）' }}</pre></template>
          </el-table-column>
          <el-table-column label="输出">
            <template #default="{ row }"><pre class="mono">{{ row.output }}</pre></template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card class="block">
        <template #header>
          <div class="head">
            <span>提交代码</span>
            <span class="dim">已提交 {{ mine.length }} 次</span>
          </div>
        </template>
        <div class="submit-bar">
          <el-select v-model="language" style="width: 160px" @change="code = codeTemplates[language] ?? code">
            <el-option v-for="o in languageOptions" :key="o.value" :value="o.value" :label="o.label" />
          </el-select>
          <el-button type="primary" :disabled="contest.status !== 'running'" :loading="submitting" @click="submit">
            {{ contest.status === 'running' ? '提交' : '比赛未在进行中' }}
          </el-button>
        </div>
        <el-input
          v-model="code"
          type="textarea"
          :rows="12"
          class="mono"
          spellcheck="false"
        />
      </el-card>

      <el-card class="block">
        <template #header><span>我的提交（{{ revealed ? '已公开' : '赛时仅记录次数' }}）</span></template>
        <el-table :data="mine">
          <el-table-column prop="id" label="ID" width="90" />
          <template v-if="revealed">
            <el-table-column label="结果" width="130">
              <template #default="{ row }">
                <el-tag v-if="row.status" :type="statusStyle(row.status).type">
                  {{ statusStyle(row.status).label }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="score" label="得分" width="80" />
            <el-table-column label="耗时" width="90">
              <template #default="{ row }">{{ row.time_ms ?? '-' }} ms</template>
            </el-table-column>
          </template>
          <el-table-column label="提交时间">
            <template #default="{ row }">
              {{ new Date(row.created_at).toLocaleString('zh-CN', { hour12: false }) }}
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!mine.length" description="还没有提交" :image-size="60" />
      </el-card>
    </template>
  </div>
</template>

<style scoped>
.mb8 {
  margin-bottom: 8px;
}

.mt8 {
  margin-top: 8px;
}

.block {
  margin-top: 12px;
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.dim {
  color: #909399;
  font-size: 13px;
}

.description {
  white-space: pre-wrap;
  line-height: 1.8;
}

.submit-bar {
  display: flex;
  justify-content: space-between;
  margin-bottom: 12px;
}

.mono :deep(textarea) {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
  line-height: 1.5;
}
</style>

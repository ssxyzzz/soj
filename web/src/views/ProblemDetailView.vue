<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { problemApi, submissionApi } from '@/api'
import { useUserStore } from '@/stores/user'
import { fmtMem, statusStyle } from '@/utils/format'
import type { ProblemDetail, Submission } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
const language = ref('cpp')
const code = ref('')
const submitting = ref(false)
const latest = ref<Submission | null>(null)
let timer: ReturnType<typeof setInterval> | undefined

const languageOptions = [
  { value: 'cpp', label: 'C++ 17' },
  { value: 'c', label: 'C' },
  { value: 'python3', label: 'Python 3' },
]

const codeTemplates: Record<string, string> = {
  cpp: '#include <iostream>\n\nint main() {\n    std::ios::sync_with_stdio(false);\n    int a, b;\n    std::cin >> a >> b;\n    std::cout << a + b << std::endl;\n    return 0;\n}\n',
  c: '#include <stdio.h>\n\nint main(void) {\n    int a, b;\n    scanf("%d %d", &a, &b);\n    printf("%d\\n", a + b);\n    return 0;\n}\n',
  python3: 'a, b = map(int, input().split())\nprint(a + b)\n',
}

watch(language, (lang) => {
  code.value = codeTemplates[lang] ?? ''
})

async function load() {
  loading.value = true
  try {
    problem.value = await problemApi.detail(Number(route.params.id))
  } catch {
    router.replace('/problems')
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  if (!code.value.trim()) {
    ElMessage.warning('代码不能为空')
    return
  }
  submitting.value = true
  try {
    const r = await submissionApi.create(Number(route.params.id), language.value, code.value)
    ElMessage.success(`已提交 #${r.id}，正在判题`)
    startPolling(r.id)
  } catch {
    // 错误提示已由 axios 拦截器统一弹出
  } finally {
    submitting.value = false
  }
}

function startPolling(id: number) {
  stopPolling()
  latest.value = null
  timer = setInterval(async () => {
    try {
      const s = await submissionApi.get(id)
      latest.value = s
      if (s.status !== 'Pending' && s.status !== 'Judging') stopPolling()
    } catch {
      stopPolling()
    }
  }, 1200)
}

function stopPolling() {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

onMounted(() => {
  code.value = codeTemplates.cpp
  load()
})

onUnmounted(stopPolling)
</script>

<template>
  <div v-loading="loading" class="page">
    <template v-if="problem">
      <el-card>
        <template #header>
          <div class="head">
            <span class="title">#{{ problem.id }} {{ problem.title }}</span>
            <span class="limits">
              <el-tag size="small" type="info">{{ problem.time_limit_ms }} ms</el-tag>
              <el-tag size="small" type="info">{{ fmtMem(problem.memory_limit_kb) }}</el-tag>
            </span>
          </div>
        </template>
        <div class="description">{{ problem.description }}</div>
      </el-card>

      <el-card v-if="problem.samples.length" class="block">
        <template #header><span>样例</span></template>
        <el-table :data="problem.samples">
          <el-table-column label="输入" min-width="240">
            <template #default="{ row }">
              <pre class="mono">{{ row.input || '（空）' }}</pre>
            </template>
          </el-table-column>
          <el-table-column label="输出" min-width="240">
            <template #default="{ row }">
              <pre class="mono">{{ row.output }}</pre>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card class="block">
        <template #header><span>提交代码</span></template>
        <div class="submit-bar">
          <el-select v-model="language" style="width: 160px">
            <el-option
              v-for="opt in languageOptions"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label"
            />
          </el-select>
          <el-button type="primary" :loading="submitting" @click="submit">提交</el-button>
        </div>
        <el-input
          v-model="code"
          type="textarea"
          :rows="14"
          class="mono code-area"
          spellcheck="false"
          placeholder="选择语言后自动填入代码模板"
        />
      </el-card>

      <el-card v-if="latest" class="block">
        <template #header><span>判题结果（提交 #{{ latest.id }}）</span></template>
        <el-descriptions :column="4" border size="small">
          <el-descriptions-item label="状态">
            <el-tag :type="statusStyle(latest.status).type">
              {{ statusStyle(latest.status).label }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="得分">{{ latest.score }}</el-descriptions-item>
          <el-descriptions-item label="耗时">{{ latest.time_ms }} ms</el-descriptions-item>
          <el-descriptions-item label="内存">{{ fmtMem(latest.memory_kb) }}</el-descriptions-item>
        </el-descriptions>
        <pre v-if="latest.error_info" class="error mono">{{ latest.error_info }}</pre>
      </el-card>
    </template>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.title {
  font-weight: 600;
}

.limits {
  display: flex;
  gap: 8px;
}

.description {
  white-space: pre-wrap;
  line-height: 1.8;
}

.block {
  margin-top: 16px;
}

.submit-bar {
  display: flex;
  justify-content: space-between;
  margin-bottom: 12px;
}

.code-area :deep(textarea) {
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace;
  line-height: 1.5;
}

.error {
  margin-top: 12px;
  color: #f56c6c;
  background: #fef0f0;
  padding: 12px;
  border-radius: 4px;
}
</style>

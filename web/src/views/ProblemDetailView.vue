<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { problemApi, submissionApi } from '@/api'
import { useUserStore } from '@/stores/user'
import { fmtDate, fmtMem, statusStyle } from '@/utils/format'
import { MAX_MARKDOWN_BYTES, markdownBytes } from '@/utils/vditor'
import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
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

// 我的提交 / 他人代码（v0.4.3 代码可见性）
const mySubs = ref<Submission[]>([])
const others = ref<Submission[]>([])
const othersTotal = ref(0)
const othersPage = ref(1)
const expandedRows = ref<number[]>([])
const showEditStatement = ref(false)
const editStatement = ref('')
const savingStatement = ref(false)

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

const lockedRunning = computed(() => !!problem.value?.locked)

const lockRoute = computed(() => {
  const p = problem.value
  if (!p) return ''
  return p.lock_kind === 'exam' && p.exam_id ? `/exams/${p.exam_id}` : `/contests/${p.contest_id}`
})
const lockWhat = computed(() => (problem.value?.lock_kind === 'exam' ? '考试' : '比赛'))
const canSubmit = computed(() => !!userStore.token && !lockedRunning.value)
const canSetHidden = computed(() => !!problem.value?.can_set_hidden)

watch(language, (lang) => {
  code.value = codeTemplates[lang] ?? code.value
})

async function load() {
  loading.value = true
  try {
    problem.value = await problemApi.detail(Number(route.params.id))
    await Promise.all([loadMine(), loadOthers()])
  } catch {
    router.replace('/problems')
  } finally {
    loading.value = false
  }
}

async function loadMine() {
  if (!userStore.token) return
  try {
    mySubs.value = (await problemApi.mySubmissions(Number(route.params.id), 1, 20)).list
  } catch {
    mySubs.value = []
  }
}

async function loadOthers(p = 1) {
  if (!problem.value?.can_view_others) return
  try {
    const r = await problemApi.submissions(Number(route.params.id), p, 20)
    others.value = r.list
    othersTotal.value = r.total
    othersPage.value = p
  } catch {
    others.value = []
  }
}

function openEditStatement() {
  editStatement.value = problem.value?.description ?? ''
  showEditStatement.value = true
}

async function saveStatement() {
  if (!problem.value) return
  if (!editStatement.value.trim()) {
    ElMessage.warning('题面不能为空')
    return
  }
  if (markdownBytes(editStatement.value) > MAX_MARKDOWN_BYTES) {
    ElMessage.error('题面过长（上限 256KB）')
    return
  }
  savingStatement.value = true
  try {
    await problemApi.setStatement(problem.value.id, editStatement.value)
    problem.value.description = editStatement.value
    ElMessage.success('题面已保存')
    showEditStatement.value = false
  } finally {
    savingStatement.value = false
  }
}

async function toggleHidden() {
  if (!problem.value) return
  const target = !problem.value.hidden
  await problemApi.setHidden(problem.value.id, target)
  problem.value.hidden = target
  ElMessage.success(target ? '已设为主题库隐藏（default 不可见）' : '已取消隐藏')
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
    loadMine()
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
      <el-alert
        v-if="problem.locked"
        :title="`该题正在${lockWhat}中`"
        type="warning"
        :closable="false"
        class="mb8"
      >
        <template #default>
          <div class="lock-bar">
            <span>{{ lockWhat }}期间只能从{{ lockWhat }}页面提交（{{ problem.contest_title ?? problem.exam_title ?? '' }}）</span>
            <el-button type="warning" size="small" @click="router.push(lockRoute)">前往{{ lockWhat }}</el-button>
          </div>
        </template>
      </el-alert>

      <el-card>
        <template #header>
          <div class="head">
            <div class="title-wrap">
              <span class="title">#{{ problem.id }} {{ problem.title }}</span>
              <el-tag v-if="problem.hidden" size="small" type="danger" class="ml8">主题库隐藏</el-tag>
            </div>
            <div class="head-right">
              <el-button
                v-if="canSetHidden"
                size="small"
                :type="problem.hidden ? 'warning' : 'default'"
                @click="toggleHidden"
              >
                {{ problem.hidden ? '取消隐藏' : '设为隐藏' }}
              </el-button>
              <el-button
                v-if="problem.can_edit_statement"
                size="small"
                type="primary"
                plain
                @click="openEditStatement"
              >
                编辑题面
              </el-button>
              <el-tag size="small" type="info">{{ problem.time_limit_ms }} ms</el-tag>
              <el-tag size="small" type="info">{{ fmtMem(problem.memory_limit_kb) }}</el-tag>
            </div>
          </div>
        </template>
        <MdView :content="problem.description" />
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
            <el-option v-for="opt in languageOptions" :key="opt.value" :value="opt.value" :label="opt.label" />
          </el-select>
          <el-button type="primary" :disabled="!canSubmit" :loading="submitting" @click="submit">
            {{ lockedRunning ? `${lockWhat}中，请从${lockWhat}页提交` : '提交' }}
          </el-button>
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

      <el-card v-if="userStore.token && !lockedRunning" class="block">
        <template #header><span>我的提交（{{ mySubs.length }}）</span></template>
        <el-table :data="mySubs" size="small">
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column label="结果" width="130">
            <template #default="{ row }">
              <el-tag :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="得分" width="80">
            <template #default="{ row }">{{ row.score }}</template>
          </el-table-column>
          <el-table-column label="耗时" width="90">
            <template #default="{ row }">{{ row.time_ms }} ms</template>
          </el-table-column>
          <el-table-column prop="language" label="语言" width="90" />
          <el-table-column label="提交时间" width="180">
            <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
          </el-table-column>
          <el-table-column type="expand">
            <template #default="{ row }">
              <pre v-if="row.code" class="mono expand-code">{{ row.code }}</pre>
              <span v-else class="dim">（暂无代码）</span>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!mySubs.length" description="还没有提交过" :image-size="50" />
      </el-card>

      <el-card v-if="problem.can_view_others" class="block">
        <template #header>
          <div class="head">
            <span>他人代码（{{ othersTotal }}）——你已在该题拿到满分</span>
            <el-button size="small" @click="loadOthers(othersPage)">刷新</el-button>
          </div>
        </template>
        <el-table :data="others" size="small">
          <el-table-column type="expand">
            <template #default="{ row }">
              <pre v-if="row.code" class="mono expand-code">{{ row.code }}</pre>
            </template>
          </el-table-column>
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column prop="username" label="用户名" min-width="120" />
          <el-table-column label="结果" width="130">
            <template #default="{ row }">
              <el-tag :type="statusStyle(row.status).type">{{ statusStyle(row.status).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="得分" width="80">
            <template #default="{ row }">{{ row.score }}</template>
          </el-table-column>
          <el-table-column prop="language" label="语言" width="90" />
          <el-table-column label="提交时间" width="180">
            <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination
            background
            layout="prev, pager, next, total"
            :total="othersTotal"
            :page-size="20"
            :current-page="othersPage"
            @current-change="(p: number) => loadOthers(p)"
          />
        </div>
        <el-empty v-if="!others.length" description="还没有人提交过" :image-size="50" />
      </el-card>

      <el-dialog v-model="showEditStatement" title="编辑题面（Markdown / 所见即所得）" width="860" top="4vh">
        <MdEditor v-model="editStatement" height="480px" />
        <template #footer>
          <el-button @click="showEditStatement = false">取消</el-button>
          <el-button type="primary" :loading="savingStatement" @click="saveStatement">保存</el-button>
        </template>
      </el-dialog>

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
.mb8 {
  margin-bottom: 8px;
}

.lock-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.title-wrap {
  display: flex;
  align-items: center;
}

.head-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.title {
  font-weight: 600;
}

.ml8 {
  margin-left: 8px;
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

.expand-code {
  padding: 8px 12px;
  background: #f8f8f8;
  border-radius: 4px;
  max-height: 320px;
  overflow: auto;
}

.dim {
  color: #909399;
  font-size: 13px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
</style>

<script setup lang="ts">
// 编辑题目独立页（2026-10-18 去弹窗化，替代 EditProblemDialog）：与创建页共用 ProblemForm。
// zip 留空仅更新题面/信息，选新 zip 二次确认后整包替换；自定义校验器为即时上传/移除（不随保存提交）。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { UploadUserFile } from 'element-plus'

import { problemApi } from '@/api'
import ProblemForm from '@/components/ProblemForm.vue'
import type { ProblemFormPayload } from '@/components/ProblemForm.vue'
import type { ObjectiveKind, ProblemDetail } from '@/types'

const route = useRoute()
const router = useRouter()
const pid = Number(route.params.id)

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
const savingEdit = ref(false)
const editInitial = ref<{
  title?: string
  type?: 'default' | 'objective' | 'function' | 'interactive'
  difficulty?: number
  time_limit_ms?: number
  memory_limit_mb?: number
  compare?: string
  float_tolerance?: number
  statement?: string
  file_io_name?: string
  tag_ids?: number[]
  questions?: {
    stem: string
    kind: ObjectiveKind
    options: string[]
    answer: number[]
    blanks: { accept: string[]; case_sensitive?: boolean }[]
    explanation: string
    score: number
  }[]
}>({})
const fileIOHint = computed(() => ({
  input: problem.value?.file_io_input || 'problem.in',
  output: problem.value?.file_io_output || 'problem.out',
}))

onMounted(async () => {
  try {
    const p = await problemApi.detail(pid)
    if (!p.can_edit_statement) {
      ElMessage.warning('没有编辑此题目的权限')
      router.replace(`/problems/${pid}`)
      return
    }
    problem.value = p
    editInitial.value = {
      title: p.title,
      type: (p.type as 'objective' | 'function' | 'interactive' | undefined) || 'default',
      difficulty: p.difficulty ?? 3,
      time_limit_ms: p.time_limit_ms,
      memory_limit_mb: Math.max(16, Math.round(p.memory_limit_kb / 1024)),
      compare: p.compare === 'tolerant' ? 'tolerant' : 'exact',
      float_tolerance: p.float_tolerance ?? 0.0001,
      statement: p.description ?? '',
      file_io_name: (p.file_io_input || 'problem.in').replace(/\.in$/, ''),
      tag_ids: p.tags ? [...p.tags] : [],
      // 客观题整卷完整定义回填（编辑者门槛下 GetProblem 附带 questions_full）
      questions: (p.questions_full ?? []).map((q) => ({
        stem: q.stem ?? '',
        kind: q.kind,
        options: q.options ?? [],
        answer: q.answer ?? [],
        blanks: (q.blanks ?? []).map((b) => ({ accept: b.accept, case_sensitive: b.case_sensitive })),
        explanation: q.explanation ?? '',
        score: q.score ?? 0,
      })),
    }
  } finally {
    loading.value = false
  }
})

// ---- 自定义校验器（即时上传/移除，不随「确认修改」提交）----
const checkerFile = ref<File | null>(null)
const savingChecker = ref(false)
const showCheckerUpload = ref(false)

function onCheckerChange(file: UploadUserFile) {
  checkerFile.value = (file.raw as File) ?? null
}

async function saveChecker() {
  if (!checkerFile.value) return
  savingChecker.value = true
  try {
    const fd = new FormData()
    fd.append('checker', checkerFile.value)
    await problemApi.uploadChecker(pid, fd)
    ElMessage.success('校验器已上传，后续提交由校验器判定')
    checkerFile.value = null
    await load()
  } finally {
    savingChecker.value = false
  }
}

// 函数题（v0.9.1）：driver/template 即时上传替换（不随「确认修改」提交）
async function uploadFuncFile(kind: 'driver' | 'template', file: UploadUserFile) {
  const f = (file.raw as File) ?? null
  if (!f) return
  const fd = new FormData()
  fd.append('file', f)
  await problemApi.uploadFunctionFile(pid, kind, fd)
  ElMessage.success(kind === 'driver' ? '驱动器已更新，之后的评测按新驱动器合编' : '学生模板已更新')
  await load()
}

// 交互题（v0.9.2）：interactor 即时上传替换（判题编译缓存随保存失效自动重编）
async function uploadInteractor(file: UploadUserFile) {
  const f = (file.raw as File) ?? null
  if (!f) return
  const fd = new FormData()
  fd.append('file', f)
  await problemApi.uploadInteractor(pid, fd)
  ElMessage.success('交互器已更新，之后的评测按新交互器判分')
  await load()
}

async function removeTemplate() {
  await ElMessageBox.confirm('移除后学生提交不再预填模板，确认移除？', '移除模板', {
    type: 'warning',
    confirmButtonText: '移除',
    cancelButtonText: '取消',
  })
  await problemApi.removeFunctionTemplate(pid)
  ElMessage.success('模板已移除')
  await load()
}

async function removeChecker() {
  await ElMessageBox.confirm('移除后恢复按比对模式判定，确认移除？', '移除校验器', {
    type: 'warning',
    confirmButtonText: '移除',
    cancelButtonText: '取消',
  })
  await problemApi.removeChecker(pid)
  ElMessage.success('校验器已移除')
  await load()
}

async function load() {
  const p = await problemApi.detail(pid)
  problem.value = p
}

// ---- 保存：题面 → 元数据 → （可选）整包替换测试数据，成功回详情 ----
async function onSubmit(p: ProblemFormPayload) {
  if (p.zip) {
    const confirmed = await ElMessageBox.confirm(
      '将以新 zip 整包替换测试数据：旧测试点全部删除，样例章节按新数据重建。确认替换？',
      '替换测试数据',
      { type: 'warning', confirmButtonText: '替换', cancelButtonText: '取消' },
    )
      .then(() => true)
      .catch(() => false)
    if (!confirmed) return
  }
  savingEdit.value = true
  try {
    await problemApi.setStatement(pid, p.statement)
    if (p.type === 'objective') {
      // 客观题编辑（v0.9）：题面走 statement，题目定义/标题/标签走 meta question_json
      await problemApi.updateMeta(pid, {
        title: p.title,
        difficulty: p.difficulty,
        tag_ids: p.tag_ids,
        questions_json: JSON.stringify(p.questions),
      })
      ElMessage.success('题目已保存')
      router.push(`/problems/${pid}`)
      return
    }
    await problemApi.updateMeta(pid, {
      title: p.title,
      time_limit_ms: p.time_limit_ms,
      memory_limit_kb: p.memory_limit_mb * 1024,
      difficulty: p.difficulty,
      compare: p.compare,
      file_io_name: p.file_io_name,
      tag_ids: p.tag_ids,
      ...(p.compare === 'tolerant' ? { float_tolerance: p.float_tolerance } : {}),
    })
    if (p.zip) {
      const fd = new FormData()
      fd.append('zip', p.zip)
      await problemApi.replaceTestdata(pid, fd)
    }
    ElMessage.success('题目已保存')
    router.push(`/problems/${pid}`)
  } finally {
    savingEdit.value = false
  }
}

function onCancel() {
  router.push(`/problems/${pid}`)
}
</script>

<template>
  <div v-loading="loading">
    <ProblemForm
      v-if="problem"
      mode="edit"
      :pid="pid"
      :saving="savingEdit"
      confirm-text="确认修改"
      :initial="editInitial"
      :file-i-o-hint="fileIOHint"
      @submit="onSubmit"
      @cancel="onCancel"
    >
      <template #others-extra>
        <!-- 函数题（v0.9.1）：driver/template 即时上传/替换 -->
        <div v-if="problem.type === 'function'" class="checker-block func-block">
          <div class="checker-status">
            驱动器
            <el-tag v-if="problem.function?.has_driver" size="small" type="success">已配置 driver.cpp</el-tag>
            <el-tag v-else size="small" type="danger">未配置（判题不可用）</el-tag>
            <el-upload
              accept=".cpp"
              :limit="1"
              :auto-upload="false"
              :show-file-list="false"
              :on-change="(f: UploadUserFile) => uploadFuncFile('driver', f)"
            >
              <el-button size="small" text type="primary">{{ problem.function?.has_driver ? '替换' : '上传' }} driver.cpp</el-button>
            </el-upload>
          </div>
          <div class="checker-status" style="margin-top: 8px">
            学生模板
            <el-tag v-if="problem.function?.has_template" size="small" type="success">已配置 template.cpp</el-tag>
            <el-tag v-else size="small" type="info">未配置（可选）</el-tag>
            <el-upload
              accept=".cpp"
              :limit="1"
              :auto-upload="false"
              :show-file-list="false"
              :on-change="(f: UploadUserFile) => uploadFuncFile('template', f)"
            >
              <el-button size="small" text type="primary">{{ problem.function?.has_template ? '替换' : '上传' }} template.cpp</el-button>
            </el-upload>
            <el-button v-if="problem.function?.has_template" size="small" text type="danger" @click="removeTemplate">移除</el-button>
          </div>
        </div>
        <!-- 交互题（v0.9.2）：interactor 即时上传/替换 -->
        <div v-if="problem.type === 'interactive'" class="checker-block func-block">
          <div class="checker-status">
            交互器
            <el-tag v-if="problem.interactive?.has_interactor" size="small" type="success">已配置 interactor.cpp</el-tag>
            <el-tag v-else size="small" type="danger">未配置（判题不可用）</el-tag>
            <el-upload
              accept=".cpp"
              :limit="1"
              :auto-upload="false"
              :show-file-list="false"
              :on-change="uploadInteractor"
            >
              <el-button size="small" text type="primary">{{ problem.interactive?.has_interactor ? '替换' : '上传' }} interactor.cpp</el-button>
            </el-upload>
          </div>
        </div>
        <div class="checker-block">
          <div class="checker-status">
            自定义校验器
            <el-tag v-if="problem.has_checker" size="small" type="success">
              已启用 · 判题由校验器决定（比对模式不生效）
            </el-tag>
            <el-tag v-else size="small" type="info">未启用 · 按比对模式判定</el-tag>
            <el-button
              v-if="!problem.has_checker"
              size="small"
              text
              type="primary"
              @click="showCheckerUpload = !showCheckerUpload"
            >
              {{ showCheckerUpload ? '收起配置' : '配置校验器' }}
            </el-button>
          </div>
          <template v-if="problem.has_checker || showCheckerUpload">
            <el-upload
              drag
              accept=".cpp"
              :limit="1"
              :auto-upload="false"
              :on-change="onCheckerChange"
              :on-remove="() => (checkerFile = null)"
              class="checker-upload"
            >
              <div class="checker-hint">
                <p>{{ problem.has_checker ? '已有校验器，拖入新文件可替换' : '拖入 C++ 校验器源文件或点击选择（.cpp，≤256KB）' }}</p>
                <p class="dim">调用约定：checker 输入 期望 考生输出，退出码 0 = 通过</p>
              </div>
            </el-upload>
            <div class="checker-actions">
              <el-button v-if="problem.has_checker" type="danger" @click="removeChecker">移除校验器</el-button>
              <el-button type="primary" :loading="savingChecker" :disabled="!checkerFile" @click="saveChecker">
                上传并启用
              </el-button>
            </div>
          </template>
        </div>
      </template>

      <!-- 客观题配置说明（v0.9.3）：编辑页右侧「其他信息」下方，告诉老师每题配什么、怎么判分 -->
      <template #aside-bottom>
        <el-card v-if="problem.type === 'objective'" shadow="never">
          <template #header><span>客观题配置说明</span></template>
          <div class="tips obj-tips">
            <p><b>题目内容</b>：在左侧「题干」里写题目描述；选择题的选项与正确答案、填空题的每空可接受答案，都在题干下方的配置区里填（可增删选项/空位）。</p>
            <p><b>填空题</b>：题干里用连续 4 个下划线（____）标记空位，数量必须与下方「第 N 空」一致；学生做题时按顺序在输入框里作答。</p>
            <p><b>判分</b>：全对得满分，否则 0 分（选择题要求所选集合与正确答案完全一致）。</p>
            <p><b>答案解析</b>：学生拿到满分后才可见，与题解同门槛；老师随时可见。</p>
            <p><b>选项答案怎么勾</b>：单选题/判断题点圆圈，多选题勾方框；选项内容改完会自动对应，不用另存。</p>
          </div>
        </el-card>
      </template>
    </ProblemForm>
  </div>
</template>

<style scoped>
.obj-tips p {
  font-size: var(--soj-fs-body);
  line-height: 1.7;
  margin: 0 0 8px;
}

.obj-tips b {
  color: var(--el-text-color-primary);
}

.checker-block {
  width: 100%;
}

.checker-status {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.checker-hint {
  padding: 12px 0;
  text-align: center;
}

.checker-hint p {
  margin: 0;
}

.checker-actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}
</style>

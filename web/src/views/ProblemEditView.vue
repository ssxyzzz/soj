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
import type { ProblemDetail } from '@/types'

const route = useRoute()
const router = useRouter()
const pid = Number(route.params.id)

const problem = ref<ProblemDetail | null>(null)
const loading = ref(true)
const savingEdit = ref(false)
const editInitial = ref<Partial<Omit<ProblemFormPayload, 'zip' | 'checker'>>>({})
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
      time_limit_ms: p.time_limit_ms,
      memory_limit_mb: Math.max(16, Math.round(p.memory_limit_kb / 1024)),
      compare: p.compare === 'tolerant' ? 'tolerant' : 'exact',
      float_tolerance: p.float_tolerance ?? 0.0001,
      statement: p.description ?? '',
      file_io_name: (p.file_io_input || 'problem.in').replace(/\.in$/, ''),
      tag_ids: p.tags ? [...p.tags] : [],
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
    await problemApi.updateMeta(pid, {
      title: p.title,
      time_limit_ms: p.time_limit_ms,
      memory_limit_kb: p.memory_limit_mb * 1024,
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
      :saving="savingEdit"
      confirm-text="确认修改"
      :initial="editInitial"
      :file-i-o-hint="fileIOHint"
      @submit="onSubmit"
      @cancel="onCancel"
    >
      <template #others-extra>
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
    </ProblemForm>
  </div>
</template>

<style scoped>
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

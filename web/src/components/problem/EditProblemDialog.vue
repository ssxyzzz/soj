<script setup lang="ts">
// 编辑题目弹窗（v0.5e ② 拆分自 ProblemDetailView）：表单走 ProblemForm 共享组件，
// zip 可选——留空仅更新题面/信息，选新 zip 则整包替换测试数据；
// 自定义校验器区并入本弹窗（上传/移除为即时操作，不随「保存」提交）。
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { UploadUserFile } from 'element-plus'

import { problemApi } from '@/api'
import ProblemForm from '@/components/ProblemForm.vue'
import type { ProblemFormPayload } from '@/components/ProblemForm.vue'
import type { ProblemDetail } from '@/types'

const props = defineProps<{
  modelValue: boolean
  problem: ProblemDetail
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void; (e: 'saved'): void }>()

const savingEdit = ref(false)
const editFormRef = ref<InstanceType<typeof ProblemForm>>()
const editInitial = ref<Partial<Omit<ProblemFormPayload, 'zip' | 'checker'>>>({})
const fileIOHint = computed(() => ({
  input: props.problem.file_io_input || 'problem.in',
  output: props.problem.file_io_output || 'problem.out',
}))

// 自定义校验器（v0.5b-2）：上传 .cpp 源文件，判题时懒编译；移除恢复按比对模式判定。
// 未启用时上传区默认收起（保留「配置校验器」入口），已启用或点开后显示。
const checkerFile = ref<File | null>(null)
const savingChecker = ref(false)
const showCheckerUpload = ref(false)

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      const p = props.problem
      editInitial.value = {
        title: p.title,
        time_limit_ms: p.time_limit_ms,
        memory_limit_mb: Math.max(16, Math.round(p.memory_limit_kb / 1024)),
        compare: p.compare === 'tolerant' ? 'tolerant' : 'exact',
        float_tolerance: p.float_tolerance ?? 0.0001,
        statement: p.description ?? '',
        file_io_name: (p.file_io_input || 'problem.in').replace(/\.in$/, ''),
      }
      checkerFile.value = null
      showCheckerUpload.value = false
    }
  },
)

function onCheckerChange(file: UploadUserFile) {
  checkerFile.value = (file.raw as File) ?? null
}

async function saveChecker() {
  if (!checkerFile.value) return
  savingChecker.value = true
  try {
    const fd = new FormData()
    fd.append('checker', checkerFile.value)
    await problemApi.uploadChecker(props.problem.id, fd)
    ElMessage.success('校验器已上传，后续提交由校验器判定')
    checkerFile.value = null
    emit('saved')
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
  await problemApi.removeChecker(props.problem.id)
  ElMessage.success('校验器已移除')
  emit('saved')
}

async function onSaveEditClick() {
  const p = editFormRef.value?.payload()
  if (!p) return
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
    await problemApi.setStatement(props.problem.id, p.statement)
    await problemApi.updateMeta(props.problem.id, {
      title: p.title,
      time_limit_ms: p.time_limit_ms,
      memory_limit_kb: p.memory_limit_mb * 1024,
      compare: p.compare,
      file_io_name: p.file_io_name,
      ...(p.compare === 'tolerant' ? { float_tolerance: p.float_tolerance } : {}),
    })
    if (p.zip) {
      const fd = new FormData()
      fd.append('zip', p.zip)
      await problemApi.replaceTestdata(props.problem.id, fd)
    }
    ElMessage.success('题目已保存')
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    savingEdit.value = false
  }
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="编辑题目"
    width="1020"
    top="4vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <ProblemForm
      v-if="modelValue"
      ref="editFormRef"
      mode="edit"
      :saving="savingEdit"
      :initial="editInitial"
      :file-i-o-hint="fileIOHint"
    />
    <el-form label-position="top">
      <el-form-item>
        <div class="checker-block">
          <div class="checker-status">
            自定义校验器
            <el-tag v-if="problem.has_checker" size="small" type="success">已启用 · 判题由校验器决定（比对模式不生效）</el-tag>
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
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="savingEdit" @click="onSaveEditClick">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.checker-status {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
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

<script setup lang="ts">
// 建题/编辑题目共用表单（v0.5b-4）：字段、布局与校验单一来源。
// 提交动作 emit 给调用方——新增 = POST /problems 一次交齐；
// 编辑 = statement / meta / testdata 分步（编辑模式的 zip 为可选的整包替换）。
// 编辑弹窗不渲染内置提交按钮（走弹窗 footer），经 expose 的 payload() 取值。
import { reactive, ref } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'
import type { UploadUserFile } from 'element-plus'
import { ElMessage } from 'element-plus'

import MdEditor from '@/components/MdEditor.vue'
import { MAX_MARKDOWN_BYTES, markdownBytes } from '@/utils/vditor'

export interface ProblemFormPayload {
  title: string
  time_limit_ms: number
  memory_limit_mb: number
  compare: string
  float_tolerance: number
  statement: string
  file_io_name: string
  zip: File | null
  checker: File | null
}

const props = withDefaults(
  defineProps<{
    mode: 'create' | 'edit'
    saving?: boolean
    initial?: Partial<Omit<ProblemFormPayload, 'zip' | 'checker'>>
    fileIOHint?: { input: string; output: string } // 编辑模式：现行文件 IO 实际文件名
  }>(),
  {
    saving: false,
    initial: () => ({}),
    fileIOHint: undefined,
  },
)

const emit = defineEmits<{ (e: 'submit', p: ProblemFormPayload): void }>()

// initial 仅在挂载时读取一次；编辑弹窗以 v-if 每次打开重新挂载
const form = reactive({
  title: props.initial.title ?? '',
  time_limit_ms: props.initial.time_limit_ms ?? 1000,
  memory_limit_mb: props.initial.memory_limit_mb ?? 256,
  compare: props.initial.compare ?? 'exact',
  float_tolerance: props.initial.float_tolerance ?? 0.0001,
  statement: props.initial.statement ?? '',
  file_io_name: props.initial.file_io_name ?? '',
})

const fileList = ref<UploadUserFile[]>([])
const checkerList = ref<UploadUserFile[]>([])

function onCheckerChange(_file: UploadUserFile, files: UploadUserFile[]) {
  checkerList.value = files.slice(-1)
}

function onCheckerRemove(_file: UploadUserFile, files: UploadUserFile[]) {
  checkerList.value = files
}

function checkerFile(): File | null {
  const f = checkerList.value[0]
  return (f?.raw as File) ?? null
}

function onZipChange(_file: UploadUserFile, files: UploadUserFile[]) {
  // 只保留最新选择的一个 zip
  fileList.value = files.slice(-1)
}

function onZipRemove(_file: UploadUserFile, files: UploadUserFile[]) {
  fileList.value = files
}

function zipFile(): File | null {
  const f = fileList.value[0]
  return (f?.raw as File) ?? null
}

// 校验并收集表单载荷；不合法时弹提示并返回 null（新增按钮与编辑弹窗保存共用）
function payload(): ProblemFormPayload | null {
  if (!form.title.trim()) {
    ElMessage.warning('请填写标题')
    return null
  }
  if (!form.statement.trim()) {
    ElMessage.warning('请填写题面')
    return null
  }
  if (markdownBytes(form.statement) > MAX_MARKDOWN_BYTES) {
    ElMessage.error('题面过长（上限 256KB）')
    return null
  }
  const zip = zipFile()
  if (props.mode === 'create' && !zip) {
    ElMessage.warning('请上传测试数据 zip（cases/<N>/in|ans 结构）')
    return null
  }
  return {
    ...form,
    title: form.title.trim(),
    statement: form.statement,
    file_io_name: form.file_io_name.trim(),
    zip,
    checker: checkerFile(),
  }
}

function onSubmitClick() {
  const p = payload()
  if (p) emit('submit', p)
}

defineExpose({ payload })
</script>

<template>
  <el-form label-position="top">
    <el-form-item label="标题" required>
      <el-input v-model="form.title" maxlength="64" show-word-limit placeholder="64 字符以内" />
    </el-form-item>
    <el-form-item label="时限 / 内存">
      <div class="limits">
        <el-input-number v-model="form.time_limit_ms" :min="100" :max="600000" :step="100" />
        <span class="dim">ms</span>
        <el-input-number v-model="form.memory_limit_mb" :min="16" :max="1024" :step="16" />
        <span class="dim">MB</span>
      </div>
    </el-form-item>
    <el-form-item label="题面" required>
      <MdEditor v-model="form.statement" :height="mode === 'edit' ? '480px' : '420px'" />
    </el-form-item>
    <el-form-item :required="mode === 'create'">
      <template #label>
        测试数据 zip
        <span v-if="mode === 'edit'" class="dim zip-replace-hint">
          留空保留现有测试数据；选择新 zip 将<b>整包替换</b>（旧测试点全部删除，样例章节按新数据重建）
        </span>
      </template>
      <el-upload
        v-model:file-list="fileList"
        class="zip-upload"
        drag
        accept=".zip"
        :limit="1"
        :auto-upload="false"
        :on-change="onZipChange"
        :on-remove="onZipRemove"
      >
        <div class="zip-hint">
          <p>{{ mode === 'create' ? '拖入 zip 或点击选择' : '拖入新 zip 可整包替换测试数据（旧数据不保留）' }}</p>
          <p class="dim">结构：cases/1/in、cases/1/ans、cases/2/in……每个测试点一对输入/答案文件</p>
        </div>
      </el-upload>
    </el-form-item>
    <el-form-item>
      <el-tooltip
        placement="top"
        content="全文比对：输出与答案精确一致（忽略行尾空白与文末换行）；宽容比对：按空白分词比较，数字可带精度容差（适合答案不唯一的构造/计算题）"
      >
        <span class="compare-label">
          比对模式
          <el-icon><QuestionFilled /></el-icon>
        </span>
      </el-tooltip>
      <div>
        <el-radio-group v-model="form.compare">
          <el-radio-button value="exact">全文比对</el-radio-button>
          <el-radio-button value="tolerant">宽容比对</el-radio-button>
        </el-radio-group>
        <span class="dim ml8">
          {{ form.compare === 'tolerant' ? '按空白分词比较，数字可带精度容差（适合答案不唯一的构造/计算题）' : '输出与答案精确一致（忽略行尾空白与文末换行）' }}
        </span>
      </div>
    </el-form-item>
    <el-form-item v-if="form.compare === 'tolerant'" label="浮点容差">
      <el-input-number v-model="form.float_tolerance" :min="0.000001" :max="1" :step="0.0001" :precision="6" />
    </el-form-item>
    <el-form-item v-if="mode === 'create'" label="自定义校验器（可选）">
      <div>
        <el-upload
          v-model:file-list="checkerList"
          accept=".cpp"
          :limit="1"
          :auto-upload="false"
          :on-change="onCheckerChange"
          :on-remove="onCheckerRemove"
        >
          <el-button>选择 checker.cpp</el-button>
        </el-upload>
        <div class="dim checker-tip">
          配置后判题由校验器决定（调用约定：checker 输入 期望 考生输出，退出码 0 = 通过）。也可创建后在题目页管理。
        </div>
      </div>
    </el-form-item>
    <el-form-item>
      <div class="fileio-block">
        <div class="fileio-title">
          文件 IO 文件名（可选）
          <span class="dim">仅比赛/考试「本场启用文件 IO」时生效；留空默认 problem，输入文件即 problem.in、输出即 problem.out</span>
        </div>
        <el-input
          v-model="form.file_io_name"
          placeholder="problem"
          style="width: 280px"
          maxlength="64"
        />
        <div v-if="fileIOHint" class="dim" style="margin-top: 6px">
          启用文件 IO 的比赛/考试中，输入文件为 <b>{{ fileIOHint.input }}</b>、输出文件为
          <b>{{ fileIOHint.output }}</b>，学生端会看到对应的提交提示
        </div>
      </div>
    </el-form-item>
    <el-form-item v-if="mode === 'create'">
      <el-button type="primary" :loading="saving" @click="onSubmitClick">创建题目</el-button>
    </el-form-item>
  </el-form>
</template>

<style scoped>
.limits {
  display: flex;
  align-items: center;
  gap: 8px;
}

.fileio-block {
  width: 100%;
}

.fileio-title {
  font-weight: 600;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.fileio-block :deep(.el-form-item__content) {
  display: block;
}

.zip-upload {
  width: 100%;
}

.zip-upload :deep(.el-upload-dragger) {
  width: 100%;
}

.zip-replace-hint {
  margin-left: 8px;
  font-weight: 400;
}

.compare-label {
  cursor: help;
  color: #606266;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.zip-hint {
  padding: 8px;
}

.zip-hint p {
  margin: 2px 0;
}

.checker-tip {
  margin-top: 6px;
}

.ml8 {
  margin-left: 8px;
}
</style>

<script setup lang="ts">
// 建题/编辑题目共用表单（v0.5b-4）：字段、布局与校验单一来源。
// 提交动作 emit 给调用方——新增 = POST /problems 一次交齐；
// 编辑 = statement / meta / testdata 分步（编辑模式的 zip 为可选的整包替换）。
// 编辑弹窗不渲染内置提交按钮（走弹窗 footer），经 expose 的 payload() 取值。
import { computed, reactive, ref } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'
import type { UploadUserFile } from 'element-plus'
import { ElMessage } from 'element-plus'

import MdEditor from '@/components/MdEditor.vue'
import TagFilter from '@/components/problem/TagFilter.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { MAX_MARKDOWN_BYTES, markdownBytes } from '@/utils/vditor'

export interface ProblemFormPayload {
  title: string
  time_limit_ms: number
  memory_limit_mb: number
  compare: string
  float_tolerance: number
  statement: string
  file_io_name: string
  tag_ids: number[]
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
  tag_ids: props.initial.tag_ids ?? [],
})

const tagStore = useTagTreeStore()
tagStore.ensure()
// 空树时展示「管理员尚未配置标签树」占位
const tagTreeEmpty = computed(() => !tagStore.nodes.length)

// 标签卡片流点选回调：整体替换 tag_ids（数组顺序即点选顺序）
function onTagsChange(ids: number[]) {
  form.tag_ids = ids
}

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
    tag_ids: [...form.tag_ids],
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
    <el-form-item>
      <div class="limits-row">
        <div class="field-group">
          <div class="field-label">时限 / 内存</div>
          <div class="limits">
            <el-input-number v-model="form.time_limit_ms" :min="100" :max="600000" :step="100" />
            <span class="dim">ms</span>
            <el-input-number v-model="form.memory_limit_mb" :min="16" :max="1024" :step="16" />
            <span class="dim">MB</span>
          </div>
        </div>
        <div class="field-group">
          <div class="field-label">
            文件 IO 文件名（可选）
            <el-tooltip
              placement="top"
              content="仅比赛/考试「本场启用文件 IO」时生效，题库/练习/作业恒为标准输入输出；留空默认 problem.in / problem.out"
            >
              <el-icon class="dim"><QuestionFilled /></el-icon>
            </el-tooltip>
          </div>
          <div class="fileio-row">
            <el-input v-model="form.file_io_name" placeholder="problem" style="width: 180px" maxlength="32" />
            <span v-if="fileIOHint" class="dim fileio-now">当前：{{ fileIOHint.input }} / {{ fileIOHint.output }}</span>
          </div>
        </div>
      </div>
    </el-form-item>
    <el-form-item label="题面" required>
      <MdEditor v-model="form.statement" :height="mode === 'edit' ? '480px' : '420px'" />
    </el-form-item>
    <el-form-item :required="mode === 'create'">
      <template #label>
        <span class="label-with-tip">
          测试数据 zip
          <span v-if="mode === 'edit'" class="dim zip-replace-hint">留空保留现有数据；选择新 zip 将整包替换（旧数据不保留）</span>
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
          <p>{{ mode === 'create' ? '拖入 zip 或点击选择' : '拖入新 zip 整包替换，留空则不修改' }}</p>
          <p class="dim">结构：test1.in、test1.ans/out……每个测试点一对输入/输出；cases/1/in|ans 目录结构亦可，外层多套一层文件夹没问题</p>
        </div>
      </el-upload>
    </el-form-item>
    <el-form-item>
      <template #label>
        <span class="label-with-tip">
          比对模式
          <el-tooltip
            placement="top"
            content="全文比对：输出与答案精确一致（忽略行尾空白与文末换行）；宽容比对：按空白分词比较，数字可带精度容差（适合答案不唯一的构造/计算题）"
          >
            <el-icon class="dim"><QuestionFilled /></el-icon>
          </el-tooltip>
        </span>
      </template>
      <div class="compare-row">
        <el-radio-group v-model="form.compare">
          <el-radio-button value="exact">全文比对</el-radio-button>
          <el-radio-button value="tolerant">宽容比对</el-radio-button>
        </el-radio-group>
        <span class="dim">
          {{ form.compare === 'tolerant' ? '按空白分词比较，数字可带精度容差（适合答案不唯一的构造/计算题）' : '输出与答案精确一致（忽略行尾空白与文末换行）' }}
        </span>
      </div>
    </el-form-item>
    <el-form-item v-if="form.compare === 'tolerant'" label="浮点容差" class="plain-label">
      <el-input-number v-model="form.float_tolerance" :min="0.000001" :max="1" :step="0.0001" :precision="6" />
    </el-form-item>
    <el-form-item class="plain-label">
      <template #label>
        <span class="label-with-tip">
          标签
          <el-tooltip
            placement="top"
            content="悬停展开下级、可搜索；单击卡片选中/取消，选中的标签显示在下方（点 × 可移除）；树的层级由 root 在后台「标签树」维护，改名后全站展示自动跟随"
          >
            <el-icon class="dim"><QuestionFilled /></el-icon>
          </el-tooltip>
        </span>
      </template>
      <TagFilter v-if="!tagTreeEmpty" :selected="form.tag_ids" @change="onTagsChange" />
      <span v-else class="dim">管理员尚未配置标签树（后台管理 → 标签树）</span>
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
    <el-form-item v-if="mode === 'create'">
      <el-button type="primary" :loading="saving" @click="onSubmitClick">创建题目</el-button>
    </el-form-item>
  </el-form>
</template>

<style scoped>
/* 表单标签不加粗（Ant：标签与正文同字重 400，颜色不降档） */
:deep(.el-form-item__label) {
  font-weight: 400;
  color: var(--el-text-color-primary);
}

/* 时限/内存 与 文件 IO 同行排布（各带自绘标签，样式对齐 el-form-item__label） */
.limits-row {
  display: flex;
  gap: 40px;
  width: 100%;
  flex-wrap: wrap;
}

.field-group {
  display: flex;
  flex-direction: column;
}

.field-label {
  font-weight: 400;
  color: var(--el-text-color-primary);
  margin-bottom: 8px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.fileio-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.fileio-now {
  white-space: nowrap; /* 防止提示文字换行 */
}

.limits {
  display: flex;
  align-items: center;
  gap: 8px;
}

.zip-upload {
  width: 100%;
}

.zip-upload :deep(.el-upload-dragger) {
  width: 100%;
}

.zip-replace-hint {
  font-weight: 400;
}

/* label 插槽内的图标/提示与标签文字垂直居中（label 为块级布局，行内元素默认按基线对齐会偏高） */
.label-with-tip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

/* 单选按钮组与说明文字垂直居中 */
.compare-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

/* 浮点容差为次级条件字段：字重颜色降一档（不随表单主标签加粗） */
.plain-label :deep(.el-form-item__label) {
  font-weight: 400;
  color: var(--el-text-color-regular);
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
</style>

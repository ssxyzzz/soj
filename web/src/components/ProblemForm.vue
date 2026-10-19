<script setup lang="ts">
// 建题/编辑题目共用表单：字段、布局与校验单一来源。
// 布局为单 el-form 跨主/右两列（统一校验与 payload），主列只留 题名/标签/题面，
// 右列 = 管理卡（确认 + 取消）/测试数据/文件 IO/其他信息；<992px 折单列。
// 提交动作 emit 给调用方——新增 = POST /problems 一次交齐；
// 编辑 = statement / meta / testdata 分步（编辑模式的 zip 为可选的整包替换）。
import { computed, reactive, ref } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'
import type { UploadUserFile } from 'element-plus'
import { ElMessage } from 'element-plus'

import MdEditor from '@/components/MdEditor.vue'
import TagFilter from '@/components/problem/TagFilter.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { MAX_RICHTEXT_BYTES, richtextBytes } from '@/utils/richtext'

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
    confirmText?: string // 管理卡主按钮文案（确认创建/确认修改）
    initial?: Partial<Omit<ProblemFormPayload, 'zip' | 'checker'>>
    fileIOHint?: { input: string; output: string } // 编辑模式：现行文件 IO 实际文件名
  }>(),
  {
    saving: false,
    confirmText: '确认创建',
    initial: () => ({}),
    fileIOHint: undefined,
  },
)

const emit = defineEmits<{
  (e: 'submit', p: ProblemFormPayload): void
  (e: 'cancel'): void
}>()

// initial 仅在挂载时读取一次；编辑页以路由承载，重进即重挂
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

// 校验并收集表单载荷；不合法时弹提示并返回 null
function payload(): ProblemFormPayload | null {
  if (!form.title.trim()) {
    ElMessage.warning('请填写标题')
    return null
  }
  if (!form.statement.trim()) {
    ElMessage.warning('请填写题面')
    return null
  }
  if (richtextBytes(form.statement) > MAX_RICHTEXT_BYTES) {
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
</script>

<template>
  <!-- 单 el-form 跨主/右两列：卡片只是字段分组容器，校验与 payload 统一 -->
  <el-form v-loading="saving" class="pf" label-position="top">
    <div class="pf-layout">
      <div class="pf-main">
        <el-card>
          <el-form-item label="标题" required>
            <el-input v-model="form.title" maxlength="64" show-word-limit placeholder="64 字符以内" />
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
            <div v-if="!tagTreeEmpty" class="tag-bar">
              <TagFilter :selected="form.tag_ids" @change="onTagsChange" />
            </div>
            <span v-else class="dim">管理员尚未配置标签树（后台管理 → 标签树）</span>
          </el-form-item>
          <el-form-item label="题面" required>
            <MdEditor v-model="form.statement" max-height="4000px" />
          </el-form-item>
        </el-card>
      </div>

      <div class="pf-aside">
        <el-card>
          <template #header><span>管理</span></template>
          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" :loading="saving" @click="onSubmitClick">
              {{ confirmText }}
            </el-button>
            <el-button style="width: 100%" @click="emit('cancel')">取消</el-button>
          </div>
        </el-card>

        <el-card>
          <template #header><span>其他信息</span></template>
          <el-form-item :required="mode === 'create'">
            <template #label>
              <span class="label-with-tip">
                测试数据 zip
                <el-tooltip
                  placement="top"
                  content="留空保留现有数据；选择新 zip 将整包替换（旧数据不保留）"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
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
                <p class="dim">结构：test1.in、test1.ans/out</p>
              </div>
            </el-upload>
          </el-form-item>
          <el-form-item>
            <div class="field-group">
              <div class="field-label">
                文件名（可选）
                <el-tooltip
                  placement="top"
                  content="仅比赛/考试「本场启用文件 IO」时生效。留空默认 problem.in / problem.out"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </div>
              <div class="fileio-row">
                <el-input v-model="form.file_io_name" placeholder="problem" maxlength="32" />
              </div>
              <span v-if="fileIOHint" class="dim fileio-now">
                当前：{{ fileIOHint.input }} / {{ fileIOHint.output }}
              </span>
            </div>
          </el-form-item>
          <!-- 说明框：占位留位，内容为后续功能（待定） -->
          <el-form-item>
            <div class="field-group">
              <div class="field-label">说明</div>
              <div class="pf-note dim">来源/年份/地市，用,分隔</div>
            </div>
          </el-form-item>
          <el-form-item>
            <div class="field-group">
              <div class="field-label">时限 / 内存</div>
              <div class="limits">
                <el-input-number v-model="form.time_limit_ms" :controls="false" :min="100" :max="600000" :step="100" />
                <span class="dim">ms</span>
                <el-input-number v-model="form.memory_limit_mb" :controls="false" :min="16" :max="1024" :step="16" />
                <span class="dim">MB</span>
              </div>
            </div>
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
                {{ form.compare === 'tolerant' ? '按空白分词比较，数字可带精度容差' : '输出与答案精确一致（忽略行尾空白与文末换行）' }}
              </span>
            </div>
          </el-form-item>
          <el-form-item v-if="form.compare === 'tolerant'" label="浮点容差" class="plain-label">
            <el-input-number v-model="form.float_tolerance" :min="0.000001" :max="1" :step="0.0001" :precision="6" />
          </el-form-item>
          <el-form-item v-if="mode === 'create'" class="plain-label">
            <template #label>
              <span class="label-with-tip">自定义校验器（可选）</span>
            </template>
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
          <!-- 编辑页在此注入自定义校验器管理（即时上传/移除，不随保存提交） -->
          <slot name="others-extra" />
        </el-card>

        <slot name="aside-bottom" />
      </div>
    </div>
  </el-form>
</template>

<style scoped>
/* 表单标签不加粗（Ant：标签与正文同字重 400，颜色不降档） */
:deep(.el-form-item__label) {
  font-weight: 400;
  color: var(--el-text-color-primary);
}

/* 主/右两列：结构对齐 PageShell（1280 + 右列 300）；el-form 跨列保统一校验 */
.pf-layout {
  max-width: 1280px;
  margin: 0 auto;
  display: flex;
  gap: 24px;
  align-items: flex-start;
}

.pf-main {
  flex: 1 1 auto;
  min-width: 0;
}

.pf-aside {
  flex: 0 0 300px;
  width: 300px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* 纵向按钮组：EP 默认给相邻按钮 margin-left: 12px，纵向排列会把下面的按钮顶离中线 */
.pf :deep(.entry-actions .el-button + .el-button) {
  margin-left: 0;
}

/* 标签栏：与题库筛选区 .tag-row 同底色/圆角/内距（#f5f5f5） */
.tag-bar {
  box-sizing: border-box;
  width: 100%;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: var(--el-border-radius-base);
}

/* 说明框：占位留位（内容为后续功能） */
.pf-note {
  border: 1px dashed var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  padding: 10px 12px;
  margin-bottom: 8px;
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
  flex-wrap: nowrap; /* 时限与内存一行排布 */
}

/* 纯填空框（去加减按钮）并限宽，保证两个框加单位在右列一行放下 */
.limits :deep(.el-input-number) {
  width: 90px;
}

.zip-upload {
  width: 100%;
}

.zip-upload :deep(.el-upload-dragger) {
  width: 100%;
  /* 纵向内边距 40 → 10：拖拽框高度约减半（高度 = 上下内边距 + 两行提示文字） */
  --el-upload-dragger-padding-vertical: 10px;
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

.checker-tip {
  padding: 8px 0 0;
  margin: 0;
}

@media (max-width: 992px) {
  .pf-layout {
    flex-direction: column;
  }

  .pf-aside {
    width: auto;
    flex: 1 1 auto;
  }
}
</style>

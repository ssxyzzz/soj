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

import { downloadFile } from '@/api/download'

import MdEditor from '@/components/MdEditor.vue'
import ObjectiveEditor from '@/components/ObjectiveEditor.vue'
import TagFilter from '@/components/problem/TagFilter.vue'
import { useLevelsStore } from '@/stores/levels'
import { useTagTreeStore } from '@/stores/tagtree'
import { MAX_CONTENT_BYTES, contentBytes } from '@/utils/md'
import { useUserStore } from '@/stores/user'
import type { ObjectiveKind, ObjectiveQuestionDraft } from '@/types'

export interface ProblemFormPayload {
  title: string
  type: 'default' | 'objective' | 'function' | 'interactive' // 题目类型（v0.9）：客观题走 question；函数题带 driver/template（v0.9.1）；交互题带 interactor（v0.9.2）
  difficulty: number // 难度 1–10（v0.11，缺省 D3）
  time_limit_ms: number
  memory_limit_mb: number
  compare: string
  float_tolerance: number
  statement: string
  file_io_name: string
  tag_ids: number[]
  zip: File | null
  checker: File | null
  questions: ObjectiveQuestionDraft[] | null
  driver: File | null // 函数题驱动器（v0.9.1）
  template: File | null // 函数题学生端模板（可选）
  interactor: File | null // 交互题交互器（v0.9.2）
}

const props = withDefaults(
  defineProps<{
    mode: 'create' | 'edit'
    saving?: boolean
    confirmText?: string // 管理卡主按钮文案（确认创建/确认修改）
    initial?: Partial<Omit<ProblemFormPayload, 'zip' | 'checker' | 'question'>> & {
      questions?: {
        stem?: string
        kind: ObjectiveKind
        options?: string[]
        answer?: number[]
        blanks?: { accept: string[]; case_sensitive?: boolean }[]
        explanation?: string
        score?: number
      }[]
    }
    fileIOHint?: { input: string; output: string } // 编辑模式：现行文件 IO 实际文件名
    pid?: number // 编辑模式：题目 ID（「下载数据」按钮拼下载地址用）
  }>(),
  {
    saving: false,
    confirmText: '确认创建',
    initial: () => ({}),
    fileIOHint: undefined,
    pid: undefined,
  },
)

const emit = defineEmits<{
  (e: 'submit', p: ProblemFormPayload): void
  (e: 'cancel'): void
}>()

// initial 仅在挂载时读取一次；编辑页以路由承载，重进即重挂
const userStore = useUserStore()

// 下载数据：有「下载数据」权限位且已有题目（编辑模式）时展示于 zip 上传行右侧
const canDownloadData = computed(() => !!props.pid && userStore.can('problem.data.download'))

async function downloadData() {
  if (!props.pid) return
  // 兜底名与服务端 Content-Disposition 同格式（problem - ID - 标题.zip）
  await downloadFile(`/problems/${props.pid}/testdata/download`, `problem - ${props.pid} - ${form.title || 'untitled'}.zip`)
}

const form = reactive({
  title: props.initial.title ?? '',
  type: (props.initial.type === 'objective'
    ? 'objective'
    : props.initial.type === 'function'
      ? 'function'
      : props.initial.type === 'interactive'
        ? 'interactive'
        : 'default') as 'default' | 'objective' | 'function' | 'interactive',
  difficulty: props.initial.difficulty ?? 3,
  time_limit_ms: props.initial.time_limit_ms ?? 1000,
  memory_limit_mb: props.initial.memory_limit_mb ?? 256,
  compare: props.initial.compare ?? 'exact',
  float_tolerance: props.initial.float_tolerance ?? 0.0001,
  statement: props.initial.statement ?? '',
  file_io_name: props.initial.file_io_name ?? '',
  tag_ids: props.initial.tag_ids ?? [],
})

// 时限输入以秒为单位（站点展示口径统一用 s），提交/存储仍为毫秒
const timeLimitSec = computed({
  get: () => form.time_limit_ms / 1000,
  set: (sec: number) => {
    form.time_limit_ms = Math.round(sec * 1000)
  },
})

const tagStore = useTagTreeStore()
tagStore.ensure()
// 难度档展示名（默认与等级名相同，root 后台可改；选择器不再用 D1~D10 描述）
const levels = useLevelsStore()
levels.ensure()
// 空树时展示「管理员尚未配置标签树」占位
const tagTreeEmpty = computed(() => !tagStore.nodes.length)
// 标签必填（建题同标题必填口径）；空树无从选择不拦截，编辑存量无标签题也不强制补挂
const tagsRequired = computed(() => props.mode === 'create' && !tagTreeEmpty.value)

// 标签卡片流点选回调：整体替换 tag_ids（数组顺序即点选顺序）
function onTagsChange(ids: number[]) {
  form.tag_ids = ids
}

// 难度锚点提示（v0.11，NOI 大纲 + 教学进度定标）：选择器旁展示当前档说明
const DIFFICULTY_HINTS: Record<number, string> = {
  1: '入门：变量/顺序/分支/循环',
  2: '基础：数组/函数/结构体',
  3: '算法入门：枚举/排序/递归递推',
  4: '算法基础：贪心/二分/DFS·BFS',
  5: 'CSP：DP入门/并查集/树状数组',
  6: 'CSPJ+：图论基础/线段树/KMP/倍增',
  7: 'CSPS：树形&状压DP/最短路进阶/网络流',
  8: 'CSPS+：数据结构综合/后缀结构/建模',
  9: 'NOIP：省选中档、NOI 简单题',
  10: 'NOI 级：NOI 难题/集训队水平',
}

const fileList = ref<UploadUserFile[]>([])
const checkerList = ref<UploadUserFile[]>([])
const driverList = ref<UploadUserFile[]>([])
const templateList = ref<UploadUserFile[]>([])
const interactorList = ref<UploadUserFile[]>([])

function oneFile(files: UploadUserFile[]): File | null {
  const f = files[0]
  return (f?.raw as File) ?? null
}

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

// ---- 客观题整卷草稿（v0.9.3）：小题列表由 ObjectiveEditor 维护，这里只存数组 ----
const objQuestions = ref<ObjectiveQuestionDraft[]>([])
const objEditor = ref<{ validate: () => string | null } | null>(null)

// 编辑回填：整卷完整定义（编辑者门槛下 questions_full）
const initQs = props.initial.questions
if (initQs && initQs.length) {
  objQuestions.value = initQs.map((q) => ({
    stem: q.stem ?? '',
    kind: q.kind,
    options: q.options?.length ? [...q.options] : q.kind === 'judge' ? ['对', '错'] : ['', '', '', ''],
    answer: [...(q.answer ?? [])],
    blanks: (q.blanks ?? []).map((b) => ({ accept: [...b.accept], case_sensitive: !!b.case_sensitive })),
    explanation: q.explanation ?? '',
    score: q.score ?? 0,
  }))
}

// 客观题的分值：新增小题默认等分（父组件校验前会提示补全）
function ensureObjectiveScores() {
  const list = objQuestions.value
  if (!list.length) return
  if (list.every((q) => (q.score || 0) === 0)) {
    const base = 100 / list.length
    list.forEach((q, i) => {
      q.score = i === list.length - 1 ? 100 - Math.floor(base) * (list.length - 1) : Math.floor(base)
    })
  }
}

// 校验并收集表单载荷；不合法时弹提示并返回 null
function payload(): ProblemFormPayload | null {
  if (!form.title.trim()) {
    ElMessage.warning('请填写标题')
    return null
  }
  if (tagsRequired.value && form.tag_ids.length === 0) {
    ElMessage.warning('请至少选择一个标签')
    return null
  }
  // 客观题的题面是「整题说明（可选）」，不强制填写；必填项转由小题列表承担（下方 validate）
  if (form.type !== 'objective' && !form.statement.trim()) {
    ElMessage.warning('请填写题面')
    return null
  }
  if (contentBytes(form.statement) > MAX_CONTENT_BYTES) {
    ElMessage.error('题面过长（上限 256KB）')
    return null
  }
  const zip = zipFile() // 可选：不传则创建暂无数据的题目（详情页提示 + 禁提交），数据后续编辑页补齐
  let questions: ObjectiveQuestionDraft[] | null = null
  if (form.type === 'objective') {
    ensureObjectiveScores()
    const err = objEditor.value?.validate()
    if (err) {
      ElMessage.warning(err)
      return null
    }
    questions = objQuestions.value.map((q) => ({ ...q }))
  }
  const driver = oneFile(driverList.value)
  const interactor = oneFile(interactorList.value)
  if (form.type === 'function' && props.mode === 'create' && !driver && !zip) {
    ElMessage.warning('函数题需要上传 driver.cpp（含 main、调用学生实现的函数）')
    return null
  }
  if (form.type === 'interactive' && props.mode === 'create' && !interactor && !zip) {
    ElMessage.warning('交互题需要上传 interactor.cpp（读输入文件、与选手双向通信、退出码 0 = 通过）')
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
    questions,
    driver,
    template: oneFile(templateList.value),
    interactor,
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
          <el-form-item>
            <template #label>
              <span class="label-with-tip">
                题目类型
                <el-tooltip placement="top" content="传统题＝编程评测（测试数据比对/SPJ/子任务/文件 IO）；客观题＝单选/多选/判断/填空，题干写在题面里、提交即判分">
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </span>
            </template>
            <el-radio-group
              :model-value="form.type"
              :disabled="mode === 'edit'"
              @change="(v?: string | number | boolean) => { form.type = v === 'objective' ? 'objective' : v === 'function' ? 'function' : v === 'interactive' ? 'interactive' : 'default' }"
            >
              <el-radio-button value="default">传统题（编程）</el-radio-button>
              <el-radio-button value="objective">客观题（选择/填空）</el-radio-button>
              <el-radio-button value="function">函数题（补全实现）</el-radio-button>
              <el-radio-button value="interactive">交互题（双向通信）</el-radio-button>
            </el-radio-group>
            <span v-if="mode === 'edit'" class="dim type-lock">题型创建后不可更改</span>
          </el-form-item>
          <el-form-item class="plain-label" :required="tagsRequired">
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
          <el-form-item :required="form.type !== 'objective'">
            <template #label>
              <span class="label-with-tip">
                {{ form.type === 'objective' ? '整题说明（可选）' : '题面' }}
                <el-tooltip
                  v-if="form.type === 'objective'"
                  placement="top"
                  content="这里放整卷的导语/说明；每道小题的题干在下方小题列表里单独填写"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </span>
            </template>
            <MdEditor v-model="form.statement" auto-height />
          </el-form-item>

          <!-- 客观题整卷（v0.9.3）：小题列表（增删/排序 + 每题题干/选项/答案/解析/分值） -->
          <el-form-item v-if="form.type === 'objective'" class="plain-label">
            <ObjectiveEditor ref="objEditor" v-model="objQuestions" :pid="pid" />
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
          <el-form-item v-if="form.type !== 'objective'">
            <template #label>
              <div class="zip-label-row">
                <span class="label-with-tip">
                  测试数据 zip（可选）
                  <el-tooltip
                    placement="top"
                    :content="mode === 'create'
                      ? '不上传则创建暂无数据的题目：题面立即可用、暂不可提交'
                      : '留空保留现有数据；选择新 zip 将整包替换（旧数据不保留）'"
                  >
                    <el-icon class="dim"><QuestionFilled /></el-icon>
                  </el-tooltip>
                </span>
                <el-button
                  v-if="canDownloadData"
                  type="primary"
                  plain
                  size="small"
                  title="下载该题测试数据包（含造数据脚本等存档文件）"
                  @click.stop="downloadData"
                >
                  下载数据
                </el-button>
              </div>
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
                <p>{{ mode === 'create' ? '拖入 zip 或点击选择' : '拖入新 zip 整包替换' }}</p>
                <p class="dim">结构：test1.in、test1.ans/out</p>
              </div>
            </el-upload>
          </el-form-item>
          <el-form-item v-if="form.type !== 'objective'">
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
          <!-- 难度（v0.11）：1–10 档，学生可见并用于等级经验与筛选；锚点定标见 tooltip -->
          <el-form-item>
            <template #label>
              <span class="label-with-tip">
                难度
                <el-tooltip
                  placement="top"
                  content="D1：入门，D2：基础，D3：算法入门，D4：算法基础，D5：J组，D6：J组+，D7：S组，D8：S组+，D9：NOIP，D10：NOI"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </span>
            </template>
            <div class="difficulty-row">
              <el-select v-model="form.difficulty" class="difficulty-select">
                <el-option v-for="d in 10" :key="d" :value="d" :label="levels.difficultyNameOf(d)" />
              </el-select>
              <span class="dim">{{ DIFFICULTY_HINTS[form.difficulty] ?? '' }}</span>
            </div>
          </el-form-item>
          <!-- 说明框：占位留位，内容为后续功能（待定） -->
          <el-form-item>
            <div class="field-group">
              <div class="field-label">说明</div>
              <div class="pf-note dim">来源/年份/地市，用,分隔</div>
            </div>
          </el-form-item>
          <el-form-item v-if="form.type !== 'objective'">
            <div class="field-group">
              <div class="field-label">时限 / 内存</div>
              <div class="limits">
                <el-input-number v-model="timeLimitSec" :controls="false" :min="0.1" :max="600" :step="0.1" />
                <span class="dim">s</span>
                <el-input-number v-model="form.memory_limit_mb" :controls="false" :min="16" :max="1024" :step="16" />
                <span class="dim">MB</span>
              </div>
            </div>
          </el-form-item>
          <el-form-item v-if="form.type !== 'objective'">
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
          <el-form-item v-if="form.type !== 'objective' && form.compare === 'tolerant'" label="浮点容差" class="plain-label">
            <el-input-number v-model="form.float_tolerance" :min="0.000001" :max="1" :step="0.0001" :precision="6" />
          </el-form-item>
          <el-form-item v-if="mode === 'create' && form.type !== 'objective'" class="plain-label">
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
          <!-- 函数题（v0.9.1）：driver/template 上传（创建模式随表单一次交齐；编辑页走即时上传） -->
          <template v-if="form.type === 'function' && mode === 'create'">
            <el-form-item class="plain-label">
              <template #label>
                <span class="label-with-tip">
                  驱动器 driver.cpp
                  <el-tooltip placement="top" content="含 main 与函数原型声明、调用学生实现的函数；与学生提交的代码合编成一个程序评测。也可放进 zip 根目录">
                    <el-icon class="dim"><QuestionFilled /></el-icon>
                  </el-tooltip>
                </span>
              </template>
              <el-upload
                v-model:file-list="driverList"
                accept=".cpp"
                :limit="1"
                :auto-upload="false"
              >
                <el-button>选择 driver.cpp</el-button>
              </el-upload>
            </el-form-item>
            <el-form-item class="plain-label">
              <template #label>
                <span class="label-with-tip">
                  学生模板 template.cpp（可选）
                  <el-tooltip placement="top" content="学生提交时代码编辑器预填的内容（函数签名与注释提示）">
                    <el-icon class="dim"><QuestionFilled /></el-icon>
                  </el-tooltip>
                </span>
              </template>
              <el-upload
                v-model:file-list="templateList"
                accept=".cpp"
                :limit="1"
                :auto-upload="false"
              >
                <el-button>选择 template.cpp</el-button>
              </el-upload>
            </el-form-item>
          </template>

          <!-- 交互题（v0.9.2）：interactor 上传（创建模式随表单；编辑页走即时上传） -->
          <el-form-item v-if="form.type === 'interactive' && mode === 'create'" class="plain-label">
            <template #label>
              <span class="label-with-tip">
                交互器 interactor.cpp
                <el-tooltip
                  placement="top"
                  content="读 argv[1] 指定的输入文件，通过 stdin/stdout 与选手程序双向通信；退出码 0 = 该测试点通过，非 0 = 不通过（stderr 末行作为反馈）。也可放进 zip 根目录"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </span>
            </template>
            <el-upload v-model:file-list="interactorList" accept=".cpp" :limit="1" :auto-upload="false">
              <el-button>选择 interactor.cpp</el-button>
            </el-upload>
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
/* 客观题编辑（v0.9） */
.type-lock {
  margin-left: 10px;
}

.obj-options,
.obj-blanks {
  width: 100%;
}

.obj-opt-row,
.obj-blank-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  width: 100%;
}

.obj-blank-idx {
  flex: 0 0 auto;
  color: var(--el-text-color-secondary);
}

.obj-blank-ops {
  display: flex;
  align-items: center;
  gap: 16px;
}

/* zip 上传行：左「测试数据 zip（可选）」标签，右「下载数据」按钮（v0.8.3） */
.zip-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
}

/* 表单标签不加粗（Ant：标签与正文同字重 400，颜色不降档） */
:deep(.el-form-item__label) {
  font-weight: var(--soj-fw-regular);
  color: var(--el-text-color-primary);
}

/* 主/右两列：结构对齐 PageShell（1440 + 右列 320）；el-form 跨列保统一校验 */
.pf-layout {
  max-width: 1440px;
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
  flex: 0 0 320px;
  width: 320px;
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
  font-weight: var(--soj-fw-regular);
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

.difficulty-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.difficulty-select {
  width: 96px;
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
  font-weight: var(--soj-fw-regular);
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

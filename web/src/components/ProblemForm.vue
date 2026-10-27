<script setup lang="ts">
// 建题/编辑题目共用表单：字段、布局与校验单一来源。
// 布局为单 el-form 跨主/右两列（统一校验与 payload），卡片只是字段分组容器：
// 主列三卡 = 题目身份 / 分类（知识点·来源·难度页签三合一，直接点选）/ 题面；
// 右列三卡 = 发布（含完成度清单）/ 测试数据 / 评测参数；<992px 折单列。
// 视觉基准 private_doc/preview/soj-problem-form-redesign.html。
// 提交动作 emit 给调用方——新增 = POST /problems 一次交齐；
// 编辑 = statement / meta / testdata 分步（编辑模式的 zip 为可选的整包替换）。
import { computed, reactive, ref } from 'vue'
import { QuestionFilled, UploadFilled } from '@element-plus/icons-vue'
import type { UploadUserFile } from 'element-plus'
import { ElMessage } from 'element-plus'

import { downloadFile } from '@/api/download'

import MdEditor from '@/components/MdEditor.vue'
import ObjectiveEditor from '@/components/ObjectiveEditor.vue'
import TierBadge from '@/components/TierBadge.vue'
import TagFilter from '@/components/problem/TagFilter.vue'
import { useLevelsStore } from '@/stores/levels'
import { useMetaTreeStore } from '@/stores/metaTree'
import { useTagTreeStore } from '@/stores/tagtree'
import { MAX_CONTENT_BYTES, contentBytes } from '@/utils/md'
import { useUserStore } from '@/stores/user'
import type { ObjectiveKind, ObjectiveQuestionDraft } from '@/types'

export interface ProblemFormPayload {
  meta_tag_ids?: number[] // 来源标签树节点（2026-10-25 双树）
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
  challenge_time_sec: number // 挑战时长秒（v0.23；0 = 按难度×5min 默认）
  video_url: string // 答案解析视频链接（v0.23；B 站/直链 mp4，可空）
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
      meta_tag_ids?: number[] // 来源标签树节点（2026-10-25 双树，编辑回填）
    }
    fileIOHint?: { input: string; output: string } // 编辑模式：现行文件 IO 实际文件名
    pid?: number // 编辑模式：题目 ID（「下载数据」按钮拼下载地址用）
    hasData?: boolean // 编辑模式：题目已配置测试数据（绿勾提示 + 「下载数据」入口）
  }>(),
  {
    saving: false,
    confirmText: '确认创建',
    initial: () => ({}),
    fileIOHint: undefined,
    pid: undefined,
    hasData: false,
  },
)

const emit = defineEmits<{
  (e: 'submit', p: ProblemFormPayload): void
  (e: 'cancel'): void
}>()

// initial 仅在挂载时读取一次；编辑页以路由承载，重进即重挂
const userStore = useUserStore()

// 下载数据：有「下载数据」权限位且已有题目（编辑模式）时展示于测试数据卡头右侧
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
  meta_tag_ids: props.initial.meta_tag_ids ?? [],
  time_limit_ms: props.initial.time_limit_ms ?? 1000,
  memory_limit_mb: props.initial.memory_limit_mb ?? 256,
  compare: props.initial.compare ?? 'exact',
  float_tolerance: props.initial.float_tolerance ?? 0.0001,
  statement: props.initial.statement ?? '',
  file_io_name: props.initial.file_io_name ?? '',
  tag_ids: props.initial.tag_ids ?? [],
  challenge_time_sec: props.initial.challenge_time_sec ?? 0,
  video_url: props.initial.video_url ?? '',
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
// 来源标签树（分类页签「来源」与卡底摘要的名字解析）
const metaStore = useMetaTreeStore()
metaStore.ensure()
// 空树时展示「管理员尚未配置标签树」占位
const tagTreeEmpty = computed(() => !tagStore.nodes.length)
// 标签必填（建题同标题必填口径）；空树无从选择不拦截，编辑存量无标签题也不强制补挂
const tagsRequired = computed(() => props.mode === 'create' && !tagTreeEmpty.value)

// 分类页签：知识点 / 来源 / 难度 三合一入口，一次只展开一个选择器
const catTab = ref<'knowledge' | 'meta' | 'difficulty'>('knowledge')

const knowledgeNames = computed(() => form.tag_ids.map((id) => tagStore.nameOf(id)))
const metaNames = computed(() => form.meta_tag_ids.map((id) => metaStore.nameOf(id)))
const difficultyName = computed(() => levels.difficultyNameOf(form.difficulty))
// 难度提示（2026-10-26）：读后台「难度等级」逐档说明（与题库说明卡同源），不再本地写死；空档不显示
const difficultyHint = computed(() => levels.difficultyNoteOf(form.difficulty))

// 标签卡片流点选回调：整体替换 tag_ids（数组顺序即点选顺序）
function onTagsChange(ids: number[]) {
  form.tag_ids = ids
}

// 来源打标点选回调（2026-10-25 双树）：整体替换 meta_tag_ids
function onMetaTagsChange(ids: number[]) {
  form.meta_tag_ids = ids
}

// 发布卡完成度清单（只做展示，拦截仍以 payload() 校验为准）
const catReady = computed(() => !tagsRequired.value || form.tag_ids.length > 0)
const statementReady = computed(() =>
  form.type === 'objective' ? objQuestions.value.length > 0 : !!form.statement.trim(),
)
const statementNote = computed(() =>
  form.type === 'objective'
    ? `客观题 · 小题 ${objQuestions.value.length} 道`
    : form.statement.trim()
      ? `题面已编辑（约 ${form.statement.length} 字）`
      : '题面未填写',
)

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
    ElMessage.warning('请至少选择一个知识点')
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
    meta_tag_ids: [...form.meta_tag_ids],
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
        <!-- ① 题目身份 -->
        <el-card class="form-card">
          <div class="card-head">
            <span class="card-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><circle cx="12" cy="12" r="9" /><path d="M12 8v4l2.5 2.5" /></svg>
            </span>
            <div class="card-head-text"><div class="card-title">题目身份</div></div>
          </div>

          <el-form-item label="标题" required>
            <el-input v-model="form.title" maxlength="64" show-word-limit placeholder="64 字符以内" />
          </el-form-item>

          <el-form-item>
            <template #label>
              <span class="label-with-tip">
                题目类型
                <el-tooltip placement="top" content="传统题=编程评测；客观题=选择/填空；函数题=补全函数；交互题=双向通信。创建后不可更改">
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
          </el-form-item>
        </el-card>

        <!-- ② 分类：知识点 / 来源 / 难度 三合一，页签切换直接点选 -->
        <el-card class="form-card">
          <div class="card-head">
            <span class="card-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M20 12l-8.5 8.5a2 2 0 01-2.8 0L3 14.8a2 2 0 010-2.8L11.5 3.5H17l3 3z" /><circle cx="15.5" cy="8.5" r="1.3" /></svg>
            </span>
            <div class="card-head-text">
              <div class="card-title">
                分类
                <el-tooltip placement="top" content="知识点 / 难度影响推荐与经验；来源用于筛选与试卷打印">
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </div>
            </div>
          </div>

          <div class="cat-tabs">
            <button type="button" class="cat-tab" :class="{ on: catTab === 'knowledge' }" @click="catTab = 'knowledge'">
              <span v-if="tagsRequired" class="req">*</span>知识点
              <span v-if="form.tag_ids.length" class="n">{{ form.tag_ids.length }}</span>
            </button>
            <button type="button" class="cat-tab" :class="{ on: catTab === 'meta' }" @click="catTab = 'meta'">
              来源
              <span v-if="form.meta_tag_ids.length" class="n">{{ form.meta_tag_ids.length }}</span>
            </button>
            <button type="button" class="cat-tab" :class="{ on: catTab === 'difficulty' }" @click="catTab = 'difficulty'">
              难度
              <span class="n">{{ difficultyName }}</span>
            </button>
          </div>

          <div class="cat-pane">
            <template v-if="catTab === 'knowledge'">
              <div v-if="!tagTreeEmpty" class="tag-bar">
                <TagFilter :selected="form.tag_ids" @change="onTagsChange" />
              </div>
              <span v-else class="dim">管理员尚未配置知识树（后台管理 → 知识树）</span>
            </template>
            <template v-else-if="catTab === 'meta'">
              <TagFilter tree="meta" :selected="form.meta_tag_ids" @change="onMetaTagsChange" />
            </template>
            <template v-else>
              <div class="diff-chips">
                <TierBadge
                  v-for="d in 10"
                  :key="d"
                  :tier="d"
                  kind="difficulty"
                  size="md"
                  class="diff-chip"
                  :class="{ sel: form.difficulty === d }"
                  @click="form.difficulty = d"
                />
              </div>
              <div v-if="difficultyHint" class="hint">{{ difficultyHint }}</div>
            </template>
          </div>

          <div class="cat-summary">
            <span class="dim">已选：</span>
            <span v-if="form.tag_ids.length" class="sum-item"><i class="sum-dot" style="background: var(--el-color-primary)" />知识点：{{ knowledgeNames.join(' · ') }}</span>
            <span v-if="form.meta_tag_ids.length" class="sum-item"><i class="sum-dot" style="background: #0e7a9c" />来源：{{ metaNames.join(' · ') }}</span>
            <span class="sum-item"><i class="sum-dot" style="background: #d99141" />难度：{{ difficultyName }}</span>
          </div>
        </el-card>

        <!-- ③ 题面 -->
        <el-card class="form-card">
          <div class="card-head">
            <span class="card-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M4 6h16M4 12h16M4 18h10" /></svg>
            </span>
            <div class="card-head-text">
              <div class="card-title">
                {{ form.type === 'objective' ? '整卷说明' : '题面' }}
                <span v-if="form.type !== 'objective'" class="req">*</span>
                <el-tooltip
                  placement="top"
                  :content="form.type === 'objective'
                    ? '可选；小题的题干在下方小题列表里填写'
                    : 'Markdown 编辑，可直接粘贴/拖入图片，公式用 $$…$$'"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </div>
            </div>
          </div>

          <el-form-item>
            <MdEditor v-model="form.statement" auto-height />
          </el-form-item>

          <!-- 客观题整卷（v0.9.3）：小题列表（增删/排序 + 每题题干/选项/答案/解析/分值） -->
          <el-form-item v-if="form.type === 'objective'">
            <ObjectiveEditor ref="objEditor" v-model="objQuestions" :pid="pid" />
          </el-form-item>
        </el-card>
      </div>

      <div class="pf-aside">
        <!-- 发布 -->
        <el-card class="form-card">
          <div class="card-head">
            <span class="card-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M5 12l5 5L20 7" /></svg>
            </span>
            <div class="card-head-text"><div class="card-title">发布</div></div>
          </div>

          <div class="entry-actions">
            <el-button type="primary" style="width: 100%" :loading="saving" @click="onSubmitClick">
              {{ confirmText }}
            </el-button>
            <el-button style="width: 100%" @click="emit('cancel')">取消</el-button>
          </div>

          <div class="pub-check">
            <div class="row">
              <span v-if="form.title.trim() && catReady" class="ok">✓</span>
              <span v-else class="todo">!</span>
              {{ form.title.trim() && catReady ? '标题 / 题型 / 分类已齐' : '标题或知识点待补全' }}
            </div>
            <div class="row">
              <span v-if="statementReady" class="ok">✓</span>
              <span v-else class="todo">–</span>
              {{ statementNote }}
            </div>
            <div v-if="form.type !== 'objective'" class="row">
              <span v-if="hasData || fileList.length" class="ok">✓</span>
              <span v-else class="todo">–</span>
              {{ hasData ? '已有测试数据，新 zip 可整包替换' : fileList.length ? '新测试数据已选择' : '测试数据未上传——可先创建' }}
            </div>
          </div>
        </el-card>

        <!-- 测试数据 -->
        <el-card v-if="form.type !== 'objective'" class="form-card">
          <div class="card-head">
            <span class="card-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M21 8v11a2 2 0 01-2 2H5a2 2 0 01-2-2V8m18 0l-2-4H5L3 8m18 0H3m9 4v6m0-6l-3 3m3-3l3 3" /></svg>
            </span>
            <div class="card-head-text">
              <div class="card-title">
                测试数据
                <el-tooltip
                  placement="top"
                  :content="mode === 'create'
                    ? '不上传可先创建；zip 里 data/ 下的数据对保持原名'
                    : '新 zip 整包替换数据与配置；zip 里 data/ 下的数据对保持原名'"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </div>
            </div>
          </div>

          <!-- 已有数据状态行：绿勾 + 「下载数据」与此行平齐 -->
          <div v-if="hasData" class="data-status">
            <span class="ok">✓</span>已有测试数据
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

          <el-form-item>
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
                <span class="zip-badge"><el-icon><UploadFilled /></el-icon></span>
                <p>{{ mode === 'create' ? '拖入 zip 或点击选择' : '拖入新 zip 整包替换' }}</p>
              </div>
            </el-upload>
          </el-form-item>
        </el-card>

        <!-- 评测参数 -->
        <el-card v-if="form.type !== 'objective'" class="form-card">
          <div class="card-head">
            <span class="card-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 3" /></svg>
            </span>
            <div class="card-head-text"><div class="card-title">评测参数</div></div>
          </div>

          <el-form-item label="时限 / 内存">
            <div class="limits">
              <el-input-number v-model="timeLimitSec" :controls="false" :min="0.1" :max="600" :step="0.1" />
              <span class="dim">s</span>
              <el-input-number v-model="form.memory_limit_mb" :controls="false" :min="16" :max="1024" :step="16" />
              <span class="dim">MB</span>
            </div>
          </el-form-item>

          <el-form-item>
            <div class="field-group">
              <div class="field-label">
                文件名（可选）
                <el-tooltip
                  placement="top"
                  content="仅考试启用文件 IO 时生效；留空默认 problem"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </div>
              <el-input v-model="form.file_io_name" placeholder="problem" maxlength="32" class="fileio-input" />
              <span v-if="fileIOHint" class="dim fileio-now">
                当前文件IO：{{ fileIOHint.input.replace(/\.in$/, '') }}
              </span>
              <span v-else-if="form.file_io_name.trim()" class="dim fileio-now">
                将使用文件IO：{{ form.file_io_name.trim() }}
              </span>
            </div>
          </el-form-item>

          <el-form-item>
            <template #label>
              <span class="label-with-tip">
                比对模式
                <el-tooltip
                  placement="top"
                  content="全文比对：输出与答案完全一致；宽容比对：忽略空白差异，数字允许小误差"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </span>
            </template>
            <el-radio-group v-model="form.compare">
              <el-radio-button value="exact">全文比对</el-radio-button>
              <el-radio-button value="tolerant">宽容比对</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="form.compare === 'tolerant'" label="浮点容差">
            <el-input-number v-model="form.float_tolerance" :min="0.000001" :max="1" :step="0.0001" :precision="6" />
          </el-form-item>

          <el-form-item>
            <template #label>
              <span class="label-with-tip">
                挑战时长（可选）
                <el-tooltip
                  placement="top"
                  content="挑战模式的限时；留空按难度默认（难度 × 5 分钟），可配 60–14400 秒"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </span>
            </template>
            <div class="limits">
              <el-input-number
                v-model="form.challenge_time_sec"
                :controls="false"
                :min="0"
                :max="14400"
                :step="60"
                placeholder="默认"
              />
              <span class="dim">秒（0 = 按难度默认）</span>
            </div>
          </el-form-item>

          <el-form-item>
            <template #label>
              <span class="label-with-tip">
                视频讲解链接（可选）
                <el-tooltip
                  placement="top"
                  content="答案解析卡的视频：B 站视频页链接或 mp4 直链；与题解同门槛（满分可见）"
                >
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </span>
            </template>
            <el-input
              v-model="form.video_url"
              placeholder="https://www.bilibili.com/video/BV… 或 https://…/solution.mp4"
              maxlength="1024"
            />
          </el-form-item>

          <el-form-item v-if="mode === 'create'">
            <template #label>
              <span class="label-with-tip">
                自定义校验器（可选）
                <el-tooltip placement="top" content="配置后判题由校验器决定；细节可在题目页调整">
                  <el-icon class="dim"><QuestionFilled /></el-icon>
                </el-tooltip>
              </span>
            </template>
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
          </el-form-item>

          <!-- 函数题（v0.9.1）：driver/template 上传（创建模式随表单一次交齐；编辑页走即时上传） -->
          <template v-if="form.type === 'function' && mode === 'create'">
            <el-form-item>
              <template #label>
                <span class="label-with-tip">
                驱动器 driver.cpp
                <el-tooltip placement="top" content="含 main，调用学生实现的函数；也可放进 zip 根目录">
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
            <el-form-item>
              <template #label>
                <span class="label-with-tip">
                  学生模板 template.cpp（可选）
                  <el-tooltip placement="top" content="学生提交时编辑器预填的代码">
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
          <el-form-item v-if="form.type === 'interactive' && mode === 'create'">
            <template #label>
              <span class="label-with-tip">
                交互器 interactor.cpp
                <el-tooltip
                  placement="top"
                  content="与选手程序双向通信判题；也可放进 zip 根目录"
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
  gap: 20px;
  align-items: flex-start;
}

.pf-main {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.pf-aside {
  flex: 0 0 320px;
  width: 320px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  position: sticky;
  top: 84px;
}

/* 纵向按钮组：EP 默认给相邻按钮 margin-left: 12px，纵向排列会把下面的按钮顶离中线 */
.pf :deep(.entry-actions .el-button + .el-button) {
  margin-left: 0;
}

/* ---- 卡片头（图标 + 标题 + 副标题，preview 基准样式）---- */
.card-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.card-ico {
  width: 30px;
  height: 30px;
  border-radius: 8px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  display: grid;
  place-items: center;
  flex: none;
}

.card-ico svg {
  width: 16px;
  height: 16px;
}

.card-title {
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-navy);
}

.card-head .right {
  margin-left: auto;
}

.req {
  color: var(--el-color-danger);
}

/* ---- 分类页签（知识点 / 来源 / 难度 三合一）---- */
.cat-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
  background: var(--el-fill-color-light);
  border: 1px solid var(--soj-line);
  border-radius: 999px;
  padding: 3px;
  margin-bottom: 14px;
}

.cat-tab {
  border: none;
  background: transparent;
  padding: 7px 16px;
  border-radius: 999px;
  font-size: var(--soj-fs-14);
  color: var(--el-text-color-regular);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  transition: background 0.15s, color 0.15s;
}

.cat-tab:hover {
  color: var(--el-color-primary);
}

.cat-tab.on {
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-blue-lo));
  color: #fff;
  font-weight: var(--soj-fw-emphasis);
  box-shadow: 0 2px 8px rgb(var(--soj-blue-rgb) / 0.28);
}

.cat-tab .n {
  font-size: var(--soj-fs-small);
  border-radius: 999px;
  padding: 1px 8px;
  font-weight: var(--soj-fw-bold);
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.cat-tab.on .n {
  background: rgba(255, 255, 255, 0.85);
  color: var(--el-color-primary);
}

.cat-pane {
  min-width: 0;
}

/* 卡底已选摘要：三路状态常驻可见 */
.cat-summary {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px dashed var(--soj-line-2);
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: var(--soj-fs-13);
}

.sum-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: var(--el-fill-color-light);
  border: 1px solid var(--soj-line);
  border-radius: 999px;
  padding: 3px 11px;
  color: var(--el-text-color-regular);
}

.sum-dot {
  width: 7px;
  height: 7px;
  border-radius: 2px;
  transform: rotate(45deg);
  display: inline-block;
}

/* 难度十档胶囊（题库「难度」筛选同款，TierBadge 色随档位；单选） */
.diff-chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.diff-chip {
  cursor: pointer;
  transition:
    transform 0.15s,
    box-shadow 0.15s,
    background-color 0.15s,
    border-color 0.15s;
}

.diff-chip:hover {
  transform: translateY(-1px);
}

.diff-chip.sel.diff-tag {
  background: color-mix(in srgb, var(--dc) 20%, #ffffff);
  border-color: var(--dc);
  box-shadow: inset 0 0 0 1px var(--dc);
}

.hint {
  font-size: var(--soj-fs-small);
  color: var(--el-text-color-secondary);
  margin-top: 8px;
  line-height: 1.6;
}

/* 发布卡完成度清单 */
.pub-check {
  margin: 14px 0 2px;
  display: grid;
  gap: 8px;
}

.pub-check .row {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-regular);
}

.pub-check .ok {
  width: 17px;
  height: 17px;
  border-radius: 50%;
  background: var(--el-color-success-light-8);
  color: var(--el-color-success);
  display: grid;
  place-items: center;
  font-size: 11px;
  font-weight: var(--soj-fw-bold);
  flex: none;
}

.pub-check .todo {
  width: 17px;
  height: 17px;
  border-radius: 50%;
  border: 1.5px dashed var(--el-text-color-secondary);
  color: var(--el-text-color-secondary);
  display: grid;
  place-items: center;
  font-size: 10px;
  flex: none;
}

/* 标签栏：与题库筛选区同底色/圆角/内距 */
.tag-bar {
  box-sizing: border-box;
  width: 100%;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: var(--el-border-radius-base);
}

.field-group {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0;
}

.field-label {
  font-weight: var(--soj-fw-regular);
  color: var(--el-text-color-primary);
  margin-bottom: 8px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

/* 文件名输入限宽（右列单列排布） */
.fileio-input {
  max-width: 280px;
}

.fileio-now {
  white-space: nowrap; /* 防止提示文字换行 */
  font-size: var(--soj-fs-small);
  margin-top: 6px;
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
  /* 虚线加深到可见（EP 默认太淡）+ 悬停主蓝 */
  border: 1.5px dashed rgb(var(--soj-blue-rgb) / 0.38);
}

.zip-upload :deep(.el-upload-dragger:hover) {
  border-color: var(--el-color-primary);
}

/* 拖放区上传图标徽章（预览基准样式）：一眼可识别「这里是传 zip 的地方」 */
.zip-badge {
  width: 40px;
  height: 40px;
  margin: 0 auto 8px;
  border-radius: 10px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  display: grid;
  place-items: center;
}

.zip-badge .el-icon {
  font-size: 24px;
}

.zip-hint p:first-of-type {
  font-size: var(--soj-fs-14);
  color: var(--el-text-color-primary);
}

/* 已有测试数据状态行：绿勾与「下载数据」按钮同一行 */
.data-status {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: var(--soj-fs-13);
  color: var(--el-text-color-regular);
  margin-bottom: 12px;
}

.data-status .ok {
  width: 17px;
  height: 17px;
  border-radius: 50%;
  background: var(--el-color-success-light-8);
  color: var(--el-color-success);
  display: grid;
  place-items: center;
  font-size: 11px;
  font-weight: var(--soj-fw-bold);
  flex: none;
}

.data-status .el-button {
  margin-left: auto;
}

/* label 插槽内的图标/提示与标签文字垂直居中（label 为块级布局，行内元素默认按基线对齐会偏高） */
.label-with-tip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

@media (max-width: 992px) {
  .pf-layout {
    flex-direction: column;
  }

  .pf-aside {
    width: auto;
    flex: 1 1 auto;
    position: static;
  }
}
</style>

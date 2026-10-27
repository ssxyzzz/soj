<script setup lang="ts">
// 比赛/考试编辑弹窗（v0.7；v0.7.4 说明与三个场景开关并入完整模式，「编辑说明」入口取消）：
// - 完整模式 = 名称/起止时间/说明/文件 IO·成绩表·赛时隐藏成绩开关，一次保存；
// - problems 模式 = 「调整题目」专用（题库选题 + 拖动排序 + 删除）。
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { contestApi, examApi, type RoundKind } from '@/api'
import MdEditor from '@/components/MdEditor.vue'
import ProblemPicker from '@/components/ProblemPicker.vue'
import SwitchRow from '@/components/SwitchRow.vue'
import { MAX_CONTENT_BYTES, contentBytes } from '@/utils/md'
import type { ContestDetail, PickedProblem } from '@/types'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    kind: RoundKind
    round: ContestDetail
    mode?: 'full' | 'problems'
  }>(),
  { mode: 'full' },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'saved'): void
}>()

const api = computed(() => (props.kind === 'exam' ? examApi : contestApi))
const router = useRouter()
const name = computed(() => (props.kind === 'exam' ? '考试' : '比赛'))
const titleText = computed(() => (props.mode === 'problems' ? `调整${name.value}题目` : `编辑${name.value}`))

const title = ref('')
const range = ref<Date[]>([])
const intro = ref('')
const fileIO = ref(false)
const scoreboard = ref(true)
const hideDuring = ref(true)
// 赛时隐藏难度与知识点（2026-10-27）：比赛场景开关（考试不提供）
const hideMeta = ref(true)
const editList = ref<PickedProblem[]>([])
const saving = ref(false)

function reset() {
  title.value = props.round.title
  range.value = [new Date(props.round.start), new Date(props.round.end)]
  intro.value = props.round.intro || ''
  fileIO.value = !!props.round.file_io
  scoreboard.value = !!props.round.scoreboard
  hideDuring.value = !!props.round.hide_during
  hideMeta.value = props.round.hide_meta ?? false
  const source = props.round.edit_problems ?? props.round.problems ?? []
  editList.value = source.map((p) => ({ id: p.id, title: p.title, hidden: 'hidden' in p ? p.hidden : false }))
}

// 全量编辑只有「时间」会影响赛中作答/榜单；改名/说明/开关不弹确认。
function timeChanged(): boolean {
  if (range.value.length !== 2) return false
  const start = new Date(range.value[0]).getTime()
  const end = new Date(range.value[1]).getTime()
  return start !== new Date(props.round.start).getTime() || end !== new Date(props.round.end).getTime()
}

// 打印试卷入口收进编辑弹窗（v0.10 验收反馈），打印的是已保存内容
function openPaper() {
  const url = router.resolve(`/${props.kind}s/${props.round.id}/paper`).href
  window.open(url, '_blank', 'noopener')
}

async function save() {
  if (props.mode === 'problems') {
    if (!editList.value.length) {
      ElMessage.warning(`${name.value}至少需要一道题目`)
      return
    }
  } else {
    if (!title.value.trim()) {
      ElMessage.warning(`请填写${name.value}名称`)
      return
    }
    if (range.value.length !== 2) {
      ElMessage.warning('请选择起止时间')
      return
    }
    if (contentBytes(intro.value) > MAX_CONTENT_BYTES) {
      ElMessage.error(`${name.value}说明过长（上限 256KB）`)
      return
    }
  }
  const needConfirm = props.round.status !== 'scheduled' && (props.mode === 'problems' || (props.mode === 'full' && timeChanged()))
  if (needConfirm) {
    try {
      await ElMessageBox.confirm(
        props.mode === 'problems'
          ? `${name.value}已开始，修改题目会影响进行中的作答与榜单，确定保存？`
          : `${name.value}已开始，修改起止时间会影响进行中的作答与榜单，确定保存？`,
        '赛中修改确认',
        { type: 'warning', confirmButtonText: '确定保存', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
  }
  saving.value = true
  try {
    const body: {
      title?: string
      start?: string
      end?: string
      intro?: string
      problem_ids?: number[]
      scoreboard?: boolean
      hide_during?: boolean
      hide_meta?: boolean
      file_io?: boolean
    } =
      props.mode === 'problems'
        ? { problem_ids: editList.value.map((p) => p.id) }
        : {
            title: title.value.trim(),
            start: new Date(range.value[0]).toISOString(),
            end: new Date(range.value[1]).toISOString(),
            intro: intro.value,
            scoreboard: scoreboard.value,
            hide_during: hideDuring.value,
            ...(props.kind === 'contest' ? { hide_meta: hideMeta.value } : {}),
            file_io: fileIO.value,
          }
    await api.value.update(props.round.id, body)
    ElMessage.success(`${name.value}已保存`)
    emit('saved')
    emit('update:modelValue', false)
  } finally {
    saving.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="titleText"
    width="960"
    top="5vh"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <template v-if="mode === 'full'">
      <el-form label-position="top">
        <el-form-item :label="`${name}名称`" required>
          <el-input v-model="title" maxlength="64" />
        </el-form-item>
        <el-form-item label="起止时间（最长 7 天）" required>
          <el-date-picker
            v-model="range"
            type="datetimerange"
            start-placeholder="开始"
            end-placeholder="结束"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item :label="`${name}说明`">
          <MdEditor v-model="intro" height="150px" variant="compact" />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="fileIO"
            label="本场启用文件 IO"
            tip="场内所有题按各题文件名对以文件读写判题，题面下会展示文件 IO 说明；改动只影响之后的提交"
          />
        </el-form-item>
        <el-form-item>
          <SwitchRow v-model="scoreboard" label="开启成绩表" tip="关闭后参与者看不到成绩表，组织者仍可见" />
        </el-form-item>
        <el-form-item>
          <SwitchRow
            v-model="hideDuring"
            label="赛时隐藏成绩"
            tip="开启后隐藏期内（赛时到成绩发布前）参赛者看不到状态、标签与通过率；关闭则赛中即可实时查看"
          />
        </el-form-item>
        <el-form-item v-if="kind === 'contest'">
          <SwitchRow
            v-model="hideMeta"
            label="赛时隐藏难度与知识点"
            tip="比赛期间对参赛者隐藏题目难度与知识点（服务端不下发，悬停不可见），题目表与做题页显示横杠；发布后恢复显示，组织者与管理员不受影响"
          />
        </el-form-item>
      </el-form>
    </template>

    <template v-else>
      <ProblemPicker v-model="editList" />
      <div class="dim">拖动 ⠿ 调整顺序，右侧「删除」移除；保存后一次性生效。</div>
    </template>

    <template #footer>
      <div class="dialog-footer">
        <el-button v-if="mode === 'full'" plain @click="openPaper">打印试卷</el-button>
        <span class="footer-spacer" />
        <el-button @click="emit('update:modelValue', false)">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.dialog-footer {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.footer-spacer {
  flex: 1;
}
</style>

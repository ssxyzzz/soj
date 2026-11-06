<script setup lang="ts">
// 发帖/编辑话题弹窗（v0.24）：三区共用一个表单体，按 scope 切类型集合与附加字段。
// 比赛赛时学生提交自动走「仅组织者可见」（服务端权威，前端只提示）。
// 悬赏（v0.33-②）：题内 + 全局可挂（发布即冻结托管，14 天无打赏未退部分自动退回）。
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import MdEditor from '@/components/MdEditor.vue'
import { discussApi, jadeApi, type DiscussTopicBody } from '@/api'
import { useUserStore } from '@/stores/user'
import type { DiscussScope, DiscussTopic } from '@/types'
import { DiscussKindSets } from './discussMeta'

const props = defineProps<{
  scope: DiscussScope
  scopeId?: number
  /** 比赛赛时（学生侧提示用） */
  contestRunning?: boolean
  contestOrganizer?: boolean
  /** 传入则为编辑模式 */
  editTopic?: DiscussTopic | null
}>()

const emit = defineEmits<{ (e: 'done', id: number): void }>()

const visible = defineModel<boolean>({ default: false })
const title = ref('')
const kind = ref('')
const content = ref('')
const relatedInput = ref('')
const submitting = ref(false)
// 悬赏（v0.33-②）：余额够下限才显示输入；0 = 不挂
const userStore = useUserStore()
const bounty = ref(0)
const myBalance = ref(0)
const canBounty = computed(() => props.scope !== 'contest' && !isEdit.value && userStore.token && myBalance.value >= 5)

onMounted(async () => {
  if (!userStore.token) return
  try {
    const m = await jadeApi.mine()
    myBalance.value = m.balance
  } catch {
    /* 静默：拉不到余额就不显示悬赏输入 */
  }
})

const kinds = computed(() => DiscussKindSets[props.scope] || [])
const isEdit = computed(() => !!props.editTopic)
const isProblem = computed(() => props.scope === 'problem')

watch(
  () => visible.value,
  (v) => {
    if (!v) return
    if (props.editTopic) {
      title.value = props.editTopic.title
      kind.value = props.editTopic.kind
      content.value = props.editTopic.content || ''
      relatedInput.value = (props.editTopic.related_pids || []).join(', ')
    } else {
      title.value = ''
      kind.value = ''
      content.value = ''
      relatedInput.value = ''
      bounty.value = 0
    }
  },
)

function parseRelated(): number[] | undefined {
  if (props.scope !== 'global') return undefined
  const pids = relatedInput.value
    .split(/[,，\s]+/)
    .map((s) => Number(s.trim()))
    .filter((n) => Number.isInteger(n) && n > 0)
  return [...new Set(pids)]
}

async function submit() {
  if (!title.value.trim()) {
    ElMessage.warning('请填写标题')
    return
  }
  if (!kind.value) {
    ElMessage.warning('请选择帖子类型')
    return
  }
  if (!content.value.trim()) {
    ElMessage.warning('请填写内容')
    return
  }
  submitting.value = true
  try {
    const body: DiscussTopicBody = {
      title: title.value.trim(),
      kind: kind.value,
      content: content.value,
    }
    const related = parseRelated()
    if (related) body.related_pids = related
    if (!isEdit.value && canBounty.value && bounty.value > 0) body.bounty = bounty.value
    if (isEdit.value && props.editTopic) {
      await discussApi.update(props.editTopic.id, body)
      ElMessage.success('已保存')
      emit('done', props.editTopic.id)
    } else if (props.scope === 'global') {
      const r = await discussApi.createGlobal(body)
      ElMessage.success('发布成功')
      emit('done', r.id)
    } else if (props.scope === 'problem') {
      const r = await discussApi.createProblem(props.scopeId!, body)
      ElMessage.success('发布成功')
      emit('done', r.id)
    } else {
      const r = await discussApi.createContest(props.scopeId!, body)
      ElMessage.success(r.private ? '提问已提交（仅组织者可见）' : '发布成功')
      emit('done', r.id)
    }
    visible.value = false
  } catch {
    // 拦截器已提示
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    width="760px"
    top="5vh"
    :close-on-click-modal="false"
    destroy-on-close
  >
    <template #header>
      <TipLabel
        :label="isEdit ? '编辑话题' : '发起讨论'"
        :tip="isProblem ? '题内讨论仅交流思路，完整 AC 代码请发布题解；不支持图片' : undefined"
      />
    </template>
    <el-alert
      v-if="scope === 'contest' && contestRunning && !contestOrganizer"
      title="比赛进行中：你的提问仅组织者可见，组织者可选择公开答复"
      type="info"
      :closable="false"
      class="post-tip"
    />
    <div class="post-row">
      <el-input v-model="title" maxlength="80" show-word-limit placeholder="标题（一句话说清主题）" />
      <el-select v-model="kind" placeholder="类型" style="width: 130px" shrink="none">
        <el-option v-for="k in kinds" :key="k.value" :value="k.value" :label="k.label" />
      </el-select>
    </div>
    <div v-if="scope === 'global'" class="post-row">
      <el-input v-model="relatedInput" placeholder="相关题号（可选，逗号分隔，如 12, 35）" />
    </div>
    <div v-if="canBounty" class="post-row bounty-row">
      <TipLabel label="悬赏" tip="发布即冻结等额玉，打赏给有帮助的回复，14 天无打赏自动退回；0 = 不挂悬赏" />
      <el-input-number
        v-model="bounty"
        :min="0"
        :max="Math.min(myBalance, 500)"
        :step="5"
        style="width: 160px"
      />
      <span class="post-hint" style="margin-top: 0">玉 · 余额 {{ myBalance }}</span>
    </div>
    <MdEditor v-model="content" :height="isProblem ? '320px' : '380px'" variant="compact" />
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="submit">
        {{ isEdit ? '保存' : '发布' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.post-tip {
  margin-bottom: var(--soj-sp-12);
}
.post-row {
  display: flex;
  gap: var(--soj-sp-10);
  margin-bottom: var(--soj-sp-12);
}
.post-row > .el-input {
  flex: 1;
}
.post-hint {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  margin-top: var(--soj-sp-8);
}
.bounty-row {
  align-items: center;
}
.bounty-label {
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  white-space: nowrap;
}
</style>

<script setup lang="ts">
// 话题详情（v0.24）：主楼 + 两层楼中楼回复 + 点赞/采纳/举报/管理动作 + 题内剧透确认层。
// 题内代码块由服务端按分数门槛裁剪（未达标视角收到占位提示），前端只做确认层与展示。
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import MdEditor from '@/components/MdEditor.vue'
import MdView from '@/components/MdView.vue'
import { discussApi } from '@/api'
import { useUserStore } from '@/stores/user'
import { fmtRel } from '@/utils/format'
import type { DiscussReply, DiscussTopic } from '@/types'
import { discussKindLabel } from './discussMeta'
import DiscussPostDialog from './DiscussPostDialog.vue'

const props = defineProps<{
  tid: number
  /** 弹窗内嵌时隐藏面包屑（返回列表由容器控制） */
  embedded?: boolean
}>()

const emit = defineEmits<{ (e: 'changed'): void }>()

const userStore = useUserStore()
const loading = ref(false)
const topic = ref<DiscussTopic | null>(null)
const replies = ref<DiscussReply[]>([])
const gate = ref<{ code_threshold: number; can_view_code: boolean } | null>(null)
const spoilerConfirmed = ref(false)

// 主楼分页（v0.26）：顶层回复按页加载（楼中楼随本页主楼带出），replies 仍是扁平列表
const REPLY_PAGE_SIZE = 20
const replyPage = ref(1)
const replyTotal = ref(0)

// 回复框：replyTarget = 0 回主楼，否则楼中楼（回复某条顶层回复）
const replyTarget = ref(0)
const replyContent = ref('')
const replySubmitting = ref(false)
const editing = ref(false)

const isProblem = computed(() => topic.value?.scope === 'problem')
const canViewCode = computed(() => gate.value?.can_view_code ?? true)
const threshold = computed(() => gate.value?.code_threshold ?? 0)

// 顶层回复与楼中楼分组（保持楼层序）
const topLevel = computed(() => replies.value.filter((r) => !r.reply_to))
function childrenOf(rid: number): DiscussReply[] {
  return replies.value.filter((r) => r.reply_to === rid)
}

async function load() {
  loading.value = true
  try {
    const r = await discussApi.detail(props.tid, replyPage.value)
    topic.value = r.topic
    replies.value = r.replies
    replyTotal.value = r.reply_total ?? r.replies.length
    gate.value = r.gate || null
    spoilerConfirmed.value = false
    editing.value = false
  } catch {
    topic.value = null
  } finally {
    loading.value = false
  }
}

async function toggleLikeTopic() {
  if (!topic.value) return
  const r = await discussApi.likeTopic(topic.value.id)
  topic.value.liked = r.liked
  topic.value.like_count = r.like_count
}

async function toggleLikeReply(rep: DiscussReply) {
  const r = await discussApi.likeReply(rep.tid, rep.id)
  rep.liked = r.liked
  rep.like_count = r.like_count
}

function startReply(rid = 0) {
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    return
  }
  replyTarget.value = rid
  replyContent.value = ''
}

async function submitReply() {
  if (!topic.value || !replyContent.value.trim()) return
  replySubmitting.value = true
  try {
    await discussApi.reply(topic.value.id, replyContent.value, replyTarget.value)
    ElMessage.success('回复成功')
    replyContent.value = ''
    // 新主楼排在楼层末尾：跳到最后一页看自己的楼层；楼中楼留在本页
    const isNewTop = replyTarget.value === 0
    replyTarget.value = 0
    if (isNewTop) {
      replyPage.value = Math.max(1, Math.ceil((replyTotal.value + 1) / REPLY_PAGE_SIZE))
    }
    await load()
    emit('changed')
  } catch {
    // 拦截器已提示
  } finally {
    replySubmitting.value = false
  }
}

async function accept(rep: DiscussReply) {
  if (!topic.value) return
  try {
    await ElMessageBox.confirm('采纳该回复为本帖答案？', '采纳', {
      confirmButtonText: '采纳',
      cancelButtonText: '取消',
      type: 'info',
    })
  } catch {
    return
  }
  await discussApi.accept(topic.value.id, rep.id)
  ElMessage.success('已采纳')
  await load()
  emit('changed')
}

async function removeTopic() {
  if (!topic.value) return
  try {
    await ElMessageBox.confirm('删除后保留楼层结构（显示「该帖已删除」），确定？', '删除话题', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  await discussApi.remove(topic.value.id)
  ElMessage.success('已删除')
  emit('changed')
  if (props.embedded) {
    await load()
  }
}

async function removeReply(rep: DiscussReply) {
  try {
    await ElMessageBox.confirm('确定删除这条回复？', '删除回复', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  await discussApi.deleteReply(rep.tid, rep.id)
  ElMessage.success('已删除')
  await load()
  emit('changed')
}

async function report(targetType: 'topic' | 'reply', targetId: number) {
  try {
    const { value } = await ElMessageBox.prompt('请填写举报理由（将进入管理队列）', '举报', {
      confirmButtonText: '提交',
      cancelButtonText: '取消',
      inputPlaceholder: '如：灌水 / 广告 / 泄露答案…',
    })
    if (!value || !value.trim()) return
    const r = await discussApi.report(topic.value!.id, targetType, targetId, value.trim())
    ElMessage.success(r.folded ? '已提交举报，该内容已被自动折叠' : '举报已提交，感谢反馈')
  } catch {
    /* 取消 */
  }
}

async function mod(action: 'pin' | 'feature' | 'lock' | 'publish') {
  if (!topic.value) return
  const body: Record<string, boolean> = {}
  if (action === 'pin') body.pinned = !topic.value.pinned
  if (action === 'feature') body.featured = !topic.value.featured
  if (action === 'lock') body.locked = !topic.value.locked
  if (action === 'publish') body.private = false
  await discussApi.mod(topic.value.id, body)
  ElMessage.success('已更新')
  await load()
  emit('changed')
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="dtd">
    <template v-if="topic">
      <!-- 题内剧透确认层（v0.24 口径：文字点一次确认可见，代码块按分数门槛始终隐藏） -->
      <el-alert
        v-if="isProblem && !canViewCode && !spoilerConfirmed"
        type="warning"
        :closable="false"
        class="spoiler"
      >
        <template #title>
          讨论内容可能包含剧透。文字点「继续查看」后可见；代码块需本题最高得分 ≥ {{ threshold }} 分。
        </template>
        <el-button type="primary" size="small" @click="spoilerConfirmed = true">继续查看</el-button>
      </el-alert>

      <template v-else>
        <!-- 主楼 -->
        <div class="dtd-main">
          <div class="dtd-tags">
            <el-tag v-if="topic.pinned" type="danger" size="small" effect="dark" round>置顶</el-tag>
            <el-tag v-if="topic.featured" type="warning" size="small" effect="plain" round>精华</el-tag>
            <el-tag v-if="topic.private" type="info" size="small" effect="plain" round>仅组织者可见</el-tag>
            <el-tag :type="topic.kind ? 'primary' : 'info'" size="small" effect="plain" round>
              {{ discussKindLabel(topic.scope, topic.kind) }}
            </el-tag>
            <el-tag v-if="topic.official" type="success" size="small" effect="plain" round>已官方解答</el-tag>
          </div>
          <h2 class="dtd-title">
            {{ topic.deleted ? '该帖已删除' : topic.title }}
            <span v-if="topic.accepted_reply_id" class="solved">✓已解决</span>
          </h2>
          <div class="dtd-meta">
            <span class="author">{{ topic.nickname || `用户${topic.uid}` }}</span>
            <span>{{ topic.created_at.slice(0, 16).replace('T', ' ') }}</span>
            <span v-if="topic.edited">已编辑</span>
          </div>
          <MdView
            v-if="!topic.deleted"
            :content="topic.content || ''"
            variant="compact"
            auto-height
            class="dtd-content"
          />
          <div v-else class="dtd-deleted-hint">内容已删除，以下回复保留</div>
          <div class="dtd-actions">
            <el-button
              size="small"
              :type="topic.liked ? 'primary' : 'default'"
              :disabled="topic.deleted"
              @click="toggleLikeTopic"
            >
              👍 {{ topic.like_count }}
            </el-button>
            <el-button v-if="!topic.locked && !topic.deleted" size="small" @click="startReply(0)">
              回复
            </el-button>
            <el-button v-if="topic.can_edit" size="small" @click="editing = !editing">编辑</el-button>
            <el-button v-if="topic.can_manage || topic.uid === userStore.uid" size="small" type="danger" plain @click="removeTopic">
              删除
            </el-button>
            <el-button v-if="topic.uid !== userStore.uid" size="small" text @click="report('topic', topic.id)">
              举报
            </el-button>
            <template v-if="topic.can_manage">
              <el-divider direction="vertical" />
              <el-button size="small" text :type="topic.pinned ? 'primary' : ''" @click="mod('pin')">
                {{ topic.pinned ? '取消置顶' : '置顶' }}
              </el-button>
              <el-button size="small" text :type="topic.featured ? 'primary' : ''" @click="mod('feature')">
                {{ topic.featured ? '取消精华' : '加精' }}
              </el-button>
              <el-button size="small" text :type="topic.locked ? 'primary' : ''" @click="mod('lock')">
                {{ topic.locked ? '解锁' : '锁定' }}
              </el-button>
            </template>
            <el-button
              v-if="topic.is_organizer && topic.private"
              size="small"
              text
              type="warning"
              @click="mod('publish')"
            >
              升格为公开答复
            </el-button>
          </div>
          <!-- 编辑弹窗（作者）：复用发帖表单 -->
          <DiscussPostDialog
            v-model="editing"
            :scope="topic.scope"
            :scope-id="topic.scope_id"
            :edit-topic="topic"
            @done="load(); emit('changed')"
          />
        </div>

        <!-- 回复区 -->
        <div class="dtd-replies">
          <div class="dtd-replies-head">
            回复 <span class="n">{{ replyTotal }}</span>
            <span v-if="topic.locked" class="lock-hint">（话题已锁定，不能回复）</span>
          </div>
          <el-empty v-if="!topLevel.length" description="还没有回复" :image-size="60" />
          <div v-for="r in topLevel" :key="r.id" class="dtd-reply">
            <div class="dtd-reply-head">
              <span class="author">{{ r.nickname || `用户${r.uid}` }}</span>
              <el-tag v-if="r.accepted" type="success" size="small" effect="dark" round>✓ 采纳</el-tag>
              <span class="time">{{ fmtRel(r.created_at) }}</span>
              <span v-if="r.edited" class="edited">已编辑</span>
              <span class="grow"></span>
              <el-button text size="small" @click="startReply(r.id)">回复</el-button>
            </div>
            <MdView v-if="!r.deleted" :content="r.content" variant="compact" auto-height class="dtd-reply-content" />
            <div v-else class="dtd-deleted-hint sm">该回复已删除</div>
            <div class="dtd-reply-actions">
              <el-button text size="small" :type="r.liked ? 'primary' : ''" @click="toggleLikeReply(r)">
                👍 {{ r.like_count }}
              </el-button>
              <el-button v-if="topic.can_accept && !r.deleted && topic.accepted_reply_id !== r.id" text size="small" type="success" @click="accept(r)">
                采纳
              </el-button>
              <el-button v-if="r.uid === userStore.uid || topic.can_manage" text size="small" type="danger" plain @click="removeReply(r)">
                删除
              </el-button>
              <el-button v-if="r.uid !== userStore.uid && !r.deleted" text size="small" @click="report('reply', r.id)">
                举报
              </el-button>
            </div>
            <!-- 楼中楼 -->
            <div v-if="childrenOf(r.id).length" class="dtd-nested">
              <div v-for="c in childrenOf(r.id)" :key="c.id" class="dtd-nested-item">
                <div class="dtd-reply-head">
                  <span class="author">{{ c.nickname || `用户${c.uid}` }}</span>
                  <el-tag v-if="c.accepted" type="success" size="small" effect="dark" round>✓</el-tag>
                  <span class="time">{{ fmtRel(c.created_at) }}</span>
                  <span class="grow"></span>
                  <el-button text size="small" :type="c.liked ? 'primary' : ''" @click="toggleLikeReply(c)">
                    👍 {{ c.like_count }}
                  </el-button>
                  <el-button
                    v-if="c.uid === userStore.uid || topic.can_manage"
                    text
                    size="small"
                    type="danger"
                    plain
                    @click="removeReply(c)"
                  >
                    删除
                  </el-button>
                </div>
                <MdView v-if="!c.deleted" :content="c.content" variant="compact" auto-height class="dtd-nested-content" />
                <div v-else class="dtd-deleted-hint sm">该回复已删除</div>
              </div>
            </div>
          </div>
          <!-- 主楼分页（v0.26）：超过一页才出现 -->
          <el-pagination
            v-if="replyTotal > REPLY_PAGE_SIZE"
            :current-page="replyPage"
            :page-size="REPLY_PAGE_SIZE"
            :total="replyTotal"
            layout="prev, pager, next"
            class="dtd-pager"
            @current-change="
              (p: number) => {
                replyPage = p
                load()
              }
            "
          />
        </div>

        <!-- 回复框 -->
        <div v-if="!topic.deleted && !topic.locked" class="dtd-editor">
          <div class="dtd-editor-tip">
            {{ replyTarget ? '回复楼层 #' + replyTarget : '回复本帖' }}
            <el-button v-if="replyTarget" text size="small" @click="replyTarget = 0">改为回复主楼</el-button>
          </div>
          <MdEditor v-model="replyContent" height="180px" variant="compact" />
          <div class="dtd-editor-foot">
            <el-button type="primary" :loading="replySubmitting" @click="submitReply">回复</el-button>
          </div>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.dtd {
  min-height: 160px;
}
.spoiler {
  margin: 12px 0;
}
.spoiler :deep(.el-button) {
  margin-top: 8px;
}
.dtd-tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 6px;
}
.dtd-title {
  font-size: 19px;
  margin: 4px 0 8px;
  display: flex;
  align-items: center;
  gap: 10px;
}
.dtd-title .solved {
  font-size: 13px;
  font-weight: 600;
  color: var(--soj-ok, #16a34a);
}
.dtd-meta {
  display: flex;
  gap: 12px;
  font-size: 12px;
  color: var(--soj-ink-3);
  margin-bottom: 10px;
}
.dtd-meta .author {
  font-weight: var(--soj-fw-emphasis, 600);
  color: var(--soj-ink);
}
.dtd-content {
  border-top: 1px solid var(--soj-line);
  padding-top: 12px;
}
.dtd-deleted-hint {
  color: var(--soj-ink-3);
  font-size: 13px;
  padding: 8px 0;
}
.dtd-deleted-hint.sm {
  padding: 2px 0;
}
.dtd-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: 12px;
  flex-wrap: wrap;
}
.dtd-replies {
  margin-top: 18px;
  border-top: 1px solid var(--soj-line);
  padding-top: 14px;
}
.dtd-replies-head {
  font-weight: var(--soj-fw-emphasis, 600);
  margin-bottom: 12px;
}
.dtd-replies-head .n {
  color: var(--soj-ink-3);
  font-weight: 400;
}
.lock-hint {
  font-size: 12px;
  color: var(--soj-ink-3);
  font-weight: 400;
  margin-left: 8px;
}
.dtd-reply {
  border: 1px solid var(--soj-line);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 10px;
}
/* 主楼分页器：钉死宽度防 flex 祖先被撑开（与全站定宽列同款三件套） */
.dtd-pager {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  justify-content: center;
  margin-top: 4px;
}
.dtd-reply-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.dtd-reply-head .author {
  font-weight: var(--soj-fw-emphasis, 600);
}
.dtd-reply-head .time,
.dtd-reply-head .edited {
  font-size: 12px;
  color: var(--soj-ink-3);
}
.dtd-reply-head .grow {
  flex: 1;
}
.dtd-reply-content {
  margin-top: 6px;
}
.dtd-reply-actions {
  display: flex;
  gap: 2px;
  margin-top: 2px;
}
.dtd-nested {
  margin-top: 8px;
  border-left: 2px solid var(--soj-line);
  padding-left: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.dtd-nested-item {
  background: color-mix(in srgb, var(--soj-line) 28%, transparent);
  border-radius: 8px;
  padding: 8px 10px;
}
.dtd-nested-content {
  margin-top: 4px;
}
.dtd-editor {
  margin-top: 16px;
}
.dtd-editor-tip {
  font-size: 12px;
  color: var(--soj-ink-3);
  margin-bottom: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.dtd-editor-foot {
  margin-top: 10px;
  display: flex;
  justify-content: flex-end;
}
</style>

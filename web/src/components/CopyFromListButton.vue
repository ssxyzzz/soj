<script setup lang="ts">
// 从题单复制（v0.18.3 拍板 7；2026-10-26 扩全范围）：题单 → 章节 → 小节 三级级联，
// 可整单 / 整章 / 指定小节取题——范围由服务端按 chapter/section 参数过滤
//（拍平序、隐藏题一律剔除并计数，与观察者权限不变），组件只管选范围与取数。
// 合并逻辑在父级（emit merge），按题号去重并入已选。
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { listApi } from '@/api'
import type { ListCard, ListChapter } from '@/types'

const emit = defineEmits<{ (e: 'merge', items: { id: number; title: string }[], skippedHidden: number): void }>()

const show = ref(false)
const lists = ref<ListCard[]>([])
const listId = ref<number | null>(null)
const detailLoading = ref(false)
// 范围级联：'' = 上级整体；章节详情随选题单拉取
const chapters = ref<ListChapter[]>([])
const chapterNo = ref('')
const sectionNo = ref('')
const loading = ref(false)

onMounted(async () => {
  try {
    const r = await listApi.all()
    lists.value = r.items.filter((l) => l.problem_count > 0)
  } catch {
    lists.value = []
  }
})

const curChapter = computed(() => chapters.value.find((c) => c.no === chapterNo.value) ?? null)

// 章题数 = 章直挂题 + 全部小节题（单章不混挂，二者不同时有，防御性求和）
function chapterCount(ch: ListChapter): number {
  return ch.problems.length + ch.sections.reduce((n, s) => n + s.problems.length, 0)
}

async function onListChange() {
  chapters.value = []
  chapterNo.value = ''
  sectionNo.value = ''
  if (!listId.value) return
  detailLoading.value = true
  try {
    const d = await listApi.detail(listId.value)
    chapters.value = d.chapters ?? []
  } catch {
    chapters.value = []
  } finally {
    detailLoading.value = false
  }
}

async function confirm() {
  if (!listId.value) {
    ElMessage.warning('先选一个题单')
    return
  }
  loading.value = true
  try {
    const r = await listApi.flat(listId.value, chapterNo.value || undefined, sectionNo.value || undefined)
    if (!r.items.length) {
      ElMessage.warning('所选范围内没有可复制的题目')
      return
    }
    emit('merge', r.items, r.skipped_hidden)
    show.value = false
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <span class="copy-wrap">
    <el-button size="small" @click="show = true">从题单复制</el-button>
    <el-dialog v-model="show" title="从题单复制题目" width="520" append-to-body>
      <div class="cascade">
        <el-select v-model="listId" filterable placeholder="选择题单" style="width: 100%" @change="onListChange">
          <el-option v-for="l in lists" :key="l.id" :value="l.id" :label="`${l.title}（${l.problem_count} 题）`" />
        </el-select>
        <el-select
          v-if="chapters.length"
          v-model="chapterNo"
          :loading="detailLoading"
          placeholder="整个题单"
          style="width: 100%"
          @change="sectionNo = ''"
        >
          <el-option value="" label="整个题单（全部章节）" />
          <el-option v-for="ch in chapters" :key="ch.no" :value="ch.no" :label="`${ch.no}. ${ch.title}（${chapterCount(ch)} 题）`" />
        </el-select>
        <el-select v-if="curChapter && curChapter.sections.length" v-model="sectionNo" style="width: 100%">
          <el-option value="" label="整章（含全部小节）" />
          <el-option
            v-for="sec in curChapter.sections"
            :key="sec.no"
            :value="sec.no"
            :label="`${sec.no} ${sec.title}（${sec.problems.length} 题）`"
          />
        </el-select>
      </div>
      <p class="hint">
        按所选范围（整个题单 / 指定章节 / 指定小节）依题单顺序合并进「已选」；同一题只保留一次；隐藏题自动跳过并提示数量。
      </p>
      <template #footer>
        <el-button @click="show = false">取消</el-button>
        <el-button type="primary" :loading="loading" @click="confirm">复制</el-button>
      </template>
    </el-dialog>
  </span>
</template>

<style scoped>
.cascade {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.hint {
  margin: 10px 0 0;
  font-size: 12.5px;
  color: var(--soj-ink-3, #5b6b85);
}
</style>

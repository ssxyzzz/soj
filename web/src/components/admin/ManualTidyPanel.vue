<script setup lang="ts">
// 手动整理（v0.49，plan §2.49；v0.62 挂载冻结根治批改惰性单元格编辑）：分页题目表
//（每页 100 题）行内手改难度 / 知识点 / 来源标签——不经 AI、无建议环节，保存走
// /admin/tidy/manual（最终值整体替换 + 按字段对应 kind 打 tidiedAt 水位：保存过 =
// 已整理，下次 AI 整理默认跳过，redo 可重跑）。
// 草稿模型：行快照（服务器值）+ 编辑副本（drafts，按 pid 记，翻页/搜索不丢未保存修改），
// 脏 = 副本与快照逐字段比对；只提交变更字段，未触碰的未设难度（0）不会误发。
// v0.62：三列编辑改 LazySelectCell——默认文本态、点格才挂 el-select（同屏 ≤1 个），
// 替代 100 行 × 3 常驻选择器（实测挂载冻结 ~940ms 的根源；全树选项也只在编辑瞬间挂载）。
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import { adminApi } from '@/api'
import LazySelectCell from '@/components/LazySelectCell.vue'
import { useTagTreeStore } from '@/stores/tagtree'
import { useMetaTreeStore } from '@/stores/metaTree'
import type { TagNode, TidyApplyItem, TidyManualProblem } from '@/types'

const PAGE_SIZE = 100

const tagStore = useTagTreeStore()
const metaStore = useMetaTreeStore()

interface ManualDraft {
  difficulty: number
  tags: number[]
  meta_tags: number[]
}

const q = ref('')
const page = ref(1)
const total = ref(0)
const rows = ref<TidyManualProblem[]>([])
const drafts = ref(new Map<number, ManualDraft>())
const loading = ref(false)
const saving = ref(false)

// 选项 = 全树扁平化，label 带全路径（同名节点可分辨，搜「一级 / 名」直中）
interface TagOpt {
  id: number
  label: string
}

function flatten(nodes: TagNode[], path: string[] = []): TagOpt[] {
  const out: TagOpt[] = []
  for (const n of nodes) {
    const p = [...path, n.name]
    out.push({ id: n.id, label: p.join(' / ') })
    if (n.children?.length) out.push(...flatten(n.children, p))
  }
  return out
}

const tagOptions = computed<TagOpt[]>(() => flatten(tagStore.nodes))
const metaOptions = computed<TagOpt[]>(() => flatten(metaStore.nodes))
// 难度固定十档（LazySelectCell 单选展示 D1..D10，0 = 未设置）
const diffOptions = Array.from({ length: 10 }, (_, i) => ({ id: i + 1, label: `D${i + 1}` }))

function ensureDrafts(list: TidyManualProblem[]) {
  for (const row of list) {
    if (!drafts.value.has(row.id)) {
      drafts.value.set(row.id, {
        difficulty: row.difficulty,
        tags: [...row.tags],
        meta_tags: [...row.meta_tags],
      })
    }
  }
}

function draftOf(row: TidyManualProblem): ManualDraft {
  let d = drafts.value.get(row.id)
  if (!d) {
    // 正常路径 load() 已建好草稿，这里只兜渲染顺序的底
    d = { difficulty: row.difficulty, tags: [...row.tags], meta_tags: [...row.meta_tags] }
    drafts.value.set(row.id, d)
  }
  return d
}

function arrEq(a: number[], b: number[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

// 脏行集（逐字段比对快照）：只提交变更字段
const dirtyItems = computed<TidyApplyItem[]>(() => {
  const out: TidyApplyItem[] = []
  for (const r of rows.value) {
    const d = drafts.value.get(r.id)
    if (!d) continue
    const it: TidyApplyItem = { pid: r.id }
    if (d.difficulty !== r.difficulty) it.difficulty = d.difficulty
    if (!arrEq(d.tags, r.tags)) it.tags = [...d.tags]
    if (!arrEq(d.meta_tags, r.meta_tags)) it.meta_tags = [...d.meta_tags]
    if (it.difficulty !== undefined || it.tags || it.meta_tags) out.push(it)
  }
  return out
})
const dirtyPids = computed(() => new Set(dirtyItems.value.map((i) => i.pid)))
const dirtyCount = computed(() => dirtyItems.value.length)

async function load() {
  loading.value = true
  try {
    const r = await adminApi.tidyManualProblems(page.value, PAGE_SIZE, q.value.trim() || undefined)
    rows.value = r.list
    total.value = r.total
    ensureDrafts(r.list)
  } finally {
    loading.value = false
  }
}

// 搜索 400ms 防抖（ProblemPicker 同参）；翻页立即加载
let qTimer: ReturnType<typeof setTimeout> | undefined
watch(q, () => {
  if (qTimer) clearTimeout(qTimer)
  qTimer = setTimeout(() => {
    page.value = 1
    void load()
  }, 400)
})

function onPage(p: number) {
  page.value = p
  void load()
}

async function savePage() {
  const items = dirtyItems.value
  if (!items.length) return
  saving.value = true
  try {
    const r = await adminApi.tidyManualApply(items)
    if (r.failed > 0) {
      // 有失败：以服务器快照重建当前页（草稿保留——失败行改完可直接重试）
      ElMessage.warning(r.message || `已保存 ${r.applied} 题，失败 ${r.failed}`)
      await load()
    } else {
      for (const row of rows.value) {
        const d = drafts.value.get(row.id)
        if (!d) continue
        row.difficulty = d.difficulty
        row.tags = [...d.tags]
        row.meta_tags = [...d.meta_tags]
      }
      ElMessage.success(r.message || `已保存 ${r.applied} 题`)
    }
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void tagStore.ensure()
  void metaStore.ensure()
  void load()
})
</script>

<template>
  <div class="manual">
    <div class="toolbar">
      <TipLabel tip="保存计入「已整理」水位：保存过的题下次 AI 整理（本面板上方卡片）默认跳过，勾选「重新整理已整理过的题」可重跑；翻页 / 搜索不会丢失未保存的修改" />
      <el-input v-model="q" class="search" placeholder="搜索题号或标题" clearable />
      <span class="muted">共 {{ total }} 题 · 每页 {{ PAGE_SIZE }} 题</span>
      <span class="spacer" />
      <el-button type="primary" :loading="saving" :disabled="!dirtyCount" @click="savePage">
        保存本页修改（{{ dirtyCount }}）
      </el-button>
    </div>

    <el-table v-loading="loading" :data="rows" size="small">
      <el-table-column prop="id" label="题号" width="76" />
      <el-table-column label="标题" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">
          <router-link :to="`/problems/${row.id}`" target="_blank" class="tlink">{{ row.title }}</router-link>
        </template>
      </el-table-column>
      <!-- v0.62 惰性单元格编辑：默认文本态，点格才挂 el-select（同屏 ≤1 个，含全树
           465 项的选项列表也只在编辑瞬间挂载）——替代 100 行 × 3 常驻选择器的 ~940ms
           挂载冻结（persistent=false 只救了选项 DOM，选择器脚手架本身才是冻结大头） -->
      <el-table-column label="难度" width="108">
        <template #default="{ row }">
          <LazySelectCell
            :model-value="draftOf(row as TidyManualProblem).difficulty"
            :options="diffOptions"
            empty="未设置"
            @update:model-value="(v: number | number[]) => (draftOf(row as TidyManualProblem).difficulty = (v as number) ?? 0)"
          />
        </template>
      </el-table-column>
      <el-table-column label="知识点" min-width="200">
        <template #default="{ row }">
          <LazySelectCell
            :model-value="draftOf(row as TidyManualProblem).tags"
            :options="tagOptions"
            multiple
            @update:model-value="(v: number | number[]) => (draftOf(row as TidyManualProblem).tags = [...(v as number[])])"
          />
        </template>
      </el-table-column>
      <el-table-column label="来源标签" min-width="200">
        <template #default="{ row }">
          <LazySelectCell
            :model-value="draftOf(row as TidyManualProblem).meta_tags"
            :options="metaOptions"
            multiple
            @update:model-value="(v: number | number[]) => (draftOf(row as TidyManualProblem).meta_tags = [...(v as number[])])"
          />
        </template>
      </el-table-column>
      <el-table-column label="状态" width="80">
        <template #default="{ row }">
          <el-tag v-if="dirtyPids.has(row.id)" type="warning" size="small">待保存</el-tag>
        </template>
      </el-table-column>
    </el-table>

    <div class="pager-row">
      <el-pagination
        background
        layout="prev, pager, next, total"
        :total="total"
        :page-size="PAGE_SIZE"
        :current-page="page"
        @current-change="onPage"
      />
    </div>
  </div>
</template>

<style scoped>
.search {
  width: 240px;
}

.spacer {
  flex: 1;
}

.pager-row {
  display: flex;
  justify-content: center;
  margin-top: var(--soj-sp-12);
}

.muted {
  color: var(--el-text-color-secondary);
}
</style>

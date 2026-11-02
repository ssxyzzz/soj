<script setup lang="ts">
// 题单列表页（v0.18.3，plan §2.12）：全站自学导航入口——横条卡（主列通栏）=
// 左信息块（色块首字 + 名称 + N 题·M 章 + 简介摘要 + 我的进度）+ 右难度分布柱状图
//（只画有题的档，同源十档色轴）；副列按 web-design.md：题单管理卡（新建）+ 口径卡。
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import PageShell from '@/components/PageShell.vue'
import RailCard from '@/components/RailCard.vue'
import { listApi } from '@/api'
import { useLevelsStore } from '@/stores/levels'
import { useUserStore } from '@/stores/user'
import type { ListCard } from '@/types'
import { fmtDay } from '@/utils/format'

const router = useRouter()
const userStore = useUserStore()
const levels = useLevelsStore()

const items = ref<ListCard[]>([])
const canCreate = ref(false)
const loading = ref(true)

// 色块首字（八色轮换，拍板 6）：与排名页头像同族配色，按 id 取模稳定轮换
const avColors = ['#5a6b4f', '#8a5a33', '#3358b8', '#237e6b', '#7c3aed', '#b8483f', '#2f7d8a', '#7a5c2e']
function avColor(id: number): string {
  return avColors[Math.abs(id) % avColors.length]
}

// 柱状图同源十档色轴（main.css .diff-* 的 --dc 值）
const DIFF_COLORS = ['#b4793f', '#75868f', '#d99141', '#3358b8', '#237e6b', '#7c3aed', '#ee7e1f', '#e63946', '#d4af37', '#d4af37']

function diffName(d: number): string {
  return levels.cfg?.difficulty_names?.[d - 1] || `D${d}`
}

// 卡片柱状图数据：只保留有题的档（拍板：没有的等级不显示）
function chartBars(l: ListCard): { d: number; count: number; pct: number }[] {
  const max = Math.max(...l.difficulties, 0)
  const bars: { d: number; count: number; pct: number }[] = []
  for (let d = 1; d <= 10; d++) {
    const count = l.difficulties[d - 1] ?? 0
    if (count <= 0) continue
    bars.push({ d, count, pct: max ? Math.max(14, Math.round((count / max) * 100)) : 14 })
  }
  return bars
}

function introSummary(s: string): string {
  // 简介摘要：掐掉 Markdown 标记取纯文本前 60 字（空简介回占位句）
  const text = (s || '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/[#>*`~_\-|]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
  return text ? (text.length > 60 ? `${text.slice(0, 60)}…` : text) : '暂无简介'
}

async function load() {
  loading.value = true
  try {
    const r = await listApi.all()
    items.value = r.items
    canCreate.value = r.can_create
  } finally {
    loading.value = false
  }
}
onMounted(load)

// 排序 = 全局次序，仅 list.reorder（默认 root）可拖（拍板 2026-10-26）
const canReorder = computed(() => userStore.can('list.reorder'))

const dragId = ref<number | null>(null)
function onDragStart(e: DragEvent, id: number) {
  dragId.value = id
  e.dataTransfer?.setData('text/plain', String(id))
}
function onDrop(id: number) {
  if (dragId.value == null || dragId.value === id) return
  const from = items.value.findIndex((x) => x.id === dragId.value)
  const to = items.value.findIndex((x) => x.id === id)
  if (from < 0 || to < 0) return
  const next = [...items.value]
  const [moved] = next.splice(from, 1)
  next.splice(to, 0, moved)
  items.value = next
  dragId.value = null
  listApi.reorder(next.map((x) => x.id))
}

async function create() {
  try {
    const { value } = await ElMessageBox.prompt('给题单起个名字（创建后进编辑器加章节与题目）', '新建题单', {
      inputPattern: /\S+/,
      inputErrorMessage: '题单名不能为空',
      inputValidator: (v: string) => (v.trim().length > 64 ? '不超过 64 字' : true),
    })
    const r = await listApi.create(value.trim())
    ElMessage.success('已创建，来加章节吧')
    router.push(`/lists/${r.id}/edit`)
  } catch {
    /* 取消 */
  }
}


</script>

<template>
  <div v-loading="loading">
    <PageShell>
      <div class="page-head">
        <div class="kicker">PROBLEM LISTS</div>
        <h1 class="page-title">题单<small>全站自学导航 · 按章节组织</small></h1>
      </div>

      <el-empty v-if="!loading && !items.length" description="还没有题单" :image-size="80" />
      <div v-else class="cards">
        <div
          v-for="l in items"
          :key="l.id"
          class="card lcard"
          :draggable="canReorder"
          @dragstart="onDragStart($event, l.id)"
          @dragover.prevent
          @drop="onDrop(l.id)"
          @dragend="dragId = null"
          @click="router.push(`/lists/${l.id}`)"
        >
          <div class="lmain">
            <div class="lhead">
              <span v-if="canReorder" class="lhandle" title="拖动调整顺序">⠿</span>
              <span class="lav" :style="{ background: avColor(l.id) }">{{ (l.title || '?').slice(0, 1).toUpperCase() }}</span>
              <div class="grow">
                <div class="ltitle-row">
                  <h3><RouterLink :to="`/lists/${l.id}`" class="clink" @click.stop>{{ l.title }}</RouterLink></h3>
                  <el-tag v-if="l.hidden" size="small" type="danger" effect="plain">隐藏</el-tag>
                </div>
                <div class="lmeta">
                  {{ l.problem_count }} 题 · {{ l.chapter_count }} 章 · {{ l.participants }} 人参与 · {{ fmtDay(l.created_at) }}
                </div>
              </div>
            </div>
            <p class="lintro">{{ introSummary(l.intro) }}</p>
            <div class="lfoot" @click.stop>
              <div class="lpct">
                <div class="lpct-bar">
                  <div class="lpct-in" :style="{ width: `${l.problem_count ? Math.round((l.solved / l.problem_count) * 100) : 0}%` }" />
                </div>
                <span class="lpct-txt mono">{{ l.solved }}/{{ l.problem_count }}</span>
              </div>
              <!-- 编辑/删除入口在详情页/编辑器；行内排序走六点拖动（仅 list.reorder 可见） -->
            </div>
          </div>
          <!-- 右：难度分布柱状图（只画有题的档） -->
          <div v-if="chartBars(l).length" class="lchart" @click.stop>
            <div v-for="b in chartBars(l)" :key="b.d" class="lbar-col">
              <span class="lbar-count mono">{{ b.count }}</span>
              <div class="lbar-track">
                <div class="lbar" :style="{ height: `${b.pct}%`, background: DIFF_COLORS[b.d - 1] }" />
              </div>
              <span class="lbar-name">{{ diffName(b.d) }}</span>
            </div>
          </div>
        </div>
      </div>

    <template #aside>
      <!-- 管理卡（2026-10-26 用户拍板：主副列两列，新建题单挪右列） -->
      <RailCard ov="管理" title="题单管理" class="aside-card">
        <template v-if="canCreate">
          <el-button type="primary" style="width: 100%" @click="create">新建题单</el-button>
          <p class="hint">创建后进编辑器按章节组织题目；编辑与删除仅限创建者。</p>
        </template>
        <p v-else class="hint">题单由老师创建维护；有权限位的账号可在角色管理中开通「题单管理」。</p>
      </RailCard>
      <RailCard ov="说明" title="口径" class="aside-card">
        <p class="hint">「已完成」= 拿到满分（AC）；隐藏题不计入进度；同名次并列跳号。</p>
      </RailCard>
    </template>
  </PageShell>
  </div>
</template>

<style scoped>
/* 横条卡纵排（主列内通栏，2026-10-26 用户拍板：参考比赛页行卡，卡高不变） */
.cards {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-14);
}

.lhandle {
  flex: none;
  cursor: grab;
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-body);
  user-select: none;
  padding: 0 var(--soj-sp-2);
}

.lhandle:hover {
  color: var(--soj-blue);
}

.lcard {
  padding: var(--soj-sp-20) var(--soj-sp-24);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-24);
  transition: transform 0.15s, box-shadow 0.15s;
}

.lcard:hover {
  transform: translateY(-2px);
  box-shadow: var(--soj-shadow-card-hover);
}

/* 左信息块 */
.lmain {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-12);
}

/* 右难度分布柱状图：只画有题的档，底对齐 */
.lchart {
  flex: none;
  display: flex;
  align-items: flex-end;
  gap: var(--soj-sp-14);
  padding: 0 var(--soj-sp-6);
  align-self: stretch;
}

.lbar-col {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--soj-sp-4);
  min-width: 34px;
}

.lbar-count {
  font-size: var(--soj-fs-12);
  color: var(--soj-navy);
}

.lbar-track {
  height: 64px;
  width: 22px;
  display: flex;
  align-items: flex-end;
}

.lbar {
  width: 100%;
  border-radius: 5px 5px 2px 2px;
  opacity: 0.88;
}

.lbar-name {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  white-space: nowrap;
}

.lhead {
  display: flex;
  gap: var(--soj-sp-12);
  align-items: center;
}

.lav {
  flex: none;
  width: 44px;
  height: 44px;
  border-radius: 12px;
  display: grid;
  place-items: center;
  font-size: var(--soj-fs-20);
  font-weight: var(--soj-fw-heavy);
  color: var(--soj-white);
}

.grow {
  flex: 1 1 auto;
  min-width: 0;
}

.ltitle-row {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
}

.ltitle-row h3 {
  margin: 0;
  font-size: var(--soj-fs-16);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lmeta {
  margin-top: var(--soj-sp-2);
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.lintro {
  margin: 0;
  font-size: var(--soj-fs-13);
  color: var(--soj-ink-2);
  min-height: 36px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.lfoot {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-10);
}

.lpct {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  min-width: 0;
}

.lpct-bar {
  flex: 1 1 auto;
  height: 7px;
  border-radius: 999px;
  background: var(--soj-surface-2);
  overflow: hidden;
}

.lpct-in {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--soj-blue-hi), var(--soj-teal));
}

.lpct-txt {
  flex: none;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}

.lacts {
  flex: none;
  display: inline-flex;
  align-items: center;
}

.lacts .el-button + .el-button {
  margin-left: var(--soj-sp-4);
}

.aside-card {
  margin-bottom: var(--soj-sp-16);
}

.hint {
  margin: var(--soj-sp-10) 0 0;
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
}
</style>

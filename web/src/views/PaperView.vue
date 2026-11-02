<script setup lang="ts">
// 比赛/考试学生版打印视图（v0.10）：独立路由（MainLayout 之外），仅 organizer/root
// 能取到数据；按卷内顺序拼接题目，只保留题号/标题/题面/样例/客观题题干与选项/标签
// （v0.12 起带，2026-11-01「来源」更名「标签」），不带答案、解析、分值、知识点、统计、
// 时空限制。浏览器打印/另存 PDF。
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { contestApi, examApi, problemApi } from '@/api'
import MdView from '@/components/MdView.vue'
import { useMetaTreeStore } from '@/stores/metaTree'
import type { ObjectiveKind, PaperData } from '@/types'

type PaperKind = 'contest' | 'exam' | 'problem'

const props = defineProps<{ kind: PaperKind }>()

const route = useRoute()
const router = useRouter()
const paper = ref<PaperData | null>(null)
const loading = ref(true)
const error = ref('')

const kindLabel = computed(() => (props.kind === 'exam' ? '考试' : props.kind === 'problem' ? '题目' : '比赛'))

const kindMap: Record<ObjectiveKind, string> = {
  single: '单选题',
  multiple: '多选题',
  judge: '判断题',
  blank: '填空题',
}

function kindName(kind: ObjectiveKind): string {
  return kindMap[kind] ?? '客观题'
}

// 标签链（2026-10-25 双树）：打印取标签树节点名，全空返回空串
const metaStore = useMetaTreeStore()
metaStore.ensure()

function sourceOf(metaTags: number[] | undefined): string {
  return (metaTags ?? []).map((id) => metaStore.nameOf(id)).join(' ')
}

function letter(i: number): string {
  return String.fromCharCode(65 + i)
}

function back() {
  if (window.history.length > 1) router.back()
  else router.push('/')
}

onMounted(async () => {
  const id = Number(route.params.id)
  try {
    if (props.kind === 'problem') paper.value = await problemApi.paper(id)
    else paper.value = await (props.kind === 'exam' ? examApi : contestApi).paper(id)
  } catch {
    // 403/404 的提示由 axios 拦截器给出，这里保留可读的页面状态
    error.value = props.kind === 'problem'
      ? '无法读取试卷：仅题目编辑者可导出'
      : '无法读取试卷：仅比赛/考试创建者可打印'
  } finally {
    loading.value = false
  }
})

// 打印前等待 Vditor.preview 与图片完成，避免 Ctrl+P 时公式/图片缺失
async function waitForRender() {
  const deadline = Date.now() + 5000
  while (Date.now() < deadline) {
    const bodies = Array.from(document.querySelectorAll<HTMLElement>('.paper-sheet .md-view-body'))
    if (bodies.length === 0 || bodies.every((el) => el.innerHTML.trim() !== '')) break
    await new Promise((r) => setTimeout(r, 100))
  }
  await Promise.all(
    Array.from(document.querySelectorAll<HTMLImageElement>('.paper-sheet img')).map((img) =>
      img.complete
        ? Promise.resolve()
        : // v0.37 加超时：外域图挂起（连接不失败也不完成）时 onload/onerror 都不触发，
          // 打印按钮会永远没反应——单图最多等 8s 后放行（打印出裂图好过没反应）
          Promise.race([
            new Promise<void>((resolve) => {
              img.onload = img.onerror = () => resolve()
            }),
            new Promise<void>((resolve) => setTimeout(resolve, 8000)),
          ]),
    ),
  )
  // Vditor 的 KaTeX 渲染也是异步的：等 .katex 数量稳定，避免打印时公式仍是 TeX 源码
  let lastKatex = -1
  for (let i = 0; i < 20; i++) {
    const n = document.querySelectorAll('.paper-sheet .katex').length
    if (n === lastKatex && i >= 3) break
    lastKatex = n
    await new Promise((r) => setTimeout(r, 100))
  }
  await new Promise((r) => setTimeout(r, 200))
}

async function printPaper() {
  await nextTick()
  await waitForRender()
  window.print()
}
</script>

<template>
  <div class="paper-view">
    <div class="paper-toolbar no-print">
      <button type="button" class="paper-btn" @click="back">返回</button>
      <button type="button" class="paper-btn is-primary" @click="printPaper">打印 / 另存 PDF</button>
    </div>

    <div v-if="loading" class="paper-status">加载中…</div>
    <div v-else-if="error" class="paper-status is-error">{{ error }}</div>
    <div v-else-if="paper" class="paper-sheet">
      <header class="paper-head">
        <h1>{{ paper.title }}</h1>
        <div class="paper-sub">{{ kindLabel }} · 学生版（无答案）</div>
      </header>

      <section v-for="(p, i) in paper.problems" :key="p.id" class="paper-problem">
        <h2 v-if="!paper.single" class="paper-problem-title"><span class="paper-label">T{{ i + 1 }}</span>{{ p.title }}</h2>
        <div v-if="sourceOf(p.meta_tags)" class="paper-source">标签：{{ sourceOf(p.meta_tags) }}</div>
        <div v-if="p.file_io_input" class="paper-file-io">
          文件 IO：{{ p.file_io_input }} / {{ p.file_io_output }}
        </div>
        <MdView v-if="p.description" :html="p.description_html ?? ''" />

        <div v-if="p.type === 'objective' && p.questions?.length" class="paper-objective">
          <div v-for="(q, qi) in p.questions" :key="qi" class="paper-question">
            <div class="paper-q-head">
              <span>第 {{ qi + 1 }} 题</span>
              <span class="paper-q-kind">{{ kindName(q.kind) }}</span>
            </div>
            <MdView v-if="q.stem" :html="q.stem_html" />
            <div v-if="q.kind === 'blank'" class="paper-blanks">
              <div v-for="bi in q.blank_count" :key="bi" class="paper-blank">
                第 {{ bi }} 空：<span class="paper-blank-line"></span>
              </div>
            </div>
            <ol v-else class="paper-options">
              <li v-for="(opt, oi) in q.options" :key="oi">{{ letter(oi) }}. {{ opt }}</li>
            </ol>
          </div>
        </div>
      </section>

      <div v-if="!paper.problems.length" class="paper-status">本场暂无题目</div>
    </div>
  </div>
</template>

<style>
.paper-view {
  min-height: 100vh;
  padding: var(--soj-sp-24) var(--soj-sp-16) var(--soj-sp-32);
  background: var(--soj-surface-3);
  color: var(--soj-ink);
}

.paper-toolbar {
  display: flex;
  justify-content: space-between;
  gap: var(--soj-sp-8);
  max-width: 760px;
  margin: 0 auto var(--soj-sp-16);
}

.paper-btn {
  padding: var(--soj-sp-8) var(--soj-sp-14);
  border: 1px solid var(--soj-ink-3);
  border-radius: 6px;
  background: var(--soj-white);
  color: var(--soj-ink);
  font: inherit;
  cursor: pointer;
}

.paper-btn.is-primary {
  border-color: var(--soj-blue);
  background: var(--soj-blue);
  color: var(--soj-white);
}

.paper-sheet {
  box-sizing: border-box;
  max-width: 760px;
  margin: 0 auto;
  padding: var(--soj-sp-32);
  background: var(--soj-white);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
}

.paper-head {
  padding-bottom: var(--soj-sp-10);
  border-bottom: 2px solid var(--soj-ink);
  margin-bottom: var(--soj-sp-24);
}

.paper-head h1 {
  margin: 0 0 var(--soj-sp-6);
  font-size: var(--soj-fs-28);
  line-height: 1.35;
}

.paper-sub {
  color: var(--soj-ink);
  font-size: var(--soj-fs-14);
}

.paper-problem {
  margin: var(--soj-sp-24) 0;
  break-inside: avoid;
  page-break-inside: avoid;
}

.paper-problem-title {
  margin: 0 0 var(--soj-sp-12);
  font-size: var(--soj-fs-28);
  line-height: 1.4;
}

.paper-file-io {
  margin: -4px 0 var(--soj-sp-12);
  color: var(--soj-ink);
  font-size: var(--soj-fs-14);
}

/* 标签行（v0.12 打印带来源，2026-11-01 更名「标签」）：题号标题下、文件 IO 上，弱化灰 */
.paper-source {
  margin: -4px 0 var(--soj-sp-8);
  color: var(--soj-ink);
  font-size: var(--soj-fs-14);
}

.paper-label {
  margin-right: var(--soj-sp-8);
  color: var(--soj-ink);
}

.paper-question {
  margin: var(--soj-sp-16) 0;
  break-inside: avoid;
  page-break-inside: avoid;
}

.paper-q-head {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  margin-bottom: var(--soj-sp-6);
  font-weight: var(--soj-fw-emphasis);
}

.paper-q-kind {
  padding: 0 var(--soj-sp-6);
  border: 1px solid var(--soj-ink-3);
  border-radius: 4px;
  color: var(--soj-ink);
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-regular);
}

.paper-options {
  margin: var(--soj-sp-8) 0 0;
  padding-left: 0;
  list-style: none;
}

.paper-options li {
  margin: var(--soj-sp-4) 0;
}

.paper-blank {
  margin: var(--soj-sp-10) 0;
}

.paper-blank-line {
  display: inline-block;
  width: 180px;
  border-bottom: 1px solid var(--soj-ink);
  vertical-align: bottom;
}

.paper-status {
  max-width: 760px;
  margin: var(--soj-sp-24) auto;
  padding: var(--soj-sp-32);
  border-radius: 8px;
  background: var(--soj-white);
  color: var(--soj-ink);
  text-align: center;
}

.paper-status.is-error {
  color: var(--soj-danger-deep);
}

.paper-view .md-view {
  max-width: none !important;
}

/* MdView 复制反馈按钮（.md-copy-btn）不应出现在试卷上 */
.paper-view .md-copy-btn,
.paper-view .md-copy-text {
  display: none !important;
}

@page {
  size: A4;
  margin: 16mm 14mm;
}

@media print {
  .no-print {
    display: none !important;
  }

  .paper-view {
    padding: 0;
    background: var(--soj-white);
  }

  .paper-sheet {
    max-width: none;
    padding: 0;
    box-shadow: none;
  }

  .paper-problem {
    margin: 0 0 var(--soj-sp-16);
  }

  .paper-view pre,
  .paper-view blockquote {
    break-inside: avoid;
    page-break-inside: avoid;
  }
}
</style>

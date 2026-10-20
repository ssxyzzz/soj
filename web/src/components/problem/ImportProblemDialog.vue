<script setup lang="ts">
// 题目导入弹窗（v0.6.1，plan.md §2.3）：
//   URL 导入 —— 服务端抓取解析（SSRF 安全 fetcher + 各站适配器）+ 外链图转存 /uploads；
//   粘贴导入 —— 剪贴板 text/html 前端清洗（DOMPurify 白名单 + KaTeX annotation 还原
//   TeX 契约），登录墙内站点（洛谷等）的兜底路线。
// 确认后 emit('apply', payload) 由建题/编辑页回填表单；测试数据 zip 不在导入范围。
// 支持站点一期：Codeforces、Hydro 系自建实例；洛谷/LOJ 走粘贴路线（服务端抓不到）。
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { problemApi } from '@/api'
import MdView from '@/components/MdView.vue'
import { sanitizeRichHTML } from '@/utils/richtext'
import type { ImportPreviewResult } from '@/types'

export interface ImportedPayload {
  title: string
  html: string
  time_limit_ms?: number
  memory_limit_mb?: number
  source: string
}

const emit = defineEmits<{ apply: [p: ImportedPayload]; close: [] }>()

const tab = ref<'url' | 'paste'>('url')
const url = ref('')
const importing = ref(false)
const preview = ref<ImportPreviewResult | null>(null)
const pasteHTML = ref('')
const pasteTitle = ref('') // 粘贴页面的 <h1>（源站题名），作为标题建议

const previewTitle = computed(() => preview.value?.title ?? pasteTitle.value)
const previewHTML = computed(() => preview.value?.html ?? pasteHTML.value)
const canApply = computed(() => !!previewHTML.value.trim())

// ---- URL 导入 ----
async function importURL() {
  const u = url.value.trim()
  if (!u) {
    ElMessage.warning('请输入题目 URL')
    return
  }
  importing.value = true
  preview.value = null
  try {
    preview.value = await problemApi.importPreview(u)
  } catch {
    // 拦截器已 toast 错误信息
  } finally {
    importing.value = false
  }
}

// ---- 粘贴导入 ----
// paste 事件拿 text/html（富文本剪贴板），浏览器里已渲染过的公式经 KaTeX/MathJax
// 的 annotation 还原 TeX；纯文本退化走 statement 手工粘贴。
function onPaste(e: ClipboardEvent) {
  const html = e.clipboardData?.getData('text/html')
  if (!html) return // 纯文本粘贴留给默认行为
  e.preventDefault()
  const doc = new DOMParser().parseFromString(html, 'text/html')
  pasteTitle.value = (doc.querySelector('h1')?.textContent ?? '').trim().slice(0, 64)
  pasteHTML.value = cleanPastedHTML(html)
  ElMessage.success('已从剪贴板提取富文本，请在下方预览确认')
}

// 清洗粘贴 HTML：DOMPurify 白名单；KaTeX 的 annotation（application/x-tex）还原为
// data-latex 契约节点（编辑端可编辑、查看端 KaTeX 重渲染，往返无损）。
function cleanPastedHTML(raw: string): string {
  const doc = new DOMParser().parseFromString(raw, 'text/html')
  // KaTeX：.katex-mathml 里的 annotation 才是 TeX 源（.katex-html 是渲染镜像）
  doc.querySelectorAll('.katex').forEach((k) => {
    const tex = k.querySelector('annotation[encoding="application/x-tex"]')?.textContent ?? ''
    const display = k.closest('.katex-display') != null
    const span = doc.createElement(display ? 'div' : 'span')
    span.setAttribute('data-type', display ? 'block-math' : 'inline-math')
    span.setAttribute('data-latex', tex)
    const holder = display ? k.closest('.katex-display') ?? k : k
    holder.replaceWith(span)
  })
  // MathJax v2：Script 节点里是 TeX 源
  doc.querySelectorAll('script[type^="math/tex"]').forEach((s) => {
    const tex = s.textContent ?? ''
    const display = /mode=display/.test(s.getAttribute('type') ?? '')
    const el = doc.createElement(display ? 'div' : 'span')
    el.setAttribute('data-type', display ? 'block-math' : 'inline-math')
    el.setAttribute('data-latex', tex)
    s.replaceWith(el)
  })
  // MathJax v3： mjx-container 带 data-latex
  doc.querySelectorAll('mjx-container[data-latex]').forEach((m) => {
    const tex = m.getAttribute('data-latex') ?? ''
    const display = m.getAttribute('display') === 'block'
    const el = doc.createElement(display ? 'div' : 'span')
    el.setAttribute('data-type', display ? 'block-math' : 'inline-math')
    el.setAttribute('data-latex', tex)
    m.replaceWith(el)
  })
  return sanitizeRichHTML(doc.body.innerHTML)
}

function apply() {
  if (!canApply.value) {
    ElMessage.warning('没有可回填的内容（题面为空）')
    return
  }
  emit('apply', {
    title: previewTitle.value,
    html: previewHTML.value,
    time_limit_ms: preview.value?.time_limit_ms || undefined,
    memory_limit_mb: preview.value?.memory_limit_mb || undefined,
    source: preview.value?.source ?? '剪贴板导入',
  })
}

function close() {
  emit('close')
}
</script>

<template>
  <el-dialog :model-value="true" title="导入题目" width="860" top="4vh" @close="close">
    <el-tabs v-model="tab">
      <el-tab-pane label="URL 导入" name="url">
        <div class="url-row">
          <el-input
            v-model="url"
            placeholder="支持 Codeforces、Hydro 系自建实例；洛谷/LOJ 请用粘贴导入"
            clearable
            @keydown.enter="importURL"
          />
          <el-button type="primary" :loading="importing" @click="importURL">抓取解析</el-button>
        </div>
        <p class="dim tip">
          服务端抓取并解析标题 / 题面 / 样例 / 时空限制，外链图片自动转存本站；测试数据 zip 仍需自行上传。
        </p>
      </el-tab-pane>
      <el-tab-pane label="粘贴导入" name="paste">
        <div
          class="paste-zone"
          contenteditable="true"
          data-ph="到源站打开题目页面，全选（Ctrl+A）复制后，在此处粘贴（Ctrl+V）"
          @paste="onPaste"
        />
        <p class="dim tip">
          富文本粘贴自动清洗（公式经 KaTeX/MathJax 源码无损还原）；任何站点可用，含登录墙内页面。
        </p>
      </el-tab-pane>
    </el-tabs>

    <div v-if="canApply" class="preview">
      <div class="preview-head">
        <span class="preview-title">{{ previewTitle }}</span>
        <el-tag v-if="preview?.source" size="small" type="info">{{ preview.source }}</el-tag>
        <el-tag v-if="preview?.time_limit_ms" size="small">{{ preview.time_limit_ms }}ms</el-tag>
        <el-tag v-if="preview?.memory_limit_mb" size="small">{{ preview.memory_limit_mb }}MB</el-tag>
      </div>
      <el-alert
        v-if="preview?.transfer"
        :title="preview.transfer"
        type="success"
        :closable="false"
        class="transfer-tip"
      />
      <div class="preview-body">
        <MdView :content="previewHTML" />
      </div>
    </div>

    <template #footer>
      <el-button @click="close">取消</el-button>
      <el-button type="primary" :disabled="!canApply" @click="apply">回填到表单</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.url-row {
  display: flex;
  gap: 8px;
}

.tip {
  margin: 8px 0 0;
  font-size: 12px;
}

.paste-zone {
  min-height: 120px;
  padding: 12px;
  border: 1px dashed var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  outline: none;
  color: var(--el-text-color-placeholder);
}

.paste-zone:empty::before {
  content: attr(data-ph);
  color: var(--el-text-color-placeholder);
}

.preview {
  margin-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 12px;
}

.preview-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.preview-title {
  font-weight: 600;
}

.transfer-tip {
  margin-top: 8px;
}

.preview-body {
  max-height: 46vh;
  overflow-y: auto;
  margin-top: 8px;
  padding: 8px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--el-border-radius-base);
}
</style>

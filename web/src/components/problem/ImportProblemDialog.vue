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

const tab = ref<'url' | 'paste' | 'script'>('paste')
const url = ref('')
const importing = ref(false)
const preview = ref<ImportPreviewResult | null>(null)
const pasteHTML = ref('')
const pasteTitle = ref('') // 粘贴页面的 <h1>（源站题名），作为标题建议

const previewTitle = computed(() => preview.value?.title ?? pasteTitle.value)
const previewHTML = computed(() => preview.value?.html ?? pasteHTML.value)
const canApply = computed(() => !!previewHTML.value.trim())
// 脚本导入下载：相对路径自动跟随当前部署域名，服务端按请求 Host 烘焙进脚本
const scriptURL = '/api/import/userscript'

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
      <el-tab-pane label="脚本导入" name="script">
        <el-alert type="success" :closable="false" class="us-alert">
          <p>推荐给经常导入的老师：在自己浏览器里一键复制题面——带真实登录态与浏览器指纹，<b>洛谷等登录墙/反爬站点零封号风险</b>（这就是正常浏览行为，不经过本站服务器）。</p>
        </el-alert>
        <el-steps :active="4" align-center class="us-steps" finish-status="success">
          <el-step title="装扩展" description="浏览器安装篡改猴（Tampermonkey）扩展，Edge/Chrome 商店搜「篡改猴」" />
          <el-step title="装脚本" description="点下方按钮下载，篡改猴自动弹出安装页，点安装" />
          <el-step title="复制题面" description="打开源站题目页 → 点浏览器篡改猴图标 → 「复制题面到 SOJ 剪贴板」" />
          <el-step title="回站粘贴" description="回到本弹窗「粘贴导入」→ Ctrl+V → 预览回填" />
        </el-steps>
        <div class="us-actions">
          <a :href="scriptURL"><el-button type="primary">下载导入脚本（本站地址已内置）</el-button></a>
          <span class="dim">文件名 soj-import.user.js，篡改猴会自动识别并弹出安装页</span>
        </div>
        <el-collapse class="us-faq">
          <el-collapse-item title="脚本会抓取整页吗？会不会把导航栏也带进来？">
            脚本按各 OJ 的题面容器智能选取（Hydro 系 / Codeforces / 通用 markdown 容器），都未命中时回退整页——多带进来的导航杂项在「粘贴导入」清洗时会自动剥掉，预览确认后再回填。
          </el-collapse-item>
          <el-collapse-item title="图片和公式怎么办？">
            图片地址复制时自动转为绝对地址（外链保留，预览可见；如需入站可后续在编辑器里手动上传替换）。公式经 KaTeX/MathJax 源码无损还原，回填后仍是可编辑公式。
          </el-collapse-item>
          <el-collapse-item title="和 URL 导入有什么区别？">
            URL 导入由本站服务器抓取，快且自动转存图片，但受限于目标站的反爬与登录墙（洛谷、hydro.ac 不可用）；脚本导入在你自己的浏览器里工作，任何站点、任何登录态都可用，两者互补。
          </el-collapse-item>
          <el-collapse-item title="安全吗？">
            脚本只在本地做「选中题面 → 复制到剪贴板」一件事，不发起任何网络请求、不上传任何数据；下载后可用文本编辑器查看全部代码。本站地址已内置在脚本里，无需配置。
          </el-collapse-item>
        </el-collapse>
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

/* 脚本导入 tab */
.us-alert {
  margin-bottom: 12px;
}

.us-alert p {
  line-height: 1.8;
}

.us-steps {
  margin: 12px 0;
}

.us-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.us-faq {
  margin-top: 4px;
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

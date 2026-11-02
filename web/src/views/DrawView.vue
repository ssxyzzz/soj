<script setup lang="ts">
// 画板工具页（v0.56，页脚入口）：drawio embed 协议壳——画稿不出浏览器、后端零存储
// （用户拍板：全临时、讲完即删）。导出 PNG/SVG 纯浏览器本地下载；localStorage 自动
// 暂存/恢复/丢弃防课堂误关。宿主→编辑器动作必须 JSON 字符串化（对象形态编辑器不理
// 会，停在加载中——首验踩坑），双向均做解析容错 + origin 校验防注入。
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Download, Trash2 } from 'lucide-vue-next'

// drawio 服务地址（课堂网络拉不动官方 CDN 时改这一处即可切自托管）
const DRAW_BASE = 'https://embed.diagrams.net'

const frameEl = ref<HTMLIFrameElement>()
const frameKey = ref(0) // 丢弃后换 key 重挂 iframe 回白板态（比发空稿更确定，不赌协议行为）
const frameReady = ref(false) // init 握手完成后协议动作才可用
const draftAt = ref(0) // 最近自动暂存时间戳（0 = 本次会话尚无暂存）
const restored = ref(false) // 本次打开恢复了浏览器暂存的草稿
let pendingExport: '' | 'png' | 'svg' = ''

const DRAFT_KEY = 'soj.draw'

const frameSrc = DRAW_BASE + (DRAW_BASE.includes('?') ? '&' : '?') + 'embed=1&proto=json&ui=min&spin=1'

// 画板消息来源校验：只信 iframe 自身 origin（防其它窗口注入消息）
const frameOrigin = (() => {
  try {
    return new URL(frameSrc).origin
  } catch {
    return ''
  }
})()

function fmtTime(ts: number) {
  const d = new Date(ts)
  const p2 = (n: number) => String(n).padStart(2, '0')
  return `${p2(d.getHours())}:${p2(d.getMinutes())}`
}

// 导出文件名：画板-20261102-1530.png
function exportName(ext: string) {
  const d = new Date()
  const p2 = (n: number) => String(n).padStart(2, '0')
  return `\u753b\u677f-${d.getFullYear()}${p2(d.getMonth() + 1)}${p2(d.getDate())}-${p2(d.getHours())}${p2(d.getMinutes())}.${ext}`
}

// 宿主→编辑器动作统一走 JSON 字符串（drawio embed 协议要求）
function sendToFrame(payload: Record<string, unknown>) {
  frameEl.value?.contentWindow?.postMessage(JSON.stringify(payload), frameOrigin)
}

function onMessage(e: MessageEvent) {
  if (!frameOrigin || e.origin !== frameOrigin) return
  // 编辑器→宿主多为对象，兼容字符串形态（双向都做 JSON 解析容错）
  let msg = e.data
  if (typeof msg === 'string') {
    try {
      msg = JSON.parse(msg)
    } catch {
      return
    }
  }
  if (!msg || typeof msg !== 'object') return
  switch (msg.event) {
    case 'init': {
      // 握手完成：恢复浏览器暂存草稿或开新稿；autosave:1 让编辑器定期回传全量 XML
      frameReady.value = true
      const draft = localStorage.getItem(DRAFT_KEY)
      sendToFrame({ action: 'load', xml: draft || '', autosave: 1 })
      restored.value = !!draft
      break
    }
    case 'autosave':
    case 'save': {
      if (typeof msg.xml === 'string' && msg.xml) {
        localStorage.setItem(DRAFT_KEY, msg.xml)
        draftAt.value = Date.now()
      }
      break
    }
    case 'export': {
      pendingExport = ''
      const data = String(msg.data || '')
      if (!data) {
        ElMessage.warning('导出失败，请重试')
        break
      }
      const fmt = msg.format === 'svg' ? 'svg' : 'png'
      let href: string
      if (data.startsWith('data:')) {
        href = data
      } else if (fmt === 'png') {
        href = `data:image/png;base64,${data}`
      } else if (data.startsWith('<')) {
        href = URL.createObjectURL(new Blob([data], { type: 'image/svg+xml' }))
      } else {
        href = `data:image/svg+xml;base64,${data}`
      }
      const a = document.createElement('a')
      a.href = href
      a.download = exportName(fmt)
      a.click()
      if (href.startsWith('blob:')) {
        setTimeout(() => URL.revokeObjectURL(href), 5000)
      }
      break
    }
  }
}

// 导出：drawio export 动作纯客户端渲染，数据经 postMessage 回传后本地下载
function exportDiag(format: 'png' | 'svg') {
  if (!frameEl.value || !frameReady.value || pendingExport) return
  pendingExport = format
  sendToFrame({ action: 'export', format, transparent: false })
  // 编辑器侧失败不回事件时兜底解锁
  setTimeout(() => {
    pendingExport = ''
  }, 8000)
}

// 丢弃：确认后清浏览器暂存并重挂 iframe 回白板态
function discardDraft() {
  ElMessageBox.confirm('\u4e22\u5f03\u5f53\u524d\u753b\u7a3f\uff1f\u6d4f\u89c8\u5668\u6682\u5b58\u4f1a\u4e00\u5e76\u6e05\u9664\uff0c\u4e0d\u53ef\u6062\u590d\u3002', '\u4e22\u5f03\u753b\u7a3f', {
    type: 'warning',
    confirmButtonText: '\u4e22\u5f03',
    cancelButtonText: '\u53d6\u6d88',
  })
    .then(() => {
      localStorage.removeItem(DRAFT_KEY)
      restored.value = false
      draftAt.value = 0
      frameReady.value = false // 重挂后等新握手，协议动作先禁用
      frameKey.value++
    })
    .catch(() => {})
}

onMounted(() => {
  window.addEventListener('message', onMessage)
})
onBeforeUnmount(() => {
  window.removeEventListener('message', onMessage)
})
</script>

<template>
  <div class="draw-page">
    <div class="dp-head">
      <h2 class="dp-title">画板</h2>
      <div class="dp-actions">
        <span v-if="draftAt" class="dp-draft">已自动暂存 {{ fmtTime(draftAt) }}</span>
        <span v-else-if="restored" class="dp-draft">已恢复上次画稿</span>
        <el-button size="small" :disabled="!frameReady" :icon="Download" @click="exportDiag('png')">导出 PNG</el-button>
        <el-button size="small" :disabled="!frameReady" @click="exportDiag('svg')">导出 SVG</el-button>
        <el-button size="small" type="danger" plain :disabled="!frameReady" :icon="Trash2" @click="discardDraft">丢弃</el-button>
      </div>
    </div>
    <iframe :key="frameKey" ref="frameEl" :src="frameSrc" class="dp-frame" title="画板" allow="clipboard-write"></iframe>
  </div>
</template>

<style scoped>
.draw-page {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-8);
  height: calc(100vh - 160px);
}

.dp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--soj-sp-8);
  flex-wrap: wrap;
}

.dp-title {
  margin: 0;
  font-size: var(--soj-fs-body);
}

.dp-actions {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  flex-wrap: wrap;
}

.dp-draft {
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-12);
}

.dp-frame {
  flex: 1;
  min-height: 0;
  width: 100%;
  border: 1px solid var(--soj-line);
  border-radius: var(--soj-radius, 10px);
  background: var(--soj-white);
}
</style>

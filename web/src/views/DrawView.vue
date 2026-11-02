<script setup lang="ts">
// 画板工具页（v0.56，页脚入口）：drawio embed 协议壳——画稿不出浏览器、后端零存储
// （用户拍板：全临时、讲完即删）。导出 PNG/SVG 纯浏览器本地下载；localStorage 自动
// 暂存/恢复/丢弃防课堂误关。宿主→编辑器动作必须 JSON 字符串化（对象形态编辑器不理
// 会，停在加载中——首验踩坑），双向均做解析容错 + origin 校验防注入。
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Download, Trash2 } from 'lucide-vue-next'

// drawio 服务地址：自托管静态卷 /drawio（deploy/drawio + compose 挂载 + SOJ_DRAWIO_DIR），
// 同源后宿主可控制编辑器内部（面板开关/样式）；目录未部署时 /drawio 404，改回
// 'https://embed.diagrams.net' 一处即回官方托管（面板按钮功能随跨域失效）
const DRAW_BASE = '/drawio'

// ui=sketch：手绘风工具栏——左侧默认带自由绘图笔 / 形状库 / 表格等图标（用户拍板默认可见）
const DRAW_UI = 'sketch'

// 编辑器内定制 CSS（configure 动作注入，官方通道）：①主菜单卡紧贴左上角；②绘图工具
// 条横排、置于主菜单（含注入的形状/格式钮）之后的第二格（同一顶行，用户拍板）；③灰源
// 根治 = 只修 mxDisabled 半透明（opacity:1），原生图标色/悬停态原样保留——brightness
// 滤镜绷带已撤（悬停态被压成全黑，用户否决）。类名对 sketch UI 恒定。
const DRAW_CUSTOM_CSS = `
.geToolbarContainer.geSketchMainMenu { top: 0 !important; left: 0 !important; }
/* 尺寸/高度统一主菜单卡（44 高；快捷键角标 A/S/D 是撑高元凶，隐藏） */
.geToolbarContainer.geVerticalToolbar {
  flex-direction: row !important;
  top: 0 !important;
  bottom: auto !important;
  left: 260px !important;
  transform: none !important;
  width: auto !important;
  height: 44px !important;
  padding: 0 10px !important;
  align-items: center !important;
}
.geVerticalToolbar .geShortcutKey { display: none !important; }
/* 折叠钮挪最左 + «/» 双箭头（用户拍板图二风格）：展开显 « = 点击收起、折叠显 » = 点击
   展开；drawio 内联 background-image 需 !important 覆盖 */
.geVerticalToolbar > a[title*="折叠"] {
  order: -1;
  background: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24'%3E%3Cpath d='M11.5 7 6.5 12l5 5M16.5 7l-5 5 5 5' fill='none' stroke='%233f3f3f' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / 20px no-repeat !important;
}
.geCollapsedToolbar > a[title*="折叠"] {
  background: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24'%3E%3Cpath d='M12.5 7l5 5-5 5M7.5 7l5 5-5 5' fill='none' stroke='%233f3f3f' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / 20px no-repeat !important;
}
.geVerticalToolbar > a { opacity: 1 !important; }
/* 折叠态小卡钉回工具条起点（钮在最左 = 两态同位） */
.geToolbarContainer.geVerticalToolbar.geCollapsedToolbar { left: 262px !important; top: 0 !important; }
/* 保存/退出卡整体隐藏（v0.56 用户拍板：autosave + 宿主导出/丢弃已覆盖其职责） */
.geToolbarContainer:has(.geEmbedBtn) { display: none !important; }
/* 形状面板弹出位：贴左缘、主菜单下方留距（用户拍板） */
.mxWindow:has(.geSidebarContainer:not(.geFormatContainer)) { left: 0 !important; top: 64px !important; }
`

const frameEl = ref<HTMLIFrameElement>()
const frameKey = ref(0) // 丢弃后换 key 重挂 iframe 回白板态（比发空稿更确定，不赌协议行为）
const frameReady = ref(false) // init 握手完成后协议动作才可用
const draftAt = ref(0) // 最近自动暂存时间戳（0 = 本次会话尚无暂存）
const restored = ref(false) // 本次打开恢复了浏览器暂存的草稿
let pendingExport: '' | 'png' | 'svg' = ''

const DRAFT_KEY = 'soj.draw'

const frameSrc = DRAW_BASE + (DRAW_BASE.includes('?') ? '&' : '?') + `embed=1&proto=json&ui=${DRAW_UI}&lang=zh&configure=1&spin=1`

// 画板消息来源校验：只信 iframe 自身 origin（防其它窗口注入消息）；自托管为同源相对路径
const frameOrigin = (() => {
  try {
    return new URL(frameSrc, window.location.href).origin
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
    case 'configure': {
      // configure=1 时编辑器以此事件代替 init 先行启动（源码级确认）：回配置后编辑器
      // 才继续 doMain → 之后 init 照旧发。customCss 经官方通道注入编辑器内。
      sendToFrame({ action: 'configure', config: { customCss: DRAW_CUSTOM_CSS } })
      break
    }
    case 'init': {
      // 握手完成：恢复浏览器暂存草稿或开新稿；autosave:1 让编辑器定期回传全量 XML
      frameReady.value = true
      const draft = localStorage.getItem(DRAFT_KEY)
      sendToFrame({ action: 'load', xml: draft || '', autosave: 1 })
      restored.value = !!draft
      collapsePanelsOnLoad()
      injectHeaderButtons()
      watchFreehandWindow()
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

// ---- 同源面板控制（自托管专属；跨域时静默无效） ----

// 面板开关走 drawio 官方快捷键（菜单里明示）：Ctrl+Shift+K = Shapes、Ctrl+Shift+P =
// Format。同源后向编辑器 document 派发合成 keydown——KeyboardEvent 构造器不带
// keyCode/which，defineProperty 补上（drawio 快捷键处理读 evt.keyCode），实测接受。
// 菜单 DOM 驱动路线废弃记录：菜单项有三层嵌套 td（外层包装 td 点了无效）且弹出菜单
// 隐藏时常驻 DOM，事件模拟踩坑两轮。
function sendShortcut(code: number) {
  const win = frameEl.value?.contentWindow
  const doc = win?.document
  if (!win || !doc) return
  const ch = String.fromCharCode(code)
  for (const target of [doc, doc.body, win]) {
    try {
      const evt = new KeyboardEvent('keydown', {
        key: ch, code: `Key${ch}`, ctrlKey: true, shiftKey: true, bubbles: true, cancelable: true,
      })
      Object.defineProperty(evt, 'keyCode', { get: () => code })
      Object.defineProperty(evt, 'which', { get: () => code })
      target.dispatchEvent(evt)
    } catch {
      // 单目标失败不阻断其余目标
    }
  }
}

const toggleShapes = () => {
  if (frameReady.value) sendShortcut(75) // Ctrl+Shift+K
}
const toggleFormat = () => {
  if (frameReady.value) sendShortcut(80) // Ctrl+Shift+P
}

// 面板可见性：形状/格式面板均为编辑器内 mxWindow（标题随 lang=zh 汉化为 形状/格式，
// 中英双匹配），宽 0 = 收起
function panelVisible(kind: 'shapes' | 'format') {
  const doc = frameEl.value?.contentWindow?.document
  if (!doc) return false
  const pattern = kind === 'shapes' ? /^Shapes$|^形状$/ : /^Format$|^格式$/
  const ws = Array.from(doc.querySelectorAll('.mxWindow')).filter((w) =>
    pattern.test((w.querySelector('.mxWindowTitle')?.textContent || '').trim()),
  )
  return ws.length ? Math.max(...ws.map((w) => w.getBoundingClientRect().width)) > 5 : false
}

// 默认收起（用户拍板：干净画布，要用点顶栏「形状/格式」按钮）——init 后串行
function collapsePanelsOnLoad() {
  setTimeout(() => {
    if (frameReady.value && panelVisible('shapes')) toggleShapes()
    setTimeout(() => {
      if (frameReady.value && panelVisible('format')) toggleFormat()
    }, 600)
  }, 500)
}

// 自由绘图窗口初始定位（用户拍板蓝标位：格式面板左侧、顶行高度）——初始只定一次，
// 之后 drawio 原生拖拽自由调整。挂 MutationObserver 监听窗口出现（每次弹出重定位）；
// 格式面板开着时贴其左侧，未开时回退视口右侧。
let freehandObserver: MutationObserver | null = null

function watchFreehandWindow() {
  const win = frameEl.value?.contentWindow
  const doc = win?.document
  if (!win || !doc) return
  freehandObserver?.disconnect()
  freehandObserver = new MutationObserver(() => {
    const fhWin = Array.from(doc.querySelectorAll('.mxWindow')).find(
      (w) =>
        /^自由绘图$/.test((w.querySelector('.mxWindowTitle')?.textContent || '').trim()) &&
        w.getBoundingClientRect().width > 5 &&
        !(w as HTMLElement).dataset.sojPlaced,
    ) as HTMLElement | undefined
    if (!fhWin) return
    fhWin.dataset.sojPlaced = '1'
    const fmtWin = Array.from(doc.querySelectorAll('.mxWindow')).find(
      (w) => /^(Format|格式)$/.test((w.querySelector('.mxWindowTitle')?.textContent || '').trim()) && w.getBoundingClientRect().width > 5,
    )
    const fmtRect = fmtWin?.getBoundingClientRect()
    const vw = doc.documentElement.clientWidth
    const fhWidth = 182
    const left = fmtRect && fmtRect.width > 5 ? Math.max(0, Math.round(fmtRect.left) - fhWidth - 12) : Math.max(0, vw - fhWidth - 300)
    fhWin.style.left = `${left}px`
    fhWin.style.top = '84px'
  })
  freehandObserver.observe(doc.body, { childList: true, subtree: true })
}

// 「形状/格式」钮并入主菜单卡（用户拍板：☰ 之后、🗑 之前，同卡同款）——同源后往
// geSketchMainMenu 注入两个原生 geButton（类名即内置风格，悬停/内边距自动继承），
// 点击触发官方快捷键。重挂 iframe（丢弃）后随 init 重注入；跨域回退时 document 访问
// 抛 SecurityError 静默跳过（面板开关随跨域失效，见 DRAW_BASE 注释）。
function injectHeaderButtons(attempt = 0) {
  let doc: Document | undefined
  try {
    doc = frameEl.value?.contentWindow?.document
  } catch {
    return // 跨域回退：不注入
  }
  if (!doc || !doc.body) {
    if (attempt < 10) setTimeout(() => injectHeaderButtons(attempt + 1), 300)
    return
  }
  const menu = doc.querySelector('.geSketchMainMenu')
  if (!menu || doc.getElementById('soj-shapes-toggle')) return
  const mk = (id: string, label: string, code: number) => {
    const a = doc.createElement('a')
    a.id = id
    a.className = 'geButton'
    a.textContent = label
    a.addEventListener('click', () => sendShortcut(code))
    return a
  }
  const shapes = mk('soj-shapes-toggle', '形状', 75)
  const format = mk('soj-format-toggle', '格式', 80)
  const first = menu.children[0] // ☰ 主菜单钮
  if (!first) return
  menu.insertBefore(format, first.nextSibling) // 先插格式（落 ☰ 后）
  menu.insertBefore(shapes, format) // 再插形状（☰ 与格式之间）——终序：☰ 形状 格式 🗑 ↺ ↻
}

onMounted(() => {
  window.addEventListener('message', onMessage)
})
onBeforeUnmount(() => {
  window.removeEventListener('message', onMessage)
  freehandObserver?.disconnect()
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

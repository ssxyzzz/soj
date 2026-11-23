// SW 注册与换版静默交接（v0.46 预缓存批；v0.98.2 横幅确认制改静默交接）。
// 机制：新部署 → sw.js 字节变化 → 浏览器 update 检查装新 SW → 新 SW install 期后台
// 预热新版资产 → waiting。正在使用的会话（考试/写代码/比赛中）**永不**自动刷新——
// 编辑器代码无草稿持久化，会话内刷新丢代码不可接受——跑旧版不受打扰。
// 换版时刻 = **下次整页加载**：initSW 在 mount 前执行，此时全新加载无用户状态可丢，
// 发现 waiting 即 postMessage SKIP_WAITING → 新 SW activate 清旧缓存 →
// controllerchange 整页 reload，全部资产命中本地缓存（零网络冷载，用户无感）。
// 多标签页：任一标签整页交接后旧缓存被清，其余旧标签懒加载旧 chunk 404 →
// router.onError 整页导航自愈到新版（v0.98 之前的既有防线，零改动复用）。
// 探测两源喂 waiting（让下次交接零网络）：30 分钟定时、标签页回到前台。
// 降级：注册失败 = 现状网络直取（零损失）；?nosw 强制注销自愈路径（调试用）。

export function initSW() {
  if (!import.meta.env.PROD) return // dev 的 vite 服务器无产物语义，不注册
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return
  if (new URLSearchParams(location.search).has('nosw')) {
    void navigator.serviceWorker.getRegistrations().then((rs) => {
      void Promise.all(rs.map((r) => r.unregister()))
    })
    return
  }
  void navigator.serviceWorker
    .register('/sw.js')
    .then((reg) => {
      silentHandover(reg)
      setInterval(() => void reg.update().catch(() => {}), 30 * 60 * 1000)
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') void reg.update().catch(() => {})
      })
    })
    .catch(() => {
      /* 注册失败 = 现状网络直取 */
    })
}

// 静默交接（每次整页加载至多一次）：10s 窗防循环——正常交接 reload 后新 SW 已
// controller、waiting 消失自然不再触发；若切换失败 waiting 顽固存在，短窗内不重试
// （此时导航 network-first 拿到的已是新 index.html，等价降级为网络直取换版）。
const HANDOVER_AT = 'soj-sw-handover-at'
let reloading = false

function silentHandover(reg: ServiceWorkerRegistration) {
  // 无 controller = 首装（本来就是这个版本），不交接
  if (!reg.waiting || !navigator.serviceWorker.controller) return
  const last = Number(sessionStorage.getItem(HANDOVER_AT) || 0)
  if (Date.now() - last < 10_000) return
  sessionStorage.setItem(HANDOVER_AT, String(Date.now()))
  navigator.serviceWorker.addEventListener(
    'controllerchange',
    () => {
      if (reloading) return
      reloading = true
      location.reload()
    },
    { once: true },
  )
  reg.waiting.postMessage({ type: 'SKIP_WAITING' })
  // 兜底：极端环境 3s 内未收到 controllerchange（waiting 已死等），直接刷新拿新
  // index.html——网络多花一次但不会卡在旧版
  window.setTimeout(() => {
    if (!reloading) {
      reloading = true
      location.reload()
    }
  }, 3000)
}

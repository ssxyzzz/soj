// SW 注册与换版横幅状态（v0.46 前端预缓存批，plan §2.48）。
// 机制：新部署 → sw.js 字节变化 → 浏览器 update 检查装新 SW → 新 SW install 期后台
// 预热新版资产 → waiting → 本模块把 swUpdateReady 置真，MainLayout 顶部横幅「新版
// 本已就绪，点此刷新」→ 用户确认 → postMessage SKIP_WAITING → 新 SW activate 清旧
// 缓存 → controllerchange 后整页 reload，全部资产命中本地缓存（零网络冷载）。
// 换版必须用户确认（拍板）：新旧 chunk 混跑白屏风险在课堂上不可接受，宁可多一次点击。
// 探测三源：浏览器自身导航/update 检查、30 分钟定时、标签页回到前台。
// 降级：注册失败 = 现状网络直取（零损失）；?nosw 强制注销自愈路径（调试用）。
import { ref } from 'vue'

export const swUpdateReady = ref(false)

let refreshing = false

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
      // 打开页面时已有 waiting（上次横幅被忽略/浏览器重启恢复）→ 直接提示
      if (reg.waiting && navigator.serviceWorker.controller) swUpdateReady.value = true
      reg.addEventListener('updatefound', () => {
        const w = reg.installing
        if (!w) return
        w.addEventListener('statechange', () => {
          // installed 且已有 controller = 新 SW 预热完进入 waiting；首装（无
          // controller）不出横幅——本来就是这个版本
          if (w.state === 'installed' && navigator.serviceWorker.controller) swUpdateReady.value = true
        })
      })
      setInterval(() => void reg.update().catch(() => {}), 30 * 60 * 1000)
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') void reg.update().catch(() => {})
      })
    })
    .catch(() => {
      /* 注册失败 = 现状网络直取 */
    })
}

// 用户点击「点此刷新」：通知 waiting SW 跳过等待；控制器切换后整页刷新一次完成换版。
export function applySWUpdate() {
  void navigator.serviceWorker.getRegistration().then((reg) => {
    if (!reg?.waiting) {
      swUpdateReady.value = false
      return
    }
    navigator.serviceWorker.addEventListener(
      'controllerchange',
      () => {
        if (refreshing) return
        refreshing = true
        location.reload()
      },
      { once: true },
    )
    reg.waiting.postMessage({ type: 'SKIP_WAITING' })
    // 兜底：极端环境 3s 内未收到 controllerchange（waiting 已死等），直接刷新拿新
    // index.html——网络多花一次但不会卡在旧版
    window.setTimeout(() => {
      if (!refreshing) {
        refreshing = true
        location.reload()
      }
    }, 3000)
  })
}

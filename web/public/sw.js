/*
 * SOJ Service Worker（v0.46 前端预缓存批，plan §2.48）——部署冷载根治。
 * 链路背景：学生→VPS Caddy→frp→家宽，资产单请求 ~100ms 双跳、总管道 ~1.8MB/s，
 * 部署时刻全员同步重下是结构性拥塞；本文件把冷载从「部署时刻」搬到「各用户后台
 * 预热时刻」。本文件经 public/ 直拷进 dist，构建尾由 vite 插件（sojPrecache）把
 * 下方 BUILD 常量的占位符盖成「版本 + 时间戳」——每次构建字节必变，这是浏览器对
 * SW 做 update 检查的唯一触发信号；清单独立成 /asset-manifest.json 运行时拉取，
 * 避免 SW 本体重哈希循环。
 *
 * 行为三则（拍板口径）：
 *   1) /assets/ 哈希资产 cache-first——同名必同内容，命中即零流量零延迟；
 *   2) 导航请求 network-first 回缓存兜底——在线拿最新 HTML 决定版本，断网给旧版；
 *   3) /api/** 与一切带 Authorization 的请求绝不经过 SW（判题/提交/玉实时性红线）。
 *
 * 管辖面刻意收窄：只接管导航、/assets/ 哈希件与 favicon.svg；/uploads/（用户图
 * 片，量不可控）与一切其他路径原样放行走网络——SW 坏也坏不到主流程。
 *
 * 换版流（skipWaiting 用户确认制）：新 SW install 时按清单后台预热（并发 2 错峰）
 * → waiting → 页面横幅「新版本已就绪」→ 用户点击 postMessage SKIP_WAITING →
 * activate 清旧缓存 → 页面 reload 后全部命中本地缓存，零网络冷载。
 */
const BUILD = '__SOJ_SW_BUILD__'
const CACHE = 'soj-v-' + BUILD
const MANIFEST_URL = '/asset-manifest.json'

// 与服务端 static.go 的 hashedAsset 同口径：assets/index-BwZDfCAb.js（8 位构建哈希）
const HASHED = /-[0-9A-Za-z_$]{8}\.[0-9A-Za-z]+$/

self.addEventListener('install', (event) => {
  // 后台预热新版资产。首装时页面刚经网络加载过全部资源，immutable 哈希件直接命中
  // HTTP 缓存、几乎零网络；换版时新哈希名才是真网络（这正是要错峰的流量）。单件
  // 失败/清单拉不到都不阻断 install——绝不让 SW 安装卡死。
  event.waitUntil(
    (async () => {
      try {
        const res = await fetch(MANIFEST_URL, { cache: 'no-store' })
        if (!res.ok) return
        const m = await res.json()
        const files = [...(m.assets || []), ...(m.entries || [])]
        const cache = await caches.open(CACHE)
        for (let i = 0; i < files.length; i += 2) {
          await Promise.all(
            files.slice(i, i + 2).map(async (f) => {
              try {
                if (await cache.match(f)) return
                const resp = await fetch(f)
                if (resp && resp.ok) await cache.put(f, resp)
              } catch (_) {
                /* 单件失败不阻断，activate 后运行时还有 cache-first 的网络回源 */
              }
            }),
          )
        }
      } catch (_) {
        /* 清单失败照常 install，页面继续网络直取（与无 SW 现状等价） */
      }
    })(),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      // 清掉所有旧版本缓存（换版/回滚同一路径：旧缓存与新版本互不共存）
      const keys = await caches.keys()
      await Promise.all(
        keys.filter((k) => k !== CACHE && k.startsWith('soj-v-')).map((k) => caches.delete(k)),
      )
      // 首装（页面尚无 controller）时立即接管注册页；换版路径页面会整页 reload，
      // claim 只是让接管不依赖那次 reload 的时序
      await self.clients.claim()
    })(),
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  // 只接管同源 GET；非 GET、/api/、带 Authorization、清单与 SW 自身一律放行网络。
  // 判定全部同步完成后再 respondWith（异步判定会错过时机）。
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api/') || req.headers.has('authorization')) return
  if (url.pathname === MANIFEST_URL || url.pathname === '/sw.js') return

  if (req.mode === 'navigate') {
    event.respondWith(networkFirstNav(req))
    return
  }
  const name = url.pathname.slice(url.pathname.lastIndexOf('/') + 1)
  if (url.pathname.startsWith('/assets/') && HASHED.test(name)) {
    event.respondWith(cacheFirst(req, url.pathname))
    return
  }
  if (url.pathname === '/favicon.svg') {
    // 稳定名、内容可能随版本换——network-first 回缓存，不能 cache-first
    event.respondWith(networkFirst(req, url.pathname))
  }
  // 其余（/uploads/ 等）原样走网络
})

// 导航：network-first。在线时与无 SW 现状完全一致（index.html 本就 no-cache 每次回源），
// 成功响应顺手更新离线兜底；断网时给缓存的 index.html（SPA 离线可用）。
async function networkFirstNav(req) {
  try {
    const res = await fetch(req)
    if (res && res.ok) {
      const cache = await caches.open(CACHE)
      await cache.put('/index.html', res.clone())
    }
    return res
  } catch (_) {
    // 跨缓存找 index.html：正常只有当前版本缓存，回滚瞬间可能新旧并存
    const cached = await caches.match('/index.html')
    return cached || Response.error()
  }
}

// 哈希资产：cache-first。命中零流量零延迟；未命中（预热漏件/新懒 chunk）走网络并补进缓存。
async function cacheFirst(req, pathname) {
  const cache = await caches.open(CACHE)
  const hit = await cache.match(pathname)
  if (hit) return hit
  try {
    const res = await fetch(req)
    if (res && res.ok) await cache.put(pathname, res.clone())
    return res
  } catch (_) {
    return Response.error()
  }
}

async function networkFirst(req, pathname) {
  try {
    const res = await fetch(req)
    if (res && res.ok) {
      const cache = await caches.open(CACHE)
      await cache.put(pathname, res.clone())
    }
    return res
  } catch (_) {
    const hit = await caches.match(pathname)
    return hit || Response.error()
  }
}

self.addEventListener('message', (event) => {
  const data = event.data || {}
  if (data.type === 'SKIP_WAITING') self.skipWaiting() // 仅用户点击横幅确认后由页面发来
  else if (data.type === 'PING') event.source && event.source.postMessage({ type: 'PONG', build: BUILD })
})

// 降级兜底：SW 自身脚本异常时自我注销回到网络直取，绝不把站点挂死
self.addEventListener('error', () => void self.registration.unregister())
self.addEventListener('unhandledrejection', () => void self.registration.unregister())

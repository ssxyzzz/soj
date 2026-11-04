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
 * 缓存封顶（v0.60 补丁）：install 预热完即回收历史预热缓存——旧逻辑清理只挂
 * activate，未确认的更新被更新的等待者顶掉时 activate 永远不跑，缓存按版本无上限
 * 堆叠（生产实测单 origin 数百 MB）；现 install 预热完保留「当前 + 激活中（IDB 记名）」
 * 至多两份，其余即删，全链 fetch 均有网络回源故删除无损在线行为。
 */
const BUILD = '__SOJ_SW_BUILD__'
const CACHE = 'soj-v-' + BUILD
const MANIFEST_URL = '/asset-manifest.json'

// 与服务端 static.go 的 hashedAsset 同口径：assets/index-BwZDfCAb.js（8 位构建哈希，
// base64url 字母表含 - 与 _——漏 - 约一成文件不吃 cache-first，冷载通道重新打开）
const HASHED = /-[0-9A-Za-z_$-]{8}\.[0-9A-Za-z]+$/

// —— 旧版本预热缓存回收（v0.60 补丁）——清理原本只挂在 activate：用户确认制换版下，
// 未确认的更新各自预热一份 ~4MB 缓存后无人清理（等待中的 SW 被更新的等待者顶掉时
// activate 永远不会跑），发版频繁时按版本无上限堆叠（生产实测单 origin 数百 MB）。
// install 预热完即回收：保留当前 warming 缓存与激活中 SW 的缓存（activate 用 IDB
// 记名；历史版本无记录按「无激活」处理，其缓存回收后各 fetch 路径有网络回源兜底），
// 把每 origin 的 SW 缓存封顶在 2 份（~8MB）
const IDB_NAME = 'soj-sw-meta'
function idbGet(key) {
  return new Promise((resolve) => {
    const open = indexedDB.open(IDB_NAME, 1)
    open.onupgradeneeded = () => open.result.createObjectStore('kv')
    open.onsuccess = () => {
      const db = open.result
      const req = db.transaction('kv').objectStore('kv').get(key)
      req.onsuccess = () => {
        resolve(req.result)
        db.close()
      }
      req.onerror = () => {
        resolve(undefined)
        db.close()
      }
    }
    open.onerror = () => resolve(undefined)
  })
}
function idbSet(key, value) {
  return new Promise((resolve) => {
    const open = indexedDB.open(IDB_NAME, 1)
    open.onupgradeneeded = () => open.result.createObjectStore('kv')
    open.onsuccess = () => {
      const db = open.result
      const tx = db.transaction('kv', 'readwrite')
      tx.objectStore('kv').put(value, key)
      tx.oncomplete = () => {
        resolve()
        db.close()
      }
      tx.onerror = () => resolve()
    }
    open.onerror = () => resolve()
  })
}

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
        // v0.65 预热收敛：剔除 admin 专属懒 chunk（仅管理端可达，普通用户永不请求）
        // 与 KaTeX 字体（题面出公式时浏览器按字形按需取，预热 1.2MB 全族纯属挤占
        // 发版窗口的共享管道）。两类件运行时仍走正常 cache-first，需要时自会入缓存。
        const files = [...(m.assets || []), ...(m.entries || [])].filter(
          (f) => !/\/assets\/Admin[\w.-]*\.(js|css)$/.test(f) && !/KaTeX_.*\.(woff2?|ttf)$/.test(f),
        )
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
      // 预热完即回收历史预热缓存（见上方说明），不等 activate
      try {
        const active = (await idbGet('activeCache')) || ''
        const keys = await caches.keys()
        await Promise.all(
          keys
            .filter((k) => k !== CACHE && k !== active && k.startsWith('soj-v-'))
            .map((k) => caches.delete(k)),
        )
      } catch (_) {
        /* 回收失败不阻断 install，activate 还有同口径清理 */
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
      // 记名供下一次 install 的回收逻辑识别「激活中缓存」并保留（见文件头回收说明）
      await idbSet('activeCache', CACHE)
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

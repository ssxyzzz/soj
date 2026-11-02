import { existsSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig, type Plugin } from 'vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

// v0.46 前端预缓存批：构建尾给 dist/sw.js 盖构建戳并产出 /asset-manifest.json。
// 构建戳 = VERSION 环境变量（Makefile 注入 git describe，与二进制 /api/health 同源）
// + 时间戳——同一提交连发两次也保证 sw.js 字节变化，这是浏览器对 SW 做 update 检查
// 的唯一触发信号；清单独立成 /asset-manifest.json 由 SW 运行时拉取，避免 SW 体内
// 联清单导致本体重哈希循环。清单含全部 assets/ 哈希件（预热+cache-first 的全集）
// 与稳定名入口件（index.html/favicon.svg，离线兜底）；sw.js 与清单本体不入清单。
function sojPrecache(): Plugin {
  return {
    name: 'soj-precache',
    apply: 'build',
    closeBundle() {
      const dist = fileURLToPath(new URL('./dist', import.meta.url))
      const build = `${process.env.VERSION || 'dev'}.${Date.now().toString(36)}`
      // public/sw.js 原样拷进 dist 后盖戳。断言占位符恰好一次：曾因散文注释里也
      // 出现同名字符串、replace 只换首个命中而盖到注释上——BUILD 常量恒为占位符、
      // 缓存名永不变、换版永不触发的静默杀手，故此处宁可构建失败也不带病出货
      const swPath = join(dist, 'sw.js')
      const sw = readFileSync(swPath, 'utf8')
      const hits = sw.split('__SOJ_SW_BUILD__').length - 1
      if (hits !== 1) {
        this.error(`sw.js 占位符 __SOJ_SW_BUILD__ 应恰好出现 1 次，实际 ${hits} 次`)
      }
      writeFileSync(swPath, sw.replace('__SOJ_SW_BUILD__', build))
      // 与服务端 static.go hashedAsset 同口径的 8 位哈希判定（base64url 字母表，含 - 与 _：
      // 漏 - 曾让约 12% 的 chunk 不进预缓存清单——哈希带连字符的路由 JS 冷载通道重新打开）
      const hashed = /-[0-9A-Za-z_$-]{8}\.[0-9A-Za-z]+$/
      const assets = readdirSync(join(dist, 'assets'))
        .filter((f) => hashed.test(f))
        .map((f) => 'assets/' + f)
      const entries = ['index.html', 'favicon.svg'].filter((f) => existsSync(join(dist, f)))
      writeFileSync(
        join(dist, 'asset-manifest.json'),
        JSON.stringify({ version: build, assets, entries }),
      )
    },
  }
}

export default defineConfig({
  plugins: [
    vue(),
    // Element Plus 按需引入（v0.28 打包批）：模板组件 + 指令（v-loading）自动按需带
    // 样式，全量注册与全量 CSS 退役；程序化调用（ElMessage/ElMessageBox）的样式在
    // main.ts 手动补（直接 import JS 不会带样式）
    Components({ resolvers: [ElementPlusResolver()], dts: 'src/components.d.ts' }),
    sojPrecache(),
  ],
  resolve: {
    alias: [
      { find: '@', replacement: fileURLToPath(new URL('./src', import.meta.url)) },
      // 裸导入 'element-plus'（ElMessage/ElMessageBox 程序化调用）重定向到子路径垫片，
      // 防止根入口 re-export 链让全量组件进包（v0.28，见 src/element-plus.ts 注释）；
      // 正则锚定整串，不影响 element-plus/es/**、dist/** 与 @element-plus/icons-vue
      { find: /^element-plus$/, replacement: fileURLToPath(new URL('./src/element-plus.ts', import.meta.url)) },
    ],
  },
  server: {
    host: true,
    port: 5173,
    proxy: {
      // 本地开发时把 API 请求代理到本机运行的 Go 服务。默认 8080（compose 场景的
      // 宿主侧端口是 80）：并行开发/验证时用环境变量覆盖，避免来回改这个文件——
      //   SOJ_API_TARGET=http://127.0.0.1:80 npm run dev
      '/api': {
        target: process.env.SOJ_API_TARGET ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        // vendor 分层（v0.36；2026-10-31 复盘修订注释）：高延迟链路（学生→VPS→隧道
        // →家，~100ms/请求）下 vendor 与业务同 chunk 导致每次部署全员重下。manualChunks
        // 分组纪律（v0.28 实测 + v0.36 复测 924KB 实锤）：手工指派会把按需引入挡在
        // 图外的组件整族拖回来（carousel/tour/watermark 全进包），**element-plus
        // 一律留给自动分包**。窄组只收 vue 全家 / pinia / axios。
        // 【稳定性天花板（实测两次破防后定论）】本层内容是摇树后的库代码——依赖没动，
        // 新批次「新用了一处 vue/pinia 导出」或「新增图标导入」都会改写摇树面 → 整层
        // 换哈希（v0.36 建 DhLJTouY → v0.39+ 批 BI3CZRwj，lockfile 零变化即此机制）。
        // 所以本层只承诺「不动库 API 用法与图标集的迭代」不换哈希；彻底消灭部署冷载
        // 需 service-worker 预缓存策略（候选项）。icons 不入层：单图标 ~1KB 随业务
        // chunk 走，别让「加图标」这种高频动作破防 223KB 大层。
        // vditor/CM6 维持组件内动态 import 的既有异步链路；
        // experimentalMinChunkSize 继续兜碎片（题面页闭包曾 40+ 个 2-7KB 小 chunk）。
        experimentalMinChunkSize: 15000,
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (
            id.includes('/vue/') ||
            id.includes('@vue/') ||
            id.includes('vue-router') ||
            id.includes('/pinia/') ||
            id.includes('pinia/') ||
            id.includes('/axios/')
          ) {
            return 'vendor-core'
          }
        },
      },
    },
  },
})

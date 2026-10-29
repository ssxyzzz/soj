import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

export default defineConfig({
  plugins: [
    vue(),
    // Element Plus 按需引入（v0.28 打包批）：模板组件 + 指令（v-loading）自动按需带
    // 样式，全量注册与全量 CSS 退役；程序化调用（ElMessage/ElMessageBox）的样式在
    // main.ts 手动补（直接 import JS 不会带样式）
    Components({ resolvers: [ElementPlusResolver()], dts: 'src/components.d.ts' }),
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
        // vendor 分包（v0.28）：注意不能用 manualChunks 硬分组 element-plus——
        // 被手工指派 chunk 的模块不会被摇树（rollup 已知行为，实测 943KB 全量进包），
        // 改由 rollup 自动分包；vditor/CM6 维持组件内动态 import 的既有异步链路。
        // 碎片收敛（加载优化批）：题面页闭包曾 40+ 个 2-7KB 小 chunk，15KB 以下
        // 尝试合并——h2 多路复用下省往返与解压开销；构建后核对首屏闭包无回退
        experimentalMinChunkSize: 15000,
      },
    },
  },
})

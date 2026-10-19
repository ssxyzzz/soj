import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: true,
    port: 5173,
    proxy: {
      // 本地开发时把 API 请求代理到本机运行的 Go 服务。默认 8080（compose 场景的
      // 宿主侧端口是 80）：并行开发/验证时用环境变量覆盖，避免来回改这个文件——
      // SOJ_API_TARGET=http://127.0.0.1:80 npm run dev
      '/api': {
        target: process.env.SOJ_API_TARGET ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})

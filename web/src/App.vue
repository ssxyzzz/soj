<script setup lang="ts">
// 编辑器资源空闲预取（v0.26.2 起；v0.52 编辑器内芯换血改预取 CM6 内芯 chunk）：
// 首屏渲染完后空闲期把编辑器懒 chunk（MdEditorInner + CodeMirror，~79KB gzip）预热
// 进 HTTP 缓存（immutable，命中即零流量）。沿用加载优化批降级口径：仅持建题/编辑
// 权限者预取（游客与纯做题学生不白付编辑器体积）。查看端 md 模式（首页公告/讨论区
// 等无 html 缓存的 Vditor.preview 直渲场景）不预取，需要时按需加载。
// requestIdleCallback 不可用时 3s 兜底。
import { onMounted } from 'vue'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'

import { useUserStore } from '@/stores/user'

function prefetchEditor() {
  const u = useUserStore()
  const editor = u.can('problem.create') || u.can('problem.edit.own') || u.can('problem.edit.all')
  if (!editor) return
  void import('@/components/MdEditorInner.vue') // 编辑器懒 chunk（CM6 内芯 + 预览组件）
}

// 高频下一跳路由预取（v0.36「卡」根治批）：公网链路（学生→VPS→隧道→家）单请求
// 固定 ~100ms 双跳往返，冷跳转的 chunk 拉取串在关键路径上——空闲期把做题主线
// （题目详情）与登录者的提交记录页预热进缓存，点过去即零等待。游客只预热详情。
function prefetchRoutes() {
  const u = useUserStore()
  void import('@/views/ProblemDetailView.vue')
  if (u.token) void import('@/views/SubmissionListView.vue')
}

onMounted(() => {
  const w = window as Window & { requestIdleCallback?: (fn: () => void, opts?: { timeout: number }) => number }
  const idle = (fn: () => void) => (w.requestIdleCallback ? w.requestIdleCallback(fn, { timeout: 5000 }) : window.setTimeout(fn, 3000))
  idle(prefetchEditor)
  idle(prefetchRoutes)
})
</script>

<template>
  <el-config-provider :locale="zhCn">
    <router-view />
  </el-config-provider>
</template>

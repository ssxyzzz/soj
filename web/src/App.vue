<script setup lang="ts">
// Vditor 空闲预取（v0.26.2；加载优化批降级口径）：首屏渲染完后空闲期把 vditor
// chunk + lute 引擎预热进 HTTP 缓存（版本化路径 + immutable，命中即零流量）。
// v0.30 写入时渲染后查看端走服务端 html 直插，lute 只剩编辑器与「html 缺失回退」
// 两个低频消费者——预取降级为仅持建题/编辑权限者（游客与纯做题学生不再白付
// ~673KB gzip）；回退路径需要时 vditor 自行按需加载，慢一次但不缺功能。
// requestIdleCallback 不可用时 3s 兜底。
import { onMounted } from 'vue'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'

import { useUserStore } from '@/stores/user'
import { VDITOR_LUTE_URL } from '@/utils/vditorCdn'

function prefetchVditor() {
  const u = useUserStore()
  const editor = u.can('problem.create') || u.can('problem.edit.own') || u.can('problem.edit.all')
  if (!editor) return
  void import('vditor') // 编辑器懒 chunk（assets，本身 immutable）
  void fetch(VDITOR_LUTE_URL, { cache: 'force-cache' }) // lute 引擎（版本路径，命中即零流量）
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
  idle(prefetchVditor)
  idle(prefetchRoutes)
})
</script>

<template>
  <el-config-provider :locale="zhCn">
    <router-view />
  </el-config-provider>
</template>

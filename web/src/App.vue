<script setup lang="ts">
// Vditor 空闲预取（v0.26.2）：题面/讨论查看端与编辑器都依赖 vditor chunk + lute 引擎
//（3.6MB，最大子资源）。首屏渲染完后空闲期预热进 HTTP 缓存（版本化路径 + immutable，
// 二次访问磁盘直读近零成本）——把「首次打开编辑器/题面卡 1 秒」的成本挪到用户无感期。
// 游客也预取：查看端（MdView）同样要 lute。requestIdleCallback 不可用时 3s 兜底。
import { onMounted } from 'vue'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'

import { VDITOR_LUTE_URL } from '@/utils/vditorCdn'

function prefetchVditor() {
  void import('vditor') // 编辑器懒 chunk（assets，本身 immutable）
  void fetch(VDITOR_LUTE_URL, { cache: 'force-cache' }) // lute 引擎（版本路径，命中即零流量）
}

onMounted(() => {
  const w = window as Window & { requestIdleCallback?: (fn: () => void, opts?: { timeout: number }) => number }
  if (w.requestIdleCallback) {
    w.requestIdleCallback(prefetchVditor, { timeout: 5000 })
  } else {
    window.setTimeout(prefetchVditor, 3000)
  }
})
</script>

<template>
  <el-config-provider :locale="zhCn">
    <router-view />
  </el-config-provider>
</template>

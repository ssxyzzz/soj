<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AmbientGrid from '@/components/AmbientGrid.vue'
import TierBadge from '@/components/TierBadge.vue'
import { authApi, notifyApi } from '@/api'
import { notifyTypeMeta } from '@/components/discuss/discussMeta'
import { useLevelsStore } from '@/stores/levels'
import { useSiteStore } from '@/stores/site'
import { useUserStore } from '@/stores/user'
import { applyTheme, initTheme, type ThemeName } from '@/utils/theme'
import { APP_VERSION as appVersion } from '@/utils/version'
import { fmtRel } from '@/utils/format'
import type { NotificationItem } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const levels = useLevelsStore()
const site = useSiteStore()

const active = computed(() => route.path)
const adminActive = computed(() => route.path.startsWith('/admin'))

// 站内通知铃铛（v0.24）：未读角标 60s 轮询 + 下拉最近 10 条；点条目跳深链并标已读
const unread = ref(0)
const recent = ref<NotificationItem[]>([])

async function refreshBadge() {
  if (!userStore.token) return
  try {
    const b = await notifyApi.badge()
    unread.value = b.unread
  } catch {
    /* 静默 */
  }
}

async function loadRecent() {
  if (!userStore.token) return
  try {
    const r = await notifyApi.list({ page: 1 })
    recent.value = r.list.slice(0, 10)
    unread.value = r.unread
  } catch {
    /* 静默 */
  }
}

let badgePoll: ReturnType<typeof setInterval> | undefined

function onBellVisible() {
  loadRecent()
}

async function openNotification(n: NotificationItem) {
  if (!n.read) {
    n.read = true
    unread.value = Math.max(0, unread.value - 1)
    try {
      await notifyApi.read([n.id])
    } catch {
      /* 静默 */
    }
  }
  if (n.link) router.push(n.link)
  else router.push('/notifications')
}

async function readAllRecent() {
  try {
    await notifyApi.read()
    await loadRecent()
  } catch {
    /* 静默 */
  }
}

// 站点主题（2026-10-26）：v5 新案（默认）/ 原版 · 深海军蓝，页脚切换、localStorage 持久化
const theme = ref<ThemeName>(initTheme())
function switchTheme(t: ThemeName) {
  applyTheme(t)
  theme.value = t
}

// 头像首字（v3 .avatar：渐变圆 + 昵称首字；昵称为空兜底「我」）
const avatarChar = computed(() => {
  const n = userStore.nickname || '我'
  return n.slice(0, 1).toUpperCase()
})

// 登录态下拉取用户偏好（默认语言/模板）与等级（顶栏徽章），供提交弹窗/在线编程消费；
// 设置页保存后同步更新 store。失败静默（未登录/过期由拦截器统一处理）。
onMounted(() => {
  levels.ensure() // 等级名字配置（公开接口，徽章渲染用）
  site.ensure() // 注册开关（公开接口，顶栏按钮文案用）
  if (!userStore.token) return
  authApi
    .getProfile()
    .then((p) => {
      userStore.setPrefs(p.prefs ?? {})
      userStore.setLevel(p.level ?? null)
      userStore.setAvatarUrl(p.avatar_url ?? '') // QQ 头像（v0.23）：顶栏同步
    })
    .catch(() => {})
  refreshBadge() // 通知角标首刷（v0.24）
  badgePoll = setInterval(refreshBadge, 60_000)
})

onUnmounted(() => {
  if (badgePoll) clearInterval(badgePoll)
})

function onCommand(command: string) {
  if (command === 'me') {
    router.push(`/users/${userStore.uid}`) // 公开主页（v0.17）
  } else if (command === 'settings') {
    router.push('/settings')
  } else if (command === 'logout') {
    userStore.logout()
    router.push('/login')
  }
}
</script>

<template>
  <el-container class="layout">
    <!-- 全站网格氛围（2026-10-25 修复⑯）：网格在鼠标处显影，静态画布撤下；teleport 到 body -->
    <AmbientGrid />
    <el-header class="header">
      <div class="header-inner">
        <!-- 品牌：渐变闪电标志（与 /favicon.svg 同源）+ 流彩渐变字 -->
        <div class="brand" @click="router.push('/')">
          <img class="brand-mark" src="/favicon.svg" alt="SOJ" /><span class="brand-word">SOJ</span>
        </div>
        <el-menu mode="horizontal" router :default-active="active" class="menu" :ellipsis="false">
          <el-menu-item index="/">首页</el-menu-item>
          <el-menu-item index="/problems">题库</el-menu-item>
          <el-menu-item index="/lists">题单</el-menu-item>
          <el-menu-item index="/contests">比赛</el-menu-item>
          <el-menu-item index="/classes">班级</el-menu-item>
          <el-menu-item v-if="site.discussGlobalEnabled" index="/discuss">讨论</el-menu-item>
          <el-menu-item index="/rank">排名</el-menu-item>
          <el-menu-item v-if="userStore.can('submission.view.all')" index="/submissions">提交记录</el-menu-item>
        </el-menu>
        <router-link v-if="userStore.canAdmin" to="/admin" class="admin-link" :class="{ on: adminActive }">
          管理后台
        </router-link>
        <template v-if="userStore.token">
          <!-- 通知铃铛（v0.24）：hover 下拉最近 10 条 + 全部已读 + 消息中心入口 -->
          <el-popover placement="bottom-end" :width="360" trigger="hover" @show="onBellVisible">
            <template #reference>
              <button type="button" class="bell" :class="{ ringing: unread > 0 }" title="消息中心">
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
                  <path d="M13.7 21a2 2 0 0 1-3.4 0" />
                </svg>
                <span v-if="unread > 0" class="bell-badge">{{ unread > 99 ? '99+' : unread }}</span>
              </button>
            </template>
            <div class="bell-panel">
              <div class="bell-head">
                <span class="bh-title">通知</span>
                <el-button v-if="unread > 0" text size="small" type="primary" @click="readAllRecent">全部已读</el-button>
              </div>
              <el-empty
                v-if="!recent.length"
                description="暂无通知"
                :image-size="48"
                style="padding: 12px 0"
              />
              <div
                v-for="n in recent"
                :key="n.id"
                class="bell-item"
                :class="{ unread: !n.read }"
                @click="openNotification(n)"
              >
                <span class="bi-icon">{{ notifyTypeMeta(n.type).icon }}</span>
                <div class="bi-body">
                  <div class="bi-title">{{ n.title }}</div>
                  <div class="bi-text">{{ n.body }}</div>
                </div>
                <span class="bi-time">{{ fmtRel(n.created_at) }}</span>
              </div>
              <div class="bell-foot">
                <el-button text size="small" type="primary" @click="router.push('/notifications')">
                  查看全部 →
                </el-button>
              </div>
            </div>
          </el-popover>
          <el-dropdown class="user-dropdown" @command="onCommand">
            <span class="user-chip">
              <img v-if="userStore.avatarUrl" :src="userStore.avatarUrl" alt="" class="avatar avatar-img" />
              <span v-else class="avatar">{{ avatarChar }}</span>
              <span class="uname">{{ userStore.nickname || '已登录' }}</span>
              <TierBadge
                v-if="userStore.level?.level"
                :tier="userStore.level.level"
                kind="level"
                size="sm"
              />
            </span>
            <template #dropdown>
            <el-dropdown-menu>
              <!-- v0.17：公开主页入口 -->
              <el-dropdown-item command="me">我的主页</el-dropdown-item>
              <el-dropdown-item command="settings">个人设置</el-dropdown-item>
              <el-dropdown-item command="logout" divided>退出登录</el-dropdown-item>
            </el-dropdown-menu>
            </template>
          </el-dropdown>
        </template>
        <el-button v-else type="primary" @click="router.push('/login')">
          {{ site.registerOpen ? '登录 / 注册' : '登录' }}
        </el-button>
      </div>
    </el-header>
    <el-main class="main">
      <router-view />
    </el-main>
    <el-footer class="footer">
      <!-- 页脚版本与仓库链接（2026-10-26）：SOJ+版本整体外链仓库，竖线分段；版本号发版时改 utils/version.ts 一处 -->
      <a class="flink" href="https://github.com/ssxyzzz/soj" target="_blank" rel="noopener noreferrer">SOJ · {{ appVersion }}</a>
      <span class="fsep">|</span>
      <span>Gin + Vue 3 + MongoDB + go-judge 在线判题系统</span>
      <span class="fsep">|</span>
      <!-- 主题切换（2026-10-26）：v5 版（默认）/ 原版，data-theme 整站变量换血 -->
      <span class="theme-switch">
        <span class="ts-label">主题    </span>
        <button type="button" :class="{ on: theme === 'v5' }" @click="switchTheme('v5')">v5 版</button>
        <button type="button" :class="{ on: theme === 'navy' }" @click="switchTheme('navy')">原版</button>
      </span>
      <!-- ICP 备案号（v0.13）：后台「系统设置」配置，空 = 不展示；法规要求链接工信部备案系统 -->
      <a
        v-if="site.icpNumber"
        class="icp"
        href="https://beian.miit.gov.cn/"
        target="_blank"
        rel="noopener noreferrer"
      >{{ site.icpNumber }}</a>
    </el-footer>
  </el-container>
</template>

<style scoped>
.layout {
  min-height: 100vh;
}

/* v3-full 玻璃顶栏：半透明白 + backdrop blur，sticky 悬浮于画布之上（底边 .07） */
.header {
  position: sticky;
  top: 0;
  z-index: 50;
  background: rgba(251, 253, 255, 0.72);
  -webkit-backdrop-filter: blur(16px) saturate(1.5);
  backdrop-filter: blur(16px) saturate(1.5);
  border-bottom: 1px solid rgba(11, 31, 69, 0.07);
  padding: 0;
  height: 64px;
}

/* v3-full：内容 1440 + 左右 24（2026-10-25 容器回调 1280→1440，与 page-shell 同步） */
.header-inner {
  box-sizing: border-box;
  max-width: 1440px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  height: 100%;
  padding: 0 24px;
  gap: 6px;
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  margin-right: 18px;
  white-space: nowrap;
}

/* 品牌标志：渐变闪电 favicon 同源（透明底），34px */
.brand-mark {
  width: 34px;
  height: 34px;
  border-radius: 10px;
}

.brand-word {
  font-size: 24px;
  font-weight: var(--soj-fw-heavy);
  letter-spacing: 1px;
  /* 斜向单向流彩（2026-10-27 终版三）：真正的坑不是渐变周期，而是背景图的「盒子平铺」——
     渐变图按元素盒大小裁切后 repeat，盒子宽度不与轴周期对齐时，平铺交界线扫过元素
     即一道全色周期硬跳（前两版的接缝与「刷新感」皆源于此）。
     解法 = 固定背景图尺寸使其四边与轴周期对齐：轴向周期 P = 51.9615px（= 60px × sin120°，
     青蓝亮蓝三段循环首色尾接），background-size 60 × 103.923px 满足 60·sin120° = P、
     103.923·cos120°投影 = P（横竖平铺缝均颜色连续）；keyframe 每轮位移恰好一个图宽 60px，
     循环首末帧全等，linear infinite 逐帧无缝 */
  background: repeating-linear-gradient(
    120deg,
    #16a7d2 0,
    #1557b8 17.3205px,
    #2c5de0 34.641px,
    #16a7d2 51.9615px
  );
  background-size: 60px 103.923px;
  background-position: 0 0;
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  animation: brand-flow 10s linear infinite;
}

@keyframes brand-flow {
  to {
    background-position: 60px 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .brand-word {
    animation: none;
    background-position: 50% 0;
  }
}

.menu {
  flex: 1;
  border-bottom: none;
  background: transparent;
  height: 100%;
}

/* v3-full 菜单胶囊（.nav-links a：8px 15px、15px 字号，hover/on 浅蓝底）：项高自适应居中 */
.menu :deep(.el-menu-item) {
  align-self: center;
  height: auto;
  line-height: 1.2;
  padding: 8px 15px;
  border-radius: 999px;
  margin: 0 1px;
  border-bottom: none;
  transition: background-color 0.18s, color 0.18s;
}

.menu :deep(.el-menu-item:hover),
.menu :deep(.el-menu-item:focus) {
  background-color: var(--soj-blue-soft);
  border-bottom: none;
}

.menu :deep(.el-menu-item.is-active) {
  background-color: var(--soj-blue-soft);
  color: var(--soj-blue);
  font-weight: var(--soj-fw-bold);
  border-bottom: none;
}

/* v3-full .admin-link：独立描边胶囊（激活 = 蓝边蓝字浅蓝底），不再借 el-menu 形态 */
.admin-link {
  flex: 0 0 auto;
  font-size: var(--soj-fs-14);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-ink-2);
  padding: 7px 14px;
  border-radius: 999px;
  border: 1px solid var(--soj-line-2);
  text-decoration: none;
  transition: all 0.18s;
  white-space: nowrap;
}

.admin-link:hover,
.admin-link.on {
  border-color: var(--soj-blue-line);
  color: var(--soj-blue);
  background: var(--soj-blue-soft);
}

/* v0.24 通知铃铛：描边圆钮 + 未读角标；hover 下拉面板列表 */
.bell {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  border: 1px solid var(--soj-line-2);
  background: transparent;
  color: var(--soj-ink-2);
  cursor: pointer;
  transition: all 0.18s;
  flex: 0 0 auto;
}

.bell:hover,
.bell.ringing {
  color: var(--soj-blue);
  border-color: var(--soj-blue-line);
  background: var(--soj-blue-soft);
}

.bell-badge {
  position: absolute;
  top: -5px;
  right: -6px;
  min-width: 17px;
  height: 17px;
  padding: 0 4px;
  border-radius: 999px;
  background: var(--el-color-danger, #f56c6c);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  line-height: 17px;
  text-align: center;
}

.bell-panel {
  margin: -6px;
}

.bell-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 2px 6px 8px;
  border-bottom: 1px solid var(--soj-line);
}

.bh-title {
  font-weight: var(--soj-fw-emphasis);
  font-size: 14px;
}

.bell-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 8px 6px;
  border-radius: 8px;
  cursor: pointer;
}

.bell-item:hover {
  background: var(--soj-blue-soft);
}

.bell-item.unread .bi-title {
  font-weight: var(--soj-fw-emphasis);
}

.bi-icon {
  font-size: 15px;
  line-height: 1.5;
}

.bi-body {
  flex: 1;
  min-width: 0;
}

.bi-title {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bi-text {
  font-size: 12px;
  color: var(--soj-ink-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bi-time {
  font-size: 11px;
  color: var(--soj-ink-3);
  white-space: nowrap;
  flex-shrink: 0;
}

.bell-foot {
  border-top: 1px solid var(--soj-line);
  padding-top: 6px;
  text-align: center;
}

/* v3-full .user-chip：白底描边胶囊（渐变圆头像 30px + 昵称 700 + 等级徽章同装一体） */
.user-dropdown {
  height: 100%;
  display: inline-flex;
  align-items: center;
  outline: none;
}

.user-chip {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  padding: 5px 14px 5px 5px;
  border-radius: 999px;
  border: 1px solid var(--soj-line);
  background: var(--el-fill-color-blank);
  cursor: pointer;
  box-shadow: 0 1px 2px rgba(11, 31, 69, 0.05);
  transition: box-shadow 0.18s, border-color 0.18s;
  outline: none;
  white-space: nowrap;
}

.user-chip:hover {
  border-color: var(--soj-blue-line);
  box-shadow: 0 2px 8px rgba(11, 31, 69, 0.09);
}

.avatar {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-teal));
  color: #fff;
  font-weight: var(--soj-fw-bold);
  font-size: var(--soj-fs-14);
}

/* QQ 头像版顶栏头像（v0.23）：同径圆 + 细描边 */
.avatar-img {
  object-fit: cover;
  border: 1px solid var(--soj-line);
  background: #fff;
}

.uname {
  font-weight: var(--soj-fw-bold);
  font-size: var(--soj-fs-14);
  color: var(--soj-navy);
}

/* 透明画布：body 三层画布（渐变+光晕+青网格）直接透出，main/footer 不再铺实底 */
.main {
  background: transparent;
  /* el-main 默认 overflow:auto——哪怕实际滚动在 document，也会让页内
     position:sticky（题单目录卡等）失效（auto 祖先劫持 sticky 基准）。
     页面横向溢出已由 .page/.main-col 的 overflow-x:clip 兜住，这里放开 */
  overflow: visible;
}

.footer {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 18px;
  flex-wrap: wrap;
  font-size: 12.5px;
  color: var(--soj-ink-3);
  background: transparent;
  /* v3 页脚：hairline 顶边收束页面 */
  border-top: 1px solid var(--soj-line);
}

/* 主题切换（2026-10-26）：页脚弱化文案钮，选中态走主蓝浅底胶囊 */
.theme-switch {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.ts-label {
  color: var(--soj-ink-3);
}

.theme-switch button {
  border: 1px solid transparent;
  background: transparent;
  border-radius: 999px;
  padding: 3px 10px;
  font: inherit;
  font-size: 12px;
  color: var(--soj-ink-3);
  cursor: pointer;
  transition: all 0.15s;
}

.theme-switch button:hover {
  color: var(--soj-blue);
}

.theme-switch button.on {
  color: var(--soj-blue);
  background: var(--soj-blue-soft);
  border-color: var(--soj-blue-line);
  font-weight: 600;
}

/* 页脚版本与仓库链接：整体外链不加粗，悬停提主蓝 */
.flink {
  color: var(--soj-ink-3);
  text-decoration: none;
}

.flink:hover {
  color: var(--soj-blue);
}

/* 竖线分段：弱化 */
.fsep {
  color: var(--soj-ink-3);
  opacity: 0.55;
}

/* 备案号外链：弱化为页脚次要信息，悬停才提色 */
.icp {
  color: var(--soj-ink-3);
  text-decoration: none;
}

.icp:hover {
  color: var(--soj-blue);
}
</style>

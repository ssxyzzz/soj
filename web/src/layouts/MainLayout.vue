<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { UserFilled } from '@element-plus/icons-vue'

import { authApi } from '@/api'
import UserNameLevel from '@/components/UserNameLevel.vue'
import { LEVEL_STARS, useLevelsStore } from '@/stores/levels'
import { useSiteStore } from '@/stores/site'
import { useUserStore } from '@/stores/user'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const levels = useLevelsStore()
const site = useSiteStore()

const active = computed(() => route.path)

// 左上角 SOJ 标识：特效开 = 最高等级（Lv.10 极境）样式——彩色渐变流动胶囊 + 深处辉光呼吸
// + 四颗错拍白星芒（ulv-tag/ulv-10/ulv-e10 同体系）；后台等级面板特效开关（全局或 Lv.10）
// 关闭即回落站点主色静态字
const brandFx = computed(() => levels.effectsOn(10))

// 极境四星（与 LEVEL_STARS[10] 同配置，随胶囊尺寸铺四角）
const brandStars = computed(() => {
  if (!brandFx.value) return []
  return (
    LEVEL_STARS[10] ?? []
  ).map((s) => {
    let style = `font-size:${s.s}px;animation-delay:${s.d}s;`
    if (s.t != null) style += `top:${s.t}px;`
    if (s.l != null) style += `left:${s.l}px;`
    if (s.r != null) style += `right:${s.r}px;`
    if (s.b != null) style += `bottom:${s.b}px;`
    return style
  })
})

// 登录态下拉取用户偏好（默认语言/模板）与等级（顶栏徽章），供提交弹窗/在线编程消费；
// 设置页保存后同步更新 store。失败静默（未登录/过期由拦截器统一处理）。
onMounted(() => {
  levels.ensure() // 等级名字/特效配置（公开接口，徽章渲染用）
  site.ensure() // 注册开关（公开接口，顶栏按钮文案用）
  if (!userStore.token) return
  authApi
    .getProfile()
    .then((p) => {
      userStore.setPrefs(p.prefs ?? {})
      userStore.setLevel(p.level ?? null)
    })
    .catch(() => {})
})

function onCommand(command: string) {
  if (command === 'settings') {
    router.push('/settings')
  } else if (command === 'logout') {
    userStore.logout()
    router.push('/login')
  }
}
</script>

<template>
  <el-container class="layout">
    <el-header class="header">
      <div class="header-inner">
        <div
          class="brand"
          :class="brandFx ? ['brand', 'brand-fx', 'ulv-tag', 'ulv-10', 'ulv-e10'] : ['brand']"
          @click="router.push('/')"
        >
          SOJ<i
            v-for="(s, i) in brandStars"
            :key="i"
            class="lv-star"
            :style="s"
          >✦</i>
        </div>
        <el-menu mode="horizontal" router :default-active="active" class="menu" :ellipsis="false">
          <el-menu-item index="/">首页</el-menu-item>
          <el-menu-item index="/problems">题库</el-menu-item>
          <el-menu-item index="/contests">比赛</el-menu-item>
          <el-menu-item index="/classes">班级</el-menu-item>
          <el-menu-item index="/rank">排名</el-menu-item>
          <el-menu-item v-if="userStore.can('submission.view.all')" index="/submissions">提交记录</el-menu-item>
        </el-menu>
        <el-menu
          v-if="userStore.canAdmin"
          mode="horizontal"
          router
          :default-active="active"
          class="menu menu-admin"
          :ellipsis="false"
        >
          <el-menu-item index="/admin">管理后台</el-menu-item>
        </el-menu>
        <template v-if="userStore.token">
          <el-dropdown class="user-dropdown" @command="onCommand">
            <span class="username">
              <span class="user-chip"><el-icon><UserFilled /></el-icon></span>
              <UserNameLevel :name="userStore.nickname || '已登录'" :level="userStore.level?.level ?? null" />
            </span>
            <template #dropdown>
            <el-dropdown-menu>
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
      <span>SOJ · Gin + Vue 3 + MongoDB + go-judge 在线判题系统</span>
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

.header {
  background: var(--el-bg-color);
  border-bottom: 1px solid var(--el-border-color-light);
  padding: 0;
  height: 60px;
}

/* 1480px = 1440px 内容列 + 左右各 20px 内边距；再各收 4px（padding 24）做视觉内收——
   卡片边框浅、有圆角阴影会视觉后退，而 SOJ 加粗蓝字和 CJK 用户名边缘更实，
   与内容列严格齐平时顶栏文字观感上会往外凸，故文字比内容列略靠内 4px。
   必须显式 border-box（默认 content-box 会让 max-width 与 padding 相加，左右各多出 20px） */
.header-inner {
  box-sizing: border-box;
  max-width: 1480px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  height: 100%;
  padding: 0 24px;
  gap: 8px;
}

/* 品牌字 22/600 */
.brand {
  font-size: var(--soj-fs-brand);
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-color-primary);
  cursor: pointer;
  margin-right: 16px;
  letter-spacing: 2px;
}

/* SOJ 极境胶囊（特效开）：模板挂全局 ulv-tag/ulv-10/ulv-e10（最高等级样式——蓝紫彩色
   渐变流动 + 深处辉光呼吸）+ 四颗 lv-star 白星芒；胶囊基础形状在此补齐；
   特效关回落上方主色静态字 */
.brand-fx {
  display: inline-flex;
  align-items: center;
  padding: 4px 16px;
  border-radius: 999px;
  color: #fff;
  line-height: 1;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
}

.menu {
  flex: 1;
  border-bottom: none;
}

/* 右侧「管理后台」独立菜单：不参与主菜单的 flex:1 拉伸，紧贴用户名左侧 */
.menu-admin {
  flex: 0 0 auto;
}

/* 用户名下拉触发器复用 el-menu--horizontal 菜单项（如「管理后台」）的视觉参数：
   全高 / 基准内边距 20px / 14px 字号 / 菜单文字色 / 悬停浅底，观感与相邻菜单项一致 */
.user-dropdown {
  height: 100%;
}

.username {
  cursor: pointer;
  outline: none;
  height: 100%;
  box-sizing: border-box;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 0 var(--el-menu-base-level-padding, 20px);
  color: var(--el-menu-text-color, var(--el-text-color-primary));
  border-bottom: 2px solid transparent;
  white-space: nowrap;
  transition:
    border-color var(--el-transition-duration, 0.3s),
    background-color var(--el-transition-duration, 0.3s),
    color var(--el-transition-duration, 0.3s);
}

.username:hover {
  background-color: var(--el-menu-hover-bg-color, var(--el-color-primary-light-9));
  color: var(--el-color-primary);
}

/* 账号圆标：常驻浅主色圆底做视觉锚点；悬停随行整体加深为实心主色 + 图标反白 */
.user-chip {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--soj-fs-body);
  margin-right: 8px;
  transition:
    background-color var(--el-transition-duration, 0.3s),
    color var(--el-transition-duration, 0.3s);
}

.username:hover .user-chip {
  background: var(--el-color-primary);
  color: var(--el-color-white);
}

/* 画布与 body 同源（--el-bg-color-page） */
.main {
  background: var(--el-bg-color-page);
}

.footer {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: var(--el-text-color-secondary);
  background: var(--el-bg-color-page);
}

/* 备案号外链：弱化为页脚次要信息，悬停才提色 */
.icp {
  color: var(--el-text-color-secondary);
  text-decoration: none;
}

.icp:hover {
  color: var(--el-color-primary);
}
</style>

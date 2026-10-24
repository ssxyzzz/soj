<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import TierBadge from '@/components/TierBadge.vue'
import { authApi } from '@/api'
import { useLevelsStore } from '@/stores/levels'
import { useSiteStore } from '@/stores/site'
import { useUserStore } from '@/stores/user'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const levels = useLevelsStore()
const site = useSiteStore()

const active = computed(() => route.path)
const adminActive = computed(() => route.path.startsWith('/admin'))

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
        <!-- 品牌（v3-full 恒定形态）：渐变方块 + 流彩渐变字（旧「极境特效胶囊」态已退役） -->
        <div class="brand" @click="router.push('/')">
          <span class="brand-mark">S</span><span class="brand-word">SOJ</span>
        </div>
        <el-menu mode="horizontal" router :default-active="active" class="menu" :ellipsis="false">
          <el-menu-item index="/">首页</el-menu-item>
          <el-menu-item index="/problems">题库</el-menu-item>
          <el-menu-item index="/contests">比赛</el-menu-item>
          <el-menu-item index="/classes">班级</el-menu-item>
          <el-menu-item index="/rank">排名</el-menu-item>
          <el-menu-item v-if="userStore.can('submission.view.all')" index="/submissions">提交记录</el-menu-item>
        </el-menu>
        <router-link v-if="userStore.canAdmin" to="/admin" class="admin-link" :class="{ on: adminActive }">
          管理后台
        </router-link>
        <template v-if="userStore.token">
          <el-dropdown class="user-dropdown" @command="onCommand">
            <span class="user-chip">
              <span class="avatar">{{ avatarChar }}</span>
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

/* v3-full：内容 1280 + 左右 24（对齐 .topnav-in，弃 1328/28 视觉内收口径） */
.header-inner {
  box-sizing: border-box;
  max-width: 1280px;
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

/* 品牌双色（v3-full）：34px 渐变方块（蓝→青）+ 24/900 流彩渐变字（7s 流动，reduced-motion 退静态） */
.brand-mark {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-teal));
  color: #fff;
  font-size: 15px;
  font-weight: var(--soj-fw-heavy);
  box-shadow: 0 6px 16px rgba(36, 85, 218, 0.32);
}

.brand-word {
  font-size: 24px;
  font-weight: var(--soj-fw-heavy);
  letter-spacing: 1px;
  background: linear-gradient(
    90deg,
    #16a7d2 0%,
    #1557b8 25%,
    #2c5de0 50%,
    #16a7d2 75%,
    #1557b8 100%
  );
  background-size: 220% 100%;
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  animation: brand-flow 7s linear infinite;
}

@keyframes brand-flow {
  to {
    background-position: 220% 0;
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

.uname {
  font-weight: var(--soj-fw-bold);
  font-size: var(--soj-fs-14);
  color: var(--soj-navy);
}

/* 透明画布：body 三层画布（渐变+光晕+青网格）直接透出，main/footer 不再铺实底 */
.main {
  background: transparent;
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

/* 备案号外链：弱化为页脚次要信息，悬停才提色 */
.icp {
  color: var(--soj-ink-3);
  text-decoration: none;
}

.icp:hover {
  color: var(--soj-blue);
}
</style>

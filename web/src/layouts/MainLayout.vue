<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { UserFilled } from '@element-plus/icons-vue'

import { authApi } from '@/api'
import { useUserStore } from '@/stores/user'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const active = computed(() => route.path)

// 登录态下拉取用户偏好（默认语言/模板/MD 编辑模式），供提交弹窗/在线编程/MdEditor 消费；
// 设置页保存后同步更新 store。失败静默（未登录/过期由拦截器统一处理）。
onMounted(() => {
  if (!userStore.token) return
  authApi
    .getProfile()
    .then((p) => userStore.setPrefs(p.prefs ?? {}))
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
        <div class="brand" @click="router.push('/')">SOJ</div>
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
              {{ userStore.nickname || '已登录' }}
            </span>
            <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="settings">个人设置</el-dropdown-item>
              <el-dropdown-item command="logout" divided>退出登录</el-dropdown-item>
            </el-dropdown-menu>
            </template>
          </el-dropdown>
        </template>
        <el-button v-else type="primary" @click="router.push('/login')">登录 / 注册</el-button>
      </div>
    </el-header>
    <el-main class="main">
      <router-view />
    </el-main>
    <el-footer class="footer">SOJ · Gin + Vue 3 + MongoDB + go-judge 在线判题系统</el-footer>
  </el-container>
</template>

<style scoped>
.layout {
  min-height: 100vh;
}

.header {
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
  padding: 0;
  height: 60px;
}

/* 1320px = 1280px 内容列 + 左右各 20px 内边距；再各收 4px（padding 24）做视觉内收——
   卡片边框浅、有圆角阴影会视觉后退，而 SOJ 加粗蓝字和 CJK 用户名边缘更实，
   与内容列严格齐平时顶栏文字观感上会往外凸，故文字比内容列略靠内 4px。
   必须显式 border-box（默认 content-box 会让 max-width 与 padding 相加，左右各多出 20px） */
.header-inner {
  box-sizing: border-box;
  max-width: 1320px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  height: 100%;
  padding: 0 24px;
  gap: 8px;
}

.brand {
  font-size: 22px;
  font-weight: 700;
  color: #409eff;
  cursor: pointer;
  margin-right: 16px;
  letter-spacing: 2px;
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
  padding: 0 var(--el-menu-base-level-padding, 20px);
  font-size: var(--el-menu-item-font-size, 14px);
  color: var(--el-menu-text-color, #303133);
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
  font-size: 15px;
  margin-right: 8px;
  transition:
    background-color var(--el-transition-duration, 0.3s),
    color var(--el-transition-duration, 0.3s);
}

.username:hover .user-chip {
  background: var(--el-color-primary);
  color: #fff;
}

.main {
  background: #f5f7fa;
}

.footer {
  display: flex;
  align-items: center;
  justify-content: center;
  color: #909399;
  font-size: 13px;
  background: #f5f7fa;
}
</style>

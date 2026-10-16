<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { useUserStore } from '@/stores/user'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const active = computed(() => route.path)

function onCommand(command: string) {
  if (command === 'logout') {
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
          <el-dropdown @command="onCommand">
            <span class="username">{{ userStore.nickname || '已登录' }}</span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="logout">退出登录</el-dropdown-item>
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

.username {
  cursor: pointer;
  color: #409eff;
  outline: none;
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

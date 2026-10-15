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
  if (command === 'admin') {
    router.push('/admin')
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
          <el-menu-item index="/submissions">提交记录</el-menu-item>
          <el-menu-item index="/rank">排行榜</el-menu-item>
        </el-menu>
        <el-dropdown v-if="userStore.token" @command="onCommand">
          <span class="username">{{ userStore.nickname || '已登录' }}</span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-if="userStore.role === 'root'" command="admin">管理后台</el-dropdown-item>
              <el-dropdown-item command="logout">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
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

.header-inner {
  max-width: 1080px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  height: 100%;
  padding: 0 16px;
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

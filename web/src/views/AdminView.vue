<script setup lang="ts">
// 管理后台（v0.5e ② 拆分后布局壳）：左列功能卡点选切换，主列渲染对应面板。
// 面板用 v-if 切换（销毁重建、各自拉数据）——顺带解决「角色变更后用户列表刷新」
// 的跨面板联动；面板实现见 components/admin/*，角色清单由 stores/adminRoles 共享。
// 角色管理仅 root 可见（后端同样硬编码 root，增删改不可下放）。
import { ref } from 'vue'

import BackupPanel from '@/components/admin/BackupPanel.vue'
import NoticePanel from '@/components/admin/NoticePanel.vue'
import RolesPanel from '@/components/admin/RolesPanel.vue'
import SettingsPanel from '@/components/admin/SettingsPanel.vue'
import TagsPanel from '@/components/admin/TagsPanel.vue'
import UsersPanel from '@/components/admin/UsersPanel.vue'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const section = ref<'users' | 'roles' | 'tags' | 'notice' | 'settings' | 'backup'>('users')
</script>

<template>
  <div class="admin-layout">
    <aside class="admin-nav">
      <div
        class="nav-item"
        :class="{ active: section === 'users' }"
        @click="section = 'users'"
      >
        <span class="nav-title">编辑用户</span>
        <span class="nav-sub">搜索 / 建号 / 角色 / 重置密码</span>
      </div>
      <div
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'roles' }"
        @click="section = 'roles'"
      >
        <span class="nav-title">角色管理</span>
        <span class="nav-sub">权限位 / 自定义角色</span>
      </div>
      <div
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'tags' }"
        @click="section = 'tags'"
      >
        <span class="nav-title">标签树</span>
        <span class="nav-sub">多级标签 / 大纲编辑</span>
      </div>
      <div
        class="nav-item"
        :class="{ active: section === 'notice' }"
        @click="section = 'notice'"
      >
        <span class="nav-title">公告编辑</span>
        <span class="nav-sub">首页公告（Markdown）</span>
      </div>
      <div
        class="nav-item"
        :class="{ active: section === 'settings' }"
        @click="section = 'settings'"
      >
        <span class="nav-title">系统设置</span>
        <span class="nav-sub">提交限速等</span>
      </div>
      <div
        v-if="userStore.can('backup.run') || userStore.can('backup.keys')"
        class="nav-item"
        :class="{ active: section === 'backup' }"
        @click="section = 'backup'"
      >
        <span class="nav-title">数据备份</span>
        <span class="nav-sub">一键备份 / 恢复方法</span>
      </div>
    </aside>

    <main class="admin-main">
      <UsersPanel v-if="section === 'users'" />
      <RolesPanel v-else-if="section === 'roles'" />
      <TagsPanel v-else-if="section === 'tags'" />
      <NoticePanel v-else-if="section === 'notice'" />
      <SettingsPanel v-else-if="section === 'settings'" />
      <BackupPanel v-else-if="section === 'backup'" />
    </main>
  </div>
</template>

<style scoped>
.admin-layout {
  max-width: 1280px;
  margin: 0 auto;
  display: flex;
  gap: 24px;
  /* stretch：主列卡片随左列高度拉伸，两列上沿与下沿对齐；
     不用 sticky——切换分区时内容高度变化会让吸附列来回跳动 */
  align-items: stretch;
}

.admin-nav {
  flex: 0 0 300px;
  width: 300px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.nav-item {
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--el-color-primary-light-7);
  border-radius: 8px;
  padding: 14px 16px;
  cursor: pointer;
  transition: all 0.2s;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.nav-item:hover {
  border-color: var(--el-color-primary);
}

.nav-item.active {
  background: var(--el-color-primary);
  border-color: var(--el-color-primary);
}

.nav-item.active .nav-title,
.nav-item.active .nav-sub {
  color: #fff;
}

.nav-title {
  font-weight: 600;
  font-size: 15px;
  color: var(--el-text-color-primary);
}

.nav-sub {
  font-size: 15px;
  color: var(--el-text-color-secondary);
}

.admin-main {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 420px;
  display: flex;
  flex-direction: column;
}

.admin-main :deep(> .el-card) {
  flex: 1 1 auto;
}

@media (max-width: 992px) {
  .admin-layout {
    flex-direction: column;
  }

  .admin-nav {
    position: static;
    width: auto;
    flex: 1 1 auto;
    flex-direction: row;
    flex-wrap: wrap;
  }

  .nav-item {
    flex: 1 1 140px;
  }
}
</style>

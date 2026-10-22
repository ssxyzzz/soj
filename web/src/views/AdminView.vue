<script setup lang="ts">
// 管理后台（v0.5.4 ② 拆分后布局壳）：左列功能卡点选切换，主列渲染对应面板。
// 面板用 v-if 切换（销毁重建、各自拉数据）——顺带解决「角色变更后用户列表刷新」
// 的跨面板联动；面板实现见 components/admin/*，角色清单由 stores/adminRoles 共享。
// 角色管理仅 root 可见（后端同样硬编码 root，增删改不可下放）。
import { ref } from 'vue'
import { ElMessageBox } from 'element-plus'

import BackupPanel from '@/components/admin/BackupPanel.vue'
import LevelPanel from '@/components/admin/LevelPanel.vue'
import NoticePanel from '@/components/admin/NoticePanel.vue'
import RolesPanel from '@/components/admin/RolesPanel.vue'
import SecurityLogsPanel from '@/components/admin/SecurityLogsPanel.vue'
import SettingsPanel from '@/components/admin/SettingsPanel.vue'
import SourcesPanel from '@/components/admin/SourcesPanel.vue'
import TagsPanel from '@/components/admin/TagsPanel.vue'
import UsersPanel from '@/components/admin/UsersPanel.vue'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const section = ref<'users' | 'roles' | 'tags' | 'sources' | 'levels' | 'security' | 'notice' | 'settings' | 'backup'>('users')

// 标签树面板草稿的脏状态：切走分区前拦截确认（面板 v-if 销毁，草稿随组件丢失）
const tagsDirty = ref(false)

async function switchSection(target: typeof section.value) {
  if (section.value === 'tags' && tagsDirty.value && target !== 'tags') {
    try {
      await ElMessageBox.confirm('标签树有未保存的修改，离开后将丢失。确定离开？', '未保存的修改', {
        type: 'warning',
        confirmButtonText: '离开',
        cancelButtonText: '留下',
      })
    } catch {
      return
    }
  }
  tagsDirty.value = false
  section.value = target
}
</script>

<template>
  <div class="admin-layout">
    <aside class="admin-nav">
      <div
        class="nav-item"
        :class="{ active: section === 'users' }"
        @click="switchSection('users')"
      >
        <span class="nav-title">编辑用户</span>
        <span class="nav-sub">搜索 / 建号 / 角色 / 重置密码</span>
      </div>
      <div
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'roles' }"
        @click="switchSection('roles')"
      >
        <span class="nav-title">角色管理</span>
        <span class="nav-sub">权限位 / 自定义角色</span>
      </div>
      <div
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'tags' }"
        @click="switchSection('tags')"
      >
        <span class="nav-title">标签树</span>
        <span class="nav-sub">多级标签 / 卡片编辑</span>
      </div>
      <div
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'sources' }"
        @click="switchSection('sources')"
      >
        <span class="nav-title">来源库</span>
        <span class="nav-sub">来源登记 / 转为标签</span>
      </div>
      <div
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'levels' }"
        @click="switchSection('levels')"
      >
        <span class="nav-title">等级设置</span>
        <span class="nav-sub">名称 / 阈值 / 特效开关</span>
      </div>
      <div
        class="nav-item"
        :class="{ active: section === 'notice' }"
        @click="switchSection('notice')"
      >
        <span class="nav-title">公告编辑</span>
        <span class="nav-sub">首页公告（Markdown）</span>
      </div>
      <div
        class="nav-item"
        :class="{ active: section === 'settings' }"
        @click="switchSection('settings')"
      >
        <span class="nav-title">系统设置</span>
        <span class="nav-sub">提交限速等</span>
      </div>
      <div
        v-if="userStore.can('backup.run') || userStore.can('backup.keys')"
        class="nav-item"
        :class="{ active: section === 'backup' }"
        @click="switchSection('backup')"
      >
        <span class="nav-title">数据备份</span>
        <span class="nav-sub">一键备份 / 恢复方法</span>
      </div>
      <div
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'security' }"
        @click="switchSection('security')"
      >
        <span class="nav-title">安全日志</span>
        <span class="nav-sub">登录 / 锁定 / 改密 / 限流</span>
      </div>
    </aside>

    <main class="admin-main">
      <UsersPanel v-if="section === 'users'" />
      <RolesPanel v-else-if="section === 'roles'" />
      <TagsPanel v-else-if="section === 'tags'" @update:dirty="tagsDirty = $event" />
      <SourcesPanel v-else-if="section === 'sources'" />
      <LevelPanel v-else-if="section === 'levels'" />
      <NoticePanel v-else-if="section === 'notice'" />
      <SettingsPanel v-else-if="section === 'settings'" />
      <BackupPanel v-else-if="section === 'backup'" />
      <SecurityLogsPanel v-else-if="section === 'security'" />
    </main>
  </div>
</template>

<style scoped>
.admin-layout {
  max-width: 1440px;
  margin: 0 auto;
  display: flex;
  gap: 24px;
  /* stretch：主列卡片随左列高度拉伸，两列上沿与下沿对齐；
     不用 sticky——切换分区时内容高度变化会让吸附列来回跳动 */
  align-items: stretch;
}

.admin-nav {
  flex: 0 0 320px;
  width: 320px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.nav-item {
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--el-color-primary-light-7);
  border-radius: var(--el-border-radius-base);
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
  color: var(--el-color-white);
}

.nav-title {
  font-weight: var(--soj-fw-emphasis);
  color: var(--el-text-color-primary);
}

.nav-sub {
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

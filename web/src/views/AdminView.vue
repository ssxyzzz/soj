<script setup lang="ts">
// 管理后台（v0.5.4 ② 拆分后布局壳）：左列功能卡点选切换，主列渲染对应面板。
// 面板用 v-if 切换（销毁重建、各自拉数据）——顺带解决「角色变更后用户列表刷新」
// 的跨面板联动；面板实现见 components/admin/*，角色清单由 stores/adminRoles 共享。
// 角色管理仅 root 可见（后端同样硬编码 root，增删改不可下放）。
// ?section= 深链（v0.15）：题库页「导入题目」等入口直达对应分区。
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessageBox } from 'element-plus'

import BackupPanel from '@/components/admin/BackupPanel.vue'
import DiscussPanel from '@/components/admin/DiscussPanel.vue'
import DuplicateProblemsPanel from '@/components/admin/DuplicateProblemsPanel.vue'
import LevelPanel from '@/components/admin/LevelPanel.vue'
import NoticePanel from '@/components/admin/NoticePanel.vue'
import RolesPanel from '@/components/admin/RolesPanel.vue'
import SecurityLogsPanel from '@/components/admin/SecurityLogsPanel.vue'
import SettingsPanel from '@/components/admin/SettingsPanel.vue'
import TagsPanel from '@/components/admin/TagsPanel.vue'
import UsersPanel from '@/components/admin/UsersPanel.vue'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const route = useRoute()

const sections = ['users', 'roles', 'problems', 'levels', 'security', 'notice', 'discuss', 'settings', 'backup'] as const
type Section = (typeof sections)[number]
// 题目管理（v0.18.1 聚合）：知识树 / 标签树 / 重复题扫描三面板同页 tab 切换（2026-10-25 双树）；
// tags/sources 旧深链值映射到 problems 的对应子页
type SubTab = 'tags' | 'meta' | 'duplicates'
const rawSection = (Array.isArray(route.query.section) ? route.query.section[0] : route.query.section) as string | undefined
const isLegacySub = rawSection === 'tags' || rawSection === 'sources' || rawSection === 'meta'
const section = ref<Section>(
  rawSection && (sections.includes(rawSection as Section) || isLegacySub) ? 'problems' : 'users',
)
const subTab = ref<SubTab>(
  rawSection === 'meta' ? 'meta' : isLegacySub ? 'tags' : rawSection === 'duplicates' ? 'duplicates' : 'tags',
)
// 题目管理入口：root（双树）∨ problem.edit.all（重复扫描）
const canProblemManage = userStore.role === 'root' || userStore.can('problem.edit.all')
const showTagsTab = userStore.role === 'root'
const showDuplicatesTab = userStore.can('problem.edit.all')

// 标签树面板草稿的脏状态：切走分区/子页前拦截确认（面板 v-if 销毁，草稿随组件丢失）
const tagsDirty = ref(false)

async function guardTagsDirty(): Promise<boolean> {
  if (subTab.value !== 'tags' || !tagsDirty.value) return true
  try {
    await ElMessageBox.confirm('标签树有未保存的修改，离开后将丢失。确定离开？', '未保存的修改', {
      type: 'warning',
      confirmButtonText: '离开',
      cancelButtonText: '留下',
    })
  } catch {
    return false
  }
  tagsDirty.value = false
  return true
}

async function switchSection(target: typeof section.value) {
  if (section.value === 'problems' && target !== 'problems') {
    if (!(await guardTagsDirty())) return
  }
  section.value = target
}

async function switchSubTab(target: SubTab) {
  if (subTab.value === 'tags' && target !== 'tags') {
    if (!(await guardTagsDirty())) return
  }
  subTab.value = target
}
</script>

<template>
  <div class="admin-wrap">
    <div class="page-head">
      <div class="kicker">ADMIN</div>
      <h1 class="page-title">管理后台<small>{{ userStore.role === 'root' ? 'root · 全部面板可用' : '管理面板' }}</small></h1>
    </div>
    <div class="admin-layout">
    <aside class="admin-nav">
      <div class="anav-eyebrow">面板</div>
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
        v-if="canProblemManage"
        class="nav-item"
        :class="{ active: section === 'problems' }"
        @click="switchSection('problems')"
      >
        <span class="nav-title">题目管理</span>
        <span class="nav-sub">知识树 / 标签树 / 重复扫描</span>
      </div>
      <div
        v-if="userStore.role === 'root'"
        class="nav-item"
        :class="{ active: section === 'levels' }"
        @click="switchSection('levels')"
      >
        <span class="nav-title">等级设置</span>
        <span class="nav-sub">用户等级 / 难度等级</span>
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
        v-if="userStore.can('discussion.manage')"
        class="nav-item"
        :class="{ active: section === 'discuss' }"
        @click="switchSection('discuss')"
      >
        <span class="nav-title">讨论管理</span>
        <span class="nav-sub">举报队列 / 禁言</span>
      </div>
      <div
        class="nav-item"
        :class="{ active: section === 'settings' }"
        @click="switchSection('settings')"
      >
        <span class="nav-title">系统设置</span>
        <span class="nav-sub">基础 / 邮箱 / 讨论区 / 账号</span>
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
      <div v-else-if="section === 'problems'" class="problem-manage">
        <el-tabs :model-value="subTab" @tab-change="switchSubTab as never">
          <el-tab-pane v-if="showTagsTab" name="tags" label="知识树" />
          <el-tab-pane v-if="showTagsTab" name="meta" label="标签树" />
          <el-tab-pane v-if="showDuplicatesTab" name="duplicates" label="重复题扫描" />
        </el-tabs>
        <TagsPanel v-if="subTab === 'tags'" @update:dirty="tagsDirty = $event" />
        <TagsPanel v-else-if="subTab === 'meta'" tree="meta" @update:dirty="tagsDirty = $event" />
        <DuplicateProblemsPanel v-else-if="subTab === 'duplicates'" />
      </div>
      <LevelPanel v-else-if="section === 'levels'" />
      <NoticePanel v-else-if="section === 'notice'" />
      <DiscussPanel v-else-if="section === 'discuss'" />
      <SettingsPanel v-else-if="section === 'settings'" />
      <BackupPanel v-else-if="section === 'backup'" />
      <SecurityLogsPanel v-else-if="section === 'security'" />
    </main>
    </div>
  </div>
</template>

<style scoped>
/* v3 后台布局：页头（kicker + 大标题）+ 白卡导航列 + 主列，容器 1440 对齐全站（2026-10-25 回调） */
.admin-wrap {
  max-width: 1440px;
  margin: 0 auto;
}

.admin-layout {
  display: flex;
  gap: 22px;
  /* stretch：主列卡片随左列高度拉伸，两列上沿与下沿对齐；
     不用 sticky——切换分区时内容高度变化会让吸附列来回跳动 */
  align-items: stretch;
}

/* v3 导航列：整列一张白卡（圆角 16 + hairline + 柔影），条目胶囊态；
   宽度对齐 PageShell 右列 320px（2026-10-28 反馈：252 偏窄） */
.admin-nav {
  flex: 0 0 320px;
  width: 320px;
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 10px;
  background: var(--el-bg-color);
  border: 1px solid var(--soj-line);
  border-radius: 16px;
  box-shadow:
    0 1px 2px rgba(11, 31, 69, 0.04),
    0 12px 32px rgba(11, 31, 69, 0.06);
}

/* v3 .anav-t：11px 大写 eyebrow */
.anav-eyebrow {
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  letter-spacing: 0.16em;
  color: var(--soj-ink-3);
  text-transform: uppercase;
  padding: 8px 13px 6px;
}

.nav-item {
  border: 1px solid transparent;
  border-radius: 10px;
  padding: 10px 13px;
  cursor: pointer;
  transition: all 0.15s;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item:hover {
  background: var(--soj-surface-2);
}

.nav-item.active {
  background: var(--soj-blue-soft);
}

.nav-item.active .nav-title {
  color: var(--soj-blue);
}

.nav-item.active .nav-sub {
  color: var(--soj-blue);
  opacity: 0.75;
}

.nav-title {
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  font-size: 14px;
}

.nav-sub {
  color: var(--el-text-color-secondary);
  font-size: var(--soj-fs-small);
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

  .problem-manage {
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    min-height: 0;
  }

  .problem-manage > :deep(.el-tabs) {
    flex: 0 0 auto;
  }

  .problem-manage > :deep(.el-card) {
    flex: 1 1 auto;
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

<script setup lang="ts">
// 管理后台（v0.35-① 信息架构重组）：左列导航按 人员/内容/运营/系统 四组眉题分区，
// 条目单行化（去副标题）+ 待办徽标（讨论区=待处理举报、兑换商店=待发货订单，
// 进页拉一次、每次切分区后刷新——面板 v-if 销毁重建，处理结果即时反映到徽标）。
// 面板仍 v-if 切换（各自拉数据），实现见 components/admin/*，角色清单由 stores/adminRoles 共享。
// ?section= 深链：旧值（tags/sources/meta/duplicates/seed）映射进 problems 分区；
// 切换分区回写 URL（仿设置页 ?tab= 模式，刷新/分享保持当前分区）。
// 角色管理仅 root 可见（后端同样硬编码 root，增删改不可下放）。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'

import BackupPanel from '@/components/admin/BackupPanel.vue'
import DiscussPanel from '@/components/admin/DiscussPanel.vue'
import DuplicateProblemsPanel from '@/components/admin/DuplicateProblemsPanel.vue'
import LevelPanel from '@/components/admin/LevelPanel.vue'
import NoticePanel from '@/components/admin/NoticePanel.vue'
import RolesPanel from '@/components/admin/RolesPanel.vue'
import SecurityLogsPanel from '@/components/admin/SecurityLogsPanel.vue'
import SeedProblemsPanel from '@/components/admin/SeedProblemsPanel.vue'
import SettingsPanel from '@/components/admin/SettingsPanel.vue'
import ShopPanel from '@/components/admin/ShopPanel.vue'
import AiPanel from '@/components/admin/AiPanel.vue'
import TagsPanel from '@/components/admin/TagsPanel.vue'
import TidyPanel from '@/components/admin/TidyPanel.vue'
import UsersPanel from '@/components/admin/UsersPanel.vue'
import { adminApi } from '@/api'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const route = useRoute()
const router = useRouter()

const sections = ['users', 'roles', 'problems', 'levels', 'security', 'notice', 'discuss', 'shop', 'ai', 'settings', 'backup'] as const
type Section = (typeof sections)[number]
// 题目管理（v0.18.1 聚合）：题库整理 / 知识树 / 标签树 / 重复题扫描 / 种子题面板同页 tab 切换（2026-10-25 双树，
// 题库整理置首且为默认落点）；tags/sources 旧深链值映射到 problems 的对应子页
type SubTab = 'tags' | 'meta' | 'duplicates' | 'seed' | 'tidy'
const rawSection = (Array.isArray(route.query.section) ? route.query.section[0] : route.query.section) as string | undefined
// 子页深链值（旧 tags/sources/meta + 重复扫描/种子题/题库整理）统一映射进题目管理分区
const isProblemSub = rawSection === 'tags' || rawSection === 'sources' || rawSection === 'meta' || rawSection === 'duplicates' || rawSection === 'seed' || rawSection === 'tidy'
const section = ref<Section>(
  rawSection && sections.includes(rawSection as Section)
    ? (rawSection as Section)
    : rawSection && isProblemSub
      ? 'problems'
      : 'users',
)
// 题目管理入口：root（双树）∨ problem.edit.all（重复扫描）
const canProblemManage = userStore.role === 'root' || userStore.can('problem.edit.all')
const showTagsTab = userStore.role === 'root'
const showDuplicatesTab = userStore.can('problem.edit.all')
// 种子题管理（清除/重部署为部署级操作）：仅 root，端点同样 RequireRoot
const showSeedTab = userStore.role === 'root'
// 题库一键整理（v0.43 批量改库）：仅 root，端点同样 RequireRoot
const showTidyTab = userStore.role === 'root'
// 默认落点（v0.92 拍板：题库整理置首即默认进）：root → 题库整理；仅 problem.edit.all 的
// 教师看不到题库整理（root 专属），落到其唯一可见的重复题扫描——不落在不可见 tab 上
const defaultSubTab: SubTab = showTidyTab ? 'tidy' : 'duplicates'
const subTab = ref<SubTab>(
  rawSection === 'tags' || rawSection === 'sources'
    ? 'tags'
    : rawSection === 'meta'
      ? 'meta'
      : isProblemSub
        ? (rawSection as SubTab)
        : defaultSubTab,
)

// ---- 分组导航（v0.35-①）----
// 四组眉题 + 名词式命名统一（编辑用户→用户管理、公告编辑→公告发布、实物兑换→兑换商店…）；
// key 与旧版一致，深链/权限口径不变
type BadgeKind = 'reports' | 'orders'
interface NavItem { key: Section; label: string; badge?: BadgeKind }
interface NavGroup { title: string; items: NavItem[] }
const NAV_GROUPS: NavGroup[] = [
  {
    title: '人员',
    items: [
      { key: 'users', label: '用户管理' },
      { key: 'roles', label: '角色与权限' },
    ],
  },
  {
    title: '内容',
    items: [
      { key: 'problems', label: '题库维护' },
      { key: 'notice', label: '公告发布' },
      { key: 'discuss', label: '讨论区管理', badge: 'reports' },
    ],
  },
  {
    title: '运营',
    items: [
      { key: 'shop', label: '兑换商店', badge: 'orders' },
      { key: 'ai', label: 'AI 助教' },
    ],
  },
  {
    title: '系统',
    items: [
      { key: 'levels', label: '等级与分档' },
      { key: 'settings', label: '系统设置' },
      { key: 'backup', label: '数据备份' },
      { key: 'security', label: '安全日志' },
    ],
  },
]

// 条目可见性（与主列各面板渲染条件同口径）；整组不可见时连组一起收掉
const visibleGroups = computed(() => {
  const show: Record<Section, boolean> = {
    users: true,
    roles: userStore.role === 'root',
    problems: canProblemManage,
    notice: true,
    discuss: userStore.can('discussion.manage'),
    shop: userStore.role === 'root',
    ai: userStore.role === 'root',
    levels: userStore.role === 'root',
    settings: true,
    backup: userStore.can('backup.run') || userStore.can('backup.keys'),
    security: userStore.role === 'root',
  }
  return NAV_GROUPS
    .map(g => ({ ...g, items: g.items.filter(i => show[i.key]) }))
    .filter(g => g.items.length > 0)
})

// 待办徽标：字段级权限——无 discussion.manage 的教师拿不到 discuss_reports、
// 非 root 拿不到 shop_orders（后端省略字段），徽标自然不渲染
const todo = ref<{ discuss_reports?: number; shop_orders?: number }>({})
async function loadTodo() {
  try {
    todo.value = await adminApi.todoCounts()
  } catch {
    // 静默失败：徽标缺席即可，不打断后台使用
  }
}
function badgeCount(kind?: BadgeKind): number {
  if (kind === 'reports') return todo.value.discuss_reports ?? 0
  if (kind === 'orders') return todo.value.shop_orders ?? 0
  return 0
}
function badgeText(n: number): string {
  return n > 99 ? '99+' : String(n)
}

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

async function switchSection(target: Section) {
  if (section.value === 'problems' && target !== 'problems') {
    if (!(await guardTagsDirty())) return
  }
  section.value = target
  // URL 同步（不触发导航守卫；仿设置页 ?tab= 深链模式）
  router.replace({ query: { ...route.query, section: target } })
  loadTodo()
}

async function switchSubTab(target: SubTab) {
  if (subTab.value === 'tags' && target !== 'tags') {
    if (!(await guardTagsDirty())) return
  }
  subTab.value = target
}

onMounted(loadTodo)
</script>

<template>
  <div class="admin-wrap">
    <div class="page-head">
      <div class="kicker">ADMIN</div>
      <h1 class="page-title">管理后台<small>{{ userStore.role === 'root' ? 'root · 全部面板可用' : '管理面板' }}</small></h1>
    </div>
    <div class="admin-layout">
    <aside class="admin-nav">
      <div v-for="g in visibleGroups" :key="g.title" class="nav-group">
        <div class="anav-eyebrow">{{ g.title }}</div>
        <button
          v-for="it in g.items"
          :key="it.key"
          type="button"
          class="nav-item"
          :class="{ active: section === it.key }"
          :aria-current="section === it.key ? 'true' : undefined"
          @click="switchSection(it.key)"
        >
          <span class="nav-title">{{ it.label }}</span>
          <span v-if="badgeCount(it.badge) > 0" class="nav-badge">{{ badgeText(badgeCount(it.badge)) }}</span>
        </button>
      </div>
    </aside>

    <main class="admin-main">
      <UsersPanel v-if="section === 'users'" />
      <RolesPanel v-else-if="section === 'roles'" />
      <div v-else-if="section === 'problems'" class="problem-manage">
        <el-tabs :model-value="subTab" @tab-change="switchSubTab as never">
          <el-tab-pane v-if="showTidyTab" name="tidy" label="题库整理" />
          <el-tab-pane v-if="showTagsTab" name="tags" label="知识树" />
          <el-tab-pane v-if="showTagsTab" name="meta" label="标签树" />
          <el-tab-pane v-if="showDuplicatesTab" name="duplicates" label="重复题扫描" />
          <el-tab-pane v-if="showSeedTab" name="seed" label="种子题管理" />
        </el-tabs>
        <TagsPanel v-if="subTab === 'tags'" @update:dirty="tagsDirty = $event" />
        <TagsPanel v-else-if="subTab === 'meta'" tree="meta" @update:dirty="tagsDirty = $event" />
        <DuplicateProblemsPanel v-else-if="subTab === 'duplicates'" />
        <SeedProblemsPanel v-else-if="subTab === 'seed'" />
        <TidyPanel v-else-if="subTab === 'tidy'" />
      </div>
      <LevelPanel v-else-if="section === 'levels'" />
      <NoticePanel v-else-if="section === 'notice'" />
      <DiscussPanel v-else-if="section === 'discuss'" />
      <ShopPanel v-else-if="section === 'shop' && userStore.role === 'root'" />
      <AiPanel v-else-if="section === 'ai' && userStore.role === 'root'" />
      <SettingsPanel v-else-if="section === 'settings'" />
      <BackupPanel v-else-if="section === 'backup'" />
      <SecurityLogsPanel v-else-if="section === 'security'" />
    </main>
    </div>
  </div>
</template>

<style scoped>
/* v3 后台布局：页头（kicker + 大标题）+ 分组导航卡 + 主列，容器 1440 对齐全站（2026-10-25 回调） */
.admin-wrap {
  max-width: 1440px;
  margin: 0 auto;
}

.admin-layout {
  display: flex;
  gap: var(--soj-sp-20);
  /* stretch：主列卡片随左列高度拉伸，两列上沿与下沿对齐；
     不用 sticky——切换分区时内容高度变化会让吸附列来回跳动 */
  align-items: stretch;
}

/* v3 导航列：整列一张白卡（圆角 16 + hairline + 柔影）；
   v0.35-① 改四组眉题 + 单行条目（原 11 项平铺双行字墙），宽度对齐 PageShell 右列 320px */
.admin-nav {
  flex: 0 0 320px;
  width: 320px;
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-4);
  padding: var(--soj-sp-10);
  background: var(--el-bg-color);
  border: 1px solid var(--soj-line);
  border-radius: 16px;
  box-shadow: var(--soj-shadow-card);
}

.nav-group {
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-4);
}

/* v3 分组眉题：--soj-fs-12 大写（中文组名只吃字距），组间距靠后续组眉题的加高 padding */
.anav-eyebrow {
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  letter-spacing: 0.16em;
  color: var(--soj-ink-3);
  text-transform: uppercase;
  padding: var(--soj-sp-4) var(--soj-sp-12) var(--soj-sp-2);
}

.nav-group + .nav-group .anav-eyebrow {
  padding-top: var(--soj-sp-12);
}

.nav-item {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-8);
  border: 1px solid transparent;
  border-radius: 10px;
  padding: var(--soj-sp-8) var(--soj-sp-12);
  cursor: pointer;
  transition: all 0.15s;
  background: none;
  font: inherit;
  width: 100%;
  text-align: left;
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

/* 键盘可达（v0.35-①）：条目由 div 改 button，焦点环走 soj-blue */
.nav-item:focus-visible {
  outline: 2px solid var(--soj-blue);
  outline-offset: 2px;
}

.nav-title {
  font-weight: var(--soj-fw-bold);
  color: var(--soj-navy);
  font-size: var(--soj-fs-14);
  white-space: nowrap;
}

/* 待办徽标：行尾红 pill（视觉同 MainLayout 铃铛角标），>99 封顶 */
.nav-badge {
  margin-left: auto;
  flex: 0 0 auto;
  min-width: 18px;
  height: 18px;
  padding: 0 var(--soj-sp-4);
  border-radius: 999px;
  background: var(--el-color-danger);
  color: var(--soj-white);
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  line-height: 18px;
  text-align: center;
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

  /* 导航改单行横滚 chips（形制同 TagChips .tag-row）：组眉题隐藏，条目顺序即隐含分组；
     替代原 11 张卡 flex-wrap 长龙 */
  .admin-nav {
    position: static;
    width: auto;
    flex: 1 1 auto;
    flex-direction: row;
    flex-wrap: nowrap;
    align-items: center;
    gap: var(--soj-sp-6);
    overflow-x: auto;
    scrollbar-width: none;
    padding: var(--soj-sp-8);
  }

  .admin-nav::-webkit-scrollbar {
    display: none;
  }

  .anav-eyebrow {
    display: none;
  }

  .nav-group {
    flex-direction: row;
    gap: var(--soj-sp-6);
  }

  /* 白卡上无边框的纯文字可点性弱，chips 形态补 hairline 描边 */
  .nav-item {
    flex: 0 0 auto;
    width: auto;
    padding: var(--soj-sp-8) var(--soj-sp-14);
    border-radius: 999px;
    border-color: var(--soj-line);
    white-space: nowrap;
  }
}
</style>

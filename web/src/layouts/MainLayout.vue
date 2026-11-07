<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { usePolling } from '@/composables/usePolling'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import AmbientGrid from '@/components/AmbientGrid.vue'
import TierBadge from '@/components/TierBadge.vue'
import { authApi, jadeApi, notifyApi } from '@/api'
import { notifyTypeMeta } from '@/components/discuss/discussMeta'
import { useLevelsStore } from '@/stores/levels'
import { useSiteStore } from '@/stores/site'
import { useUserStore } from '@/stores/user'
import FeastOverlay from '@/components/game/FeastOverlay.vue'
import { gameApi } from '@/api'
import { isMuted, setMuted, playLevelUp } from '@/utils/gameSounds'
import { pickWelcome } from '@/utils/gameText'
import { applySWUpdate, swUpdateReady } from '@/utils/swClient'
import { fmtRel } from '@/utils/format'
import SiteFooterLine from '@/components/SiteFooterLine.vue'
import type { NotificationItem } from '@/types'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const levels = useLevelsStore()
const site = useSiteStore()

const active = computed(() => route.path)

// URL 即状态（v0.39）：顶栏导航自绘 RouterLink 胶囊（原 el-menu router 模式渲染
// <li> 非 <a>——中键/Ctrl+点/右键新开全不可用）；视觉沿用 v3 胶囊样式
const navItems = computed(() => {
  const items: { to: string; label: string }[] = [
    { to: '/', label: '首页' },
    { to: '/problems', label: '题库' },
    { to: '/lists', label: '题单' },
    { to: '/contests', label: '比赛' },
    { to: '/classes', label: '班级' },
  ]
  if (site.discussGlobalEnabled) items.push({ to: '/discuss', label: '讨论' })
  // 实物兑换（v0.41 调整）：不再占导航栏，入口在「玉」页右列
  items.push({ to: '/rank', label: '排名' })
  if (userStore.can('submission.view.all')) items.push({ to: '/submissions', label: '提交记录' })
  return items
})

function navActive(to: string): boolean {
  return to === '/' ? active.value === '/' : active.value === to || active.value.startsWith(to + '/')
}
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

// 玉余额（v0.33-①）：用户菜单条目数据源；消费/获取后各页调 jadeApi.mine + setJade 自行刷新
const blessUntil = ref('') // 升维祝福（v0.40）截止时刻，空 = 无

async function refreshJade() {
  if (!userStore.token) return
  try {
    const m = await jadeApi.mine()
    userStore.setJade(m.balance, m.currency)
    blessUntil.value = m.bless_until ?? ''
  } catch {
    /* 静默 */
  }
}

const blessActive = computed(() => !!blessUntil.value && new Date(blessUntil.value) > new Date())

// ---- 首登改密提醒（批量建号/管理员建号/重置密码的账号） ----
// 标志在 user store（登录响应与 /me/profile 各刷一次）；弹窗开关是本地视图态——
// 关闭仅隐藏，服务端标志只有「跳过」清除，未改密下次登录仍提醒
const showPwdRemind = computed({
  get: () => userStore.pwdRemind,
  set: (v: boolean) => {
    userStore.pwdRemind = v
  },
})
const skippingPwdRemind = ref(false)
async function skipPwdRemind() {
  skippingPwdRemind.value = true
  try {
    await authApi.skipPwdRemind()
    userStore.pwdRemind = false
    ElMessage.success('已跳过，下次不再提醒')
  } catch {
    ElMessage.error('操作失败，请重试')
  } finally {
    skippingPwdRemind.value = false
  }
}
function goChangePassword() {
  userStore.pwdRemind = false // 只关弹窗：未真改密下次登录仍提醒（改密成功走服务端清除）
  router.push('/settings')
}

// ---- 游戏感批（v0.41）----
const soundOff = ref(isMuted())
function toggleMute() {
  setMuted(!soundOff.value)
  soundOff.value = isMuted()
  if (!soundOff.value) playLevelUp() // 开声试音
}
// 降临横幅：60s 轮询最近 Lv≥7 升级，未播过的弹一次（localStorage 记最近一条）
// v0.65 轮询归一：usePolling（后台标签暂停、作用域卸载自清——原裸 setInterval 后台空转）
const heralds = ref<{ uid: number; level: number; nickname: string; at: string }[]>([])
async function pollHeralds() {
  if (!userStore.token) return
  try {
    const r = await gameApi.heralds()
    const last = Number(localStorage.getItem('soj_herald_last') ?? 0)
    const fresh = r.list.filter(
      (x) => x.uid * 100000 + x.level > last && Date.now() - new Date(x.at).getTime() < 3600_000,
    )
    if (fresh.length) {
      heralds.value = fresh
      localStorage.setItem('soj_herald_last', String(Math.max(...fresh.map((x) => x.uid * 100000 + x.level))))
      if (fresh.some((x) => x.level >= 10)) playLevelUp()
      setTimeout(() => (heralds.value = []), 8000)
    }
  } catch {
    /* 静默 */
  }
}
// 登录欢迎语：登录态首刷后弹一次（sessionStorage 会话内一次）
function welcomeOnce() {
  if (!userStore.token || sessionStorage.getItem('soj_welcomed')) return
  sessionStorage.setItem('soj_welcomed', '1')
  const lv = userStore.level?.level ?? 0
  ElMessage({ message: pickWelcome(lv), type: 'success', duration: 3200 })
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

const badgePoll = usePolling(() => refreshBadge(), 60_000)
const heraldPoll = usePolling(() => pollHeralds(), 60_000) // 降临横幅轮询（v0.65 归一）

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

// 头像首字（v3 .avatar：渐变圆 + 昵称首字；昵称为空兜底「我」）
const avatarChar = computed(() => {
  const n = userStore.nickname || '我'
  return n.slice(0, 1).toUpperCase()
})
// QQ 头像外域失败兜底（v0.37）：校园网墙掉 qlogo 时顶栏裂图——@error 回退首字头像
//（内存态标记，不落库；换新头像 URL 由 profile 刷新重置）
const avatarBroken = ref(false)
watch(
  () => userStore.avatarUrl,
  () => {
    avatarBroken.value = false
  },
)
function onAvatarError() {
  avatarBroken.value = true
}

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
      userStore.pwdRemind = !!p.pwdRemind // 首登改密提醒（刷新后由此恢复）
    })
    .catch(() => {})
  refreshBadge() // 通知角标首刷（v0.24）
  badgePoll.start() // 60s 轮询（usePolling：后台标签暂停、卸载自清）
  refreshJade() // 玉余额首刷（v0.33-①）
  pollHeralds()
  heraldPoll.start()
  welcomeOnce()
})

function onCommand(command: string) {
  if (command === 'me') {
    router.push(`/users/${userStore.uid}`) // 公开主页（v0.17）
  } else if (command === 'jade') {
    router.push('/jade') // 余额与流水独立页（2026-10-30）
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
    <!-- 降临横幅（v0.41）：Lv≥7 升级全站播报 -->
    <Transition name="herald-slide">
      <div v-if="heralds.length" class="herald-banner" :class="{ gold: heralds.some((x) => x.level >= 10) }">
        <span v-for="x in heralds" :key="x.uid" class="herald-line">
          ⚡ Lv.{{ x.level }} 大佬 <b>{{ x.nickname }}</b> 降临
        </span>
      </div>
    </Transition>
    <FeastOverlay />
    <!-- 换版横幅（v0.46 SW 预缓存）：新版本已后台预热就绪，用户确认才切换（skipWaiting
         确认制）；× 可暂缓（本次会话内不再打扰），下次探测到 waiting 仍会提示 -->
    <Transition name="herald-slide">
      <div v-if="swUpdateReady" class="sw-update-banner" role="status">
        <span>新版本已就绪</span>
        <button type="button" class="sw-refresh-btn" @click="applySWUpdate">点此刷新</button>
        <button type="button" class="sw-dismiss" aria-label="暂不刷新" @click="swUpdateReady = false">×</button>
      </div>
    </Transition>
    <el-header class="header">
      <div class="header-inner">
        <!-- 品牌：渐变闪电标志（与 /favicon.svg 同源）+ 流彩渐变字 -->
        <div class="brand" @click="router.push('/')">
          <img class="brand-mark" src="/favicon.svg" alt="SOJ" /><span class="brand-word">SOJ</span>
        </div>
        <button
          v-if="userStore.token"
          class="mute-btn"
          :title="soundOff ? '开启音效' : '静音'"
          @click="toggleMute"
        >
          {{ soundOff ? '🔇' : '🔊' }}
        </button>
        <nav class="menu nav-links" aria-label="主导航">
          <RouterLink
            v-for="item in navItems"
            :key="item.to"
            :to="item.to"
            class="nav-a"
            :class="{ on: navActive(item.to) }"
            :aria-current="navActive(item.to) ? 'page' : undefined"
          >
            {{ item.label }}
          </RouterLink>
        </nav>
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
          <!-- 玉余额挪入用户菜单（2026-10-30 拍板）：顶栏 chip 退役，余额在悬停菜单「我的主页」上方，
               点击进独立流水页 /jade；货币名后台可配 -->
          <el-dropdown class="user-dropdown" @command="onCommand">
            <span class="user-chip">
              <img v-if="userStore.avatarUrl && !avatarBroken" :src="userStore.avatarUrl" alt="" class="avatar avatar-img" decoding="async" @error="onAvatarError" />
              <span v-else class="avatar">{{ avatarChar }}</span>
              <span class="uname">{{ userStore.nickname || '已登录' }}</span>
              <TierBadge
                v-if="userStore.level?.level"
                :tier="userStore.level.level"
                kind="level"
                size="sm"
              />
            </span>            <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="jade" class="jade-item">
                <span class="jade-glyph" aria-hidden="true">◈</span>
                <b class="jade-num">{{ userStore.jadeBalance }}</b>
                <span class="jade-unit">{{ userStore.jadeCurrency }}</span>
                <span v-if="blessActive" class="bless-chip" title="升维祝福：做题玉收入 ×2（48 小时）">⚡×2</span>
              </el-dropdown-item>
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
      <!-- 页脚工具行（v0.60 改名）：独立工具页入口，置于页脚信息行上方一行 -->
      <div class="footer-tools">
        <router-link to="/draw" class="ftool">画板</router-link>
        <router-link to="/diff" class="ftool">代码对比</router-link>
      </div>
      <div class="footer-line">
        <!-- 页脚信息行（2026-11-02 单行化 + 主题 popover 向上选择）：SiteFooterLine 共用组件，
             登录页页脚同款 -->
        <SiteFooterLine />
      </div>
    </el-footer>
    <!-- 首登改密提醒（批量建号/管理员建号/重置密码的账号）：登录后与刷新后各弹一次。
         仅「跳过」清除服务端标志（不再提醒）；「去修改密码」只关弹窗不落库——未真改密
         下次登录仍提醒。X/ESC/点遮罩关闭同理不清标志。 -->
    <el-dialog
      v-model="showPwdRemind"
      title="修改密码提醒"
      width="420"
      align-center
    >
      <p style="margin: 0 0 8px">你的账号正在使用初始密码，建议尽快修改，以保证账号安全。</p>
      <p style="margin: 0; color: var(--el-text-color-secondary); font-size: var(--soj-fs-13)">
        修改密码后本次提醒自动解除；你也可以选择跳过。
      </p>
      <template #footer>
        <el-button @click="skipPwdRemind" :loading="skippingPwdRemind">跳过</el-button>
        <el-button type="primary" @click="goChangePassword">去修改密码</el-button>
      </template>
    </el-dialog>
  </el-container>
</template>

<style scoped>
.layout {
  min-height: 100vh;
}

/* v3-full 玻璃顶栏：半透明白 + backdrop blur，sticky 悬浮于画布之上（底边 --soj-line） */
.header {
  position: sticky;
  top: 0;
  z-index: 50;
  background: color-mix(in srgb, var(--soj-bg-top) 72%, transparent);
  /* v0.65 降档 16px→12px：sticky 顶栏滚动时对背后内容逐帧重采样模糊，12px 视觉
     仍充分、重采样开销随半径线性下降；背后 fixed 渐变层已随本批改走合成 */
  -webkit-backdrop-filter: blur(12px) saturate(1.5);
  backdrop-filter: blur(12px) saturate(1.5);
  border-bottom: 1px solid var(--soj-line);
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
  padding: 0 var(--soj-sp-24);
  gap: var(--soj-sp-6);
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-10);
  cursor: pointer;
  margin-right: var(--soj-sp-16);
  white-space: nowrap;
}

/* 品牌标志：渐变闪电 favicon 同源（透明底），34px */
.brand-mark {
  width: 34px;
  height: 34px;
  border-radius: 10px;
}

.brand-word {
  font-size: var(--soj-fs-brand);
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
    var(--soj-brand-a) 0,
    var(--soj-brand-b) 17.3205px,
    var(--soj-brand-c) 34.641px,
    var(--soj-brand-a) 51.9615px
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

/* v3-full 菜单胶囊（.nav-links a：--soj-sp-8/--soj-sp-16、--soj-fs-body 字号，hover/on 浅蓝底）：项高自适应居中 */
.nav-links {
  display: flex;
  align-items: center;
}

/* v3-full 菜单胶囊（v0.39 链接化：el-menu → RouterLink，参数同源 --soj-sp-8/--soj-sp-16 / 999 / soft 底） */
.nav-links a {
  align-self: center;
  height: auto;
  line-height: 1.2;
  padding: var(--soj-sp-8) var(--soj-sp-16);
  border-radius: 999px;
  margin: 0 var(--soj-sp-2);
  border-bottom: none;
  font-size: var(--soj-fs-body);
  color: inherit;
  text-decoration: none;
  transition: background-color 0.18s, color 0.18s;
}

.nav-links a:hover,
.nav-links a:focus-visible {
  background-color: var(--soj-blue-soft);
}

.nav-links a.on {
  background-color: var(--soj-blue-soft);
  color: var(--soj-blue);
  font-weight: var(--soj-fw-bold);
}

/* v0.78 右簇轻量化（用户反馈顶栏右侧太挤，参考左侧导航重设计）：描边全部退役——
   左侧导航是「无描边胶囊 + hover 浅蓝底」的轻语言，右侧三件（管理后台/铃铛/用户
   chip）旧为描边件挤在 header gap 6px 里，三条边框视觉连成一团；改无描边胶囊与
   左侧同语言，簇内间距 margin 补至 10px */
.admin-link {
  flex: 0 0 auto;
  margin-left: var(--soj-sp-4);
  font-size: var(--soj-fs-body);
  font-weight: var(--soj-fw-emphasis);
  color: var(--soj-ink-2);
  padding: var(--soj-sp-8) var(--soj-sp-16);
  border-radius: 999px;
  text-decoration: none;
  transition: all 0.18s;
  white-space: nowrap;
}

.admin-link:hover,
.admin-link.on {
  color: var(--soj-blue);
  background: var(--soj-blue-soft);
}

/* v0.24 通知铃铛：圆钮 + 未读角标；hover 下拉面板列表。v0.78 去描边对齐右簇轻语言，
   径 34→38 与胶囊簇同高（基线齐平） */
.bell {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  margin-left: var(--soj-sp-4);
  border-radius: 50%;
  border: none;
  background: transparent;
  color: var(--soj-ink-2);
  cursor: pointer;
  transition: all 0.18s;
  flex: 0 0 auto;
}

.bell:hover,
.bell.ringing {
  color: var(--soj-blue);
  background: var(--soj-blue-soft);
}

.bell-badge {
  position: absolute;
  top: -5px;
  right: -6px;
  min-width: 17px;
  height: 17px;
  padding: 0 var(--soj-sp-4);
  border-radius: 999px;
  background: var(--el-color-danger);
  color: var(--soj-white);
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
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
  padding: var(--soj-sp-2) var(--soj-sp-6) var(--soj-sp-8);
  border-bottom: 1px solid var(--soj-line);
}

.bh-title {
  font-weight: var(--soj-fw-emphasis);
  font-size: var(--soj-fs-14);
}

.bell-item {
  display: flex;
  align-items: flex-start;
  gap: var(--soj-sp-8);
  padding: var(--soj-sp-8) var(--soj-sp-6);
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
  font-size: var(--soj-fs-body);
  line-height: 1.5;
}

.bi-body {
  flex: 1;
  min-width: 0;
}

.bi-title {
  font-size: var(--soj-fs-13);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bi-text {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bi-time {
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  white-space: nowrap;
  flex-shrink: 0;
}

.bell-foot {
  border-top: 1px solid var(--soj-line);
  padding-top: var(--soj-sp-6);
  text-align: center;
}

/* v3-full .user-chip：白底描边胶囊（渐变圆头像 30px + 昵称 700 + 等级徽章同装一体） */
/* 玉余额 chip（v0.33-①）：与 user-chip 同族描边胶囊，玉青色数字强调 */
/* 用户菜单玉余额条目（2026-10-30）：玉青数字强调，右端浅色提示；菜单 teleport 渲染，
   但元素带本组件 scope 属性，scoped 规则可达 */
.jade-item {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-6);
  min-width: 200px;
}

.jade-item .jade-glyph {
  color: var(--soj-teal-deep);
  font-size: var(--soj-fs-14);
  line-height: 1;
}

.jade-item .jade-num {
  color: var(--soj-teal-deep);
  font-weight: var(--soj-fw-bold);
  font-variant-numeric: tabular-nums;
}

.jade-item .jade-unit {
  color: var(--soj-text-2, var(--el-text-color-secondary));
  font-size: var(--soj-fs-13);
}

.jade-item .jade-hint {
  margin-left: auto;
  padding-left: var(--soj-sp-16);
  color: var(--soj-ink-3);
  font-size: var(--soj-fs-12);
}

.user-dropdown {
  height: 100%;
  display: inline-flex;
  align-items: center;
  outline: none;
}

.user-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--soj-sp-10);
  /* v0.78 右簇轻量化：描边+阴影退役（描边三连挤是「太挤」根因），hover 浅蓝底对齐
     左侧导航；右内边距 16 对齐 nav 横向留白，昵称与等级徽章呼吸开 */
  margin-left: var(--soj-sp-4);
  padding: var(--soj-sp-4) var(--soj-sp-16) var(--soj-sp-4) var(--soj-sp-4);
  border-radius: 999px;
  background: transparent;
  cursor: pointer;
  transition: background-color 0.18s;
  outline: none;
  white-space: nowrap;
}

.user-chip:hover {
  background: var(--soj-blue-soft);
}

.avatar {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, var(--soj-blue-hi), var(--soj-teal));
  color: var(--soj-white);
  font-weight: var(--soj-fw-bold);
  font-size: var(--soj-fs-14);
}

/* QQ 头像版顶栏头像（v0.23）：同径圆 + 细描边 */
.avatar-img {
  object-fit: cover;
  border: 1px solid var(--soj-line);
  background: var(--soj-white);
}

.uname {
  /* v0.78 对齐左侧导航字号（15px）：14px 粗体贴等级徽章是「挤」的第二根因 */
  font-weight: var(--soj-fw-bold);
  font-size: var(--soj-fs-body);
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
  flex-direction: column;
  align-items: center;
  /* 工具行与信息行的行距（2026-11-02 用户反馈增大）：8 → 14 */
  gap: var(--soj-sp-14);
  /* el-footer 默认固定高 60px 会裁掉双行内容，双行页脚需放开高度自撑 */
  height: auto;
  padding: var(--soj-sp-12) var(--soj-sp-20) var(--soj-sp-14);
  font-size: var(--soj-fs-12);
  color: var(--soj-ink-3);
  background: transparent;
  /* v3 页脚：hairline 顶边收束页面 */
  border-top: 1px solid var(--soj-line);
}

/* 原页脚信息行（版本/说明/主题/备案）：排布口径不变 */
.footer-line {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--soj-sp-16);
  flex-wrap: wrap;
}

/* 工具入口行：弱化灰、hover 提主蓝，与 .flink 同口径 */
.footer-tools {
  display: flex;
  align-items: center;
  gap: var(--soj-sp-14);
}

.ftool {
  /* 工具行字号（2026-11-02 用户反馈调大）：随页脚 12px → 14px 界面基线 */
  font-size: var(--soj-fs-14);
  color: var(--soj-ink-3);
  text-decoration: none;
}

.ftool:hover {
  color: var(--soj-blue);
}
</style>

<style scoped>
/* 升维祝福徽章（v0.40）：玉余额旁的小闪电 */
.bless-chip {
  margin-left: var(--soj-sp-6);
  padding: 0 var(--soj-sp-6);
  border-radius: 999px;
  font-size: var(--soj-fs-12);
  font-weight: var(--soj-fw-bold);
  color: var(--soj-warning-deep);
  /* 金渐变高光停 = warning 向白提亮（v0.57 收编：原野生 #fbbf24，随主题换血） */
  background: linear-gradient(135deg, var(--soj-tle-bg), color-mix(in srgb, var(--soj-warning) 70%, var(--soj-white)));
  animation: blessPulse 1.6s ease-in-out infinite;
}

@keyframes blessPulse {
  0%,
  100% {
    transform: scale(1);
  }
  50% {
    transform: scale(1.12);
  }
}

@media (prefers-reduced-motion: reduce) {
  .bless-chip {
    animation: none;
  }
}
</style>

<style scoped>
.mute-btn {
  border: none;
  background: transparent;
  cursor: pointer;
  font-size: var(--soj-fs-16);
  padding: var(--soj-sp-4) var(--soj-sp-6);
  border-radius: 8px;
  line-height: 1;
}

.mute-btn:hover {
  background: var(--soj-blue-soft);
}

/* 降临横幅（v0.41）：顶部滑入，Lv10 金色 */
.herald-banner {
  position: fixed;
  top: 12px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 2600;
  display: flex;
  flex-direction: column;
  gap: var(--soj-sp-4);
  padding: var(--soj-sp-8) var(--soj-sp-20);
  border-radius: 999px;
  background: color-mix(in srgb, var(--soj-blue) 92%, transparent);
  color: var(--soj-white);
  font-size: var(--soj-fs-14);
  box-shadow: 0 8px 28px color-mix(in srgb, var(--soj-blue) 40%, transparent);
}

.herald-banner.gold {
  background: linear-gradient(135deg, var(--soj-warning-deep), var(--soj-warning));
}

.herald-line b {
  margin: 0 var(--soj-sp-2);
}

.herald-slide-enter-active,
.herald-slide-leave-active {
  transition: all 0.5s ease;
}

.herald-slide-enter-from,
.herald-slide-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(-24px);
}

@media (prefers-reduced-motion: reduce) {
  .herald-slide-enter-active,
  .herald-slide-leave-active {
    transition: opacity 0.3s;
  }
}

/* 换版横幅（v0.46）：与降临横幅同一套顶部胶囊视觉，蓝底白字、错开降临横幅的 12px 层位 */
.sw-update-banner {
  position: fixed;
  top: 60px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 2600;
  display: flex;
  align-items: center;
  gap: var(--soj-sp-12);
  padding: var(--soj-sp-6) var(--soj-sp-8) var(--soj-sp-6) var(--soj-sp-16);
  border-radius: 999px;
  background: color-mix(in srgb, var(--soj-blue) 92%, transparent);
  color: var(--soj-white);
  font-size: var(--soj-fs-14);
  box-shadow: 0 8px 28px color-mix(in srgb, var(--soj-blue) 40%, transparent);
}

.sw-refresh-btn {
  border: none;
  border-radius: 999px;
  padding: var(--soj-sp-4) var(--soj-sp-14);
  background: var(--soj-white);
  color: var(--soj-blue);
  font-size: var(--soj-fs-13);
  font-weight: var(--soj-fw-emphasis);
  cursor: pointer;
}

.sw-refresh-btn:hover {
  background: var(--soj-blue-soft);
}

.sw-dismiss {
  border: none;
  background: transparent;
  color: color-mix(in srgb, var(--soj-white) 75%, transparent);
  font-size: var(--soj-fs-16);
  line-height: 1;
  padding: var(--soj-sp-2) var(--soj-sp-8);
  cursor: pointer;
}

.sw-dismiss:hover {
  color: var(--soj-white);
}
</style>

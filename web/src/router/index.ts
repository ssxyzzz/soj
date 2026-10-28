import { createRouter, createWebHistory } from 'vue-router'

import MainLayout from '@/layouts/MainLayout.vue'
import { useUserStore } from '@/stores/user'

// 视图全部懒加载（v0.28 打包批）：主包只留布局/框架/公共依赖，重页面（管理后台/
// 导入/挑战/编辑器族/试卷）按路由按需拉取；MainLayout 每路由必用，保持静态引入。

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    requiresAuth?: boolean
    requiresAdmin?: boolean
    requiresPerm?: string
    staffOnly?: boolean
  }
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: MainLayout,
      children: [
        { path: '', name: 'home', component: () => import('@/views/HomeView.vue'), meta: { title: '首页' } },
        {
          path: 'problems',
          name: 'problems',
          component: () => import('@/views/ProblemListView.vue'),
          meta: { title: '题库' },
        },
        {
          path: 'discuss',
          name: 'discuss',
          component: () => import('@/views/DiscussView.vue'),
          meta: { title: '讨论', requiresAuth: true },
        },
        {
          path: 'discuss/:tid',
          name: 'discuss-topic',
          component: () => import('@/views/DiscussTopicView.vue'),
          meta: { title: '讨论', requiresAuth: true },
        },
        {
          path: 'notifications',
          name: 'notifications',
          component: () => import('@/views/NotificationsView.vue'),
          meta: { title: '消息中心', requiresAuth: true },
        },
        {
          path: 'lists',
          name: 'lists',
          component: () => import('@/views/ListsView.vue'),
          meta: { title: '题单', requiresAuth: true },
        },
        {
          path: 'lists/:id/edit',
          name: 'list-edit',
          component: () => import('@/views/ListEditView.vue'),
          meta: { title: '编辑题单', requiresAuth: true },
        },
        {
          path: 'lists/:id',
          name: 'list-detail',
          component: () => import('@/views/ListDetailView.vue'),
          meta: { title: '题单', requiresAuth: true },
        },
        {
          path: 'problems/create',
          name: 'problem-create',
          component: () => import('@/views/ProblemCreateView.vue'),
          meta: { title: '新建题目', requiresPerm: 'problem.create' },
        },
        {
          path: 'problems/:id/edit',
          name: 'problem-edit',
          // 编辑权限是题目级（can_edit_statement），由编辑页自查；静态权限位守卫不适用
          component: () => import('@/views/ProblemEditView.vue'),
          meta: { title: '编辑题目', requiresAuth: true },
        },
        {
          path: 'problems/:id',
          name: 'problem-detail',
          component: () => import('@/views/ProblemDetailView.vue'),
          meta: { title: '题目' },
        },
        {
          path: 'classes',
          name: 'classes',
          component: () => import('@/views/ClassesView.vue'),
          meta: { title: '班级' },
        },
        {
          path: 'classes/:id',
          name: 'class-detail',
          component: () => import('@/views/ClassDetailView.vue'),
          meta: { title: '班级' },
        },
        {
          path: 'contests',
          name: 'contests',
          component: () => import('@/views/ContestsView.vue'),
          meta: { title: '比赛' },
        },
        {
          path: 'contests/:id',
          name: 'contest-detail',
          component: () => import('@/views/ContestDetailView.vue'),
          meta: { title: '比赛' },
        },
        {
          path: 'contests/:id/problems/:pid',
          name: 'contest-problem',
          component: () => import('@/views/ContestProblemView.vue'),
          meta: { title: '赛题' },
        },
        {
          path: 'assignments/:id',
          name: 'assignment-detail',
          component: () => import('@/views/AssignmentView.vue'),
          meta: { title: '作业' },
        },
        {
          path: 'exams/:id',
          name: 'exam-detail',
          component: () => import('@/views/ExamDetailView.vue'),
          meta: { title: '考试' },
        },
        {
          path: 'exams/:id/problems/:pid',
          name: 'exam-problem',
          component: () => import('@/views/ExamProblemView.vue'),
          meta: { title: '考题' },
        },
        {
          path: 'submissions',
          name: 'submissions',
          component: () => import('@/views/SubmissionListView.vue'),
          meta: { title: '提交记录', staffOnly: true },
        },
        {
          path: 'rank',
          name: 'rank',
          component: () => import('@/views/RankView.vue'),
          meta: { title: '排名' },
        },
        {
          path: 'users/:id',
          name: 'user-profile',
          // 公开主页（v0.17）：登录后可见，未登录由守卫带 redirect 弹登录
          component: () => import('@/views/UserProfileView.vue'),
          meta: { title: '用户主页', requiresAuth: true },
        },
        {
          path: 'settings',
          name: 'settings',
          component: () => import('@/views/SettingsView.vue'),
          meta: { title: '个人设置', requiresAuth: true },
        },
        {
          path: 'admin',
          name: 'admin',
          component: () => import('@/views/AdminView.vue'),
          meta: { title: '管理', requiresAdmin: true },
        },
        {
          path: 'import',
          name: 'import',
          component: () => import('@/views/ImportView.vue'),
          meta: { title: '导入题目', requiresPerm: 'problem.create' },
        },
      ],
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { title: '登录' },
    },
    {
      path: '/contests/:id/paper',
      name: 'contest-paper',
      component: () => import('@/views/PaperView.vue'),
      props: { kind: 'contest' },
      meta: { title: '打印试卷', requiresAuth: true },
    },
    {
      path: '/exams/:id/paper',
      name: 'exam-paper',
      component: () => import('@/views/PaperView.vue'),
      props: { kind: 'exam' },
      meta: { title: '打印试卷', requiresAuth: true },
    },
    {
      path: '/problems/:id/paper',
      name: 'problem-paper',
      component: () => import('@/views/PaperView.vue'),
      props: { kind: 'problem' },
      meta: { title: '导出试卷', requiresAuth: true },
    },
    {
      path: '/problems/:id/challenge',
      name: 'problem-challenge',
      // 挑战全屏页（v0.23）：脱离 MainLayout 独立画布，倒计时不吃导航栏
      component: () => import('@/views/ChallengeView.vue'),
      meta: { title: '挑战模式', requiresAuth: true },
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('@/views/NotFoundView.vue'),
      meta: { title: '页面不存在' },
    },
  ],
})

router.beforeEach((to) => {
  // 与请求身份同一来源（pinia store，每标签页一份内存态）：
  // 直接读共享 localStorage 会拿到其他标签页登录的账号，守卫与请求判权不一致
  const userStore = useUserStore()
  // 游客可见面（v0.7.3）：仅首页/题库/题目详情，其余一律引到登录页（服务端接口同步收紧）
  if (!userStore.token) {
    const guestOpen = to.name === 'home' || to.name === 'problems' || to.name === 'problem-detail'
    if (!guestOpen && to.name !== 'login' && to.name !== 'not-found') {
      return { path: '/login', query: { redirect: to.fullPath } }
    }
  }
  if (to.meta.requiresAdmin && !userStore.canAdmin) {
    return { path: '/' }
  }
  // 通用权限位守卫：requiresPerm 为权限位 id（如 problem.create）
  if (to.meta.requiresPerm && !userStore.can(to.meta.requiresPerm as string)) {
    return { path: '/' }
  }
  if (to.meta.staffOnly && !userStore.can('submission.view.all')) {
    return { path: '/' }
  }
  if (to.meta.requiresAuth && !userStore.token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  document.title = to.meta.title ? `${to.meta.title} - SOJ` : 'SOJ'
})

export default router

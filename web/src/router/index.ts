import { createRouter, createWebHistory } from 'vue-router'

import MainLayout from '@/layouts/MainLayout.vue'
import { useUserStore } from '@/stores/user'
import AdminView from '@/views/AdminView.vue'
import ImportView from '@/views/ImportView.vue'
import ClassDetailView from '@/views/ClassDetailView.vue'
import ClassesView from '@/views/ClassesView.vue'
import ContestDetailView from '@/views/ContestDetailView.vue'
import ContestProblemView from '@/views/ContestProblemView.vue'
import ContestsView from '@/views/ContestsView.vue'
import ExamDetailView from '@/views/ExamDetailView.vue'
import ExamProblemView from '@/views/ExamProblemView.vue'
import HomeView from '@/views/HomeView.vue'
import AssignmentView from '@/views/AssignmentView.vue'
import LoginView from '@/views/LoginView.vue'
import NotFoundView from '@/views/NotFoundView.vue'
import PaperView from '@/views/PaperView.vue'
import ProblemDetailView from '@/views/ProblemDetailView.vue'
import ProblemCreateView from '@/views/ProblemCreateView.vue'
import ProblemEditView from '@/views/ProblemEditView.vue'
import ProblemListView from '@/views/ProblemListView.vue'
import RankView from '@/views/RankView.vue'
import SettingsView from '@/views/SettingsView.vue'
import SubmissionListView from '@/views/SubmissionListView.vue'
import UserProfileView from '@/views/UserProfileView.vue'
import ListsView from '@/views/ListsView.vue'
import ListDetailView from '@/views/ListDetailView.vue'
import ListEditView from '@/views/ListEditView.vue'
import ChallengeView from '@/views/ChallengeView.vue'

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
        { path: '', name: 'home', component: HomeView, meta: { title: '首页' } },
        { path: 'problems', name: 'problems', component: ProblemListView, meta: { title: '题库' } },
        {
          path: 'lists',
          name: 'lists',
          component: ListsView,
          meta: { title: '题单', requiresAuth: true },
        },
        {
          path: 'lists/:id/edit',
          name: 'list-edit',
          component: ListEditView,
          meta: { title: '编辑题单', requiresAuth: true },
        },
        {
          path: 'lists/:id',
          name: 'list-detail',
          component: ListDetailView,
          meta: { title: '题单', requiresAuth: true },
        },
        {
          path: 'problems/create',
          name: 'problem-create',
          component: ProblemCreateView,
          meta: { title: '新建题目', requiresPerm: 'problem.create' },
        },
        {
          path: 'problems/:id/edit',
          name: 'problem-edit',
          component: ProblemEditView,
          // 编辑权限是题目级（can_edit_statement），由编辑页自查；静态权限位守卫不适用
          meta: { title: '编辑题目', requiresAuth: true },
        },
        {
          path: 'problems/:id',
          name: 'problem-detail',
          component: ProblemDetailView,
          meta: { title: '题目' },
        },
        {
          path: 'classes',
          name: 'classes',
          component: ClassesView,
          meta: { title: '班级' },
        },
        {
          path: 'classes/:id',
          name: 'class-detail',
          component: ClassDetailView,
          meta: { title: '班级' },
        },
        {
          path: 'contests',
          name: 'contests',
          component: ContestsView,
          meta: { title: '比赛' },
        },
        {
          path: 'contests/:id',
          name: 'contest-detail',
          component: ContestDetailView,
          meta: { title: '比赛' },
        },
        {
          path: 'contests/:id/problems/:pid',
          name: 'contest-problem',
          component: ContestProblemView,
          meta: { title: '赛题' },
        },
        {
          path: 'assignments/:id',
          name: 'assignment-detail',
          component: AssignmentView,
          meta: { title: '作业' },
        },
        {
          path: 'exams/:id',
          name: 'exam-detail',
          component: ExamDetailView,
          meta: { title: '考试' },
        },
        {
          path: 'exams/:id/problems/:pid',
          name: 'exam-problem',
          component: ExamProblemView,
          meta: { title: '考题' },
        },
        {
          path: 'submissions',
          name: 'submissions',
          component: SubmissionListView,
          meta: { title: '提交记录', staffOnly: true },
        },
        { path: 'rank', name: 'rank', component: RankView, meta: { title: '排名' } },
        {
          path: 'users/:id',
          name: 'user-profile',
          component: UserProfileView,
          // 公开主页（v0.17）：登录后可见，未登录由守卫带 redirect 弹登录
          meta: { title: '用户主页', requiresAuth: true },
        },
        {
          path: 'settings',
          name: 'settings',
          component: SettingsView,
          meta: { title: '个人设置', requiresAuth: true },
        },
        {
          path: 'admin',
          name: 'admin',
          component: AdminView,
          meta: { title: '管理', requiresAdmin: true },
        },
        {
          path: 'import',
          name: 'import',
          component: ImportView,
          meta: { title: '导入题目', requiresPerm: 'problem.create' },
        },
      ],
    },
    { path: '/login', name: 'login', component: LoginView, meta: { title: '登录' } },
    {
      path: '/contests/:id/paper',
      name: 'contest-paper',
      component: PaperView,
      props: { kind: 'contest' },
      meta: { title: '打印试卷', requiresAuth: true },
    },
    {
      path: '/exams/:id/paper',
      name: 'exam-paper',
      component: PaperView,
      props: { kind: 'exam' },
      meta: { title: '打印试卷', requiresAuth: true },
    },
    {
      path: '/problems/:id/paper',
      name: 'problem-paper',
      component: PaperView,
      props: { kind: 'problem' },
      meta: { title: '导出试卷', requiresAuth: true },
    },
    {
      path: '/problems/:id/challenge',
      name: 'problem-challenge',
      component: ChallengeView,
      // 挑战全屏页（v0.23）：脱离 MainLayout 独立画布，倒计时不吃导航栏
      meta: { title: '挑战模式', requiresAuth: true },
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: NotFoundView,
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

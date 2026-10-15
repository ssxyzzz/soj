import { createRouter, createWebHistory } from 'vue-router'

import MainLayout from '@/layouts/MainLayout.vue'
import AdminView from '@/views/AdminView.vue'
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
import ProblemDetailView from '@/views/ProblemDetailView.vue'
import ProblemListView from '@/views/ProblemListView.vue'
import RankView from '@/views/RankView.vue'
import SubmissionListView from '@/views/SubmissionListView.vue'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    requiresAuth?: boolean
    requiresAdmin?: boolean
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
          path: 'admin',
          name: 'admin',
          component: AdminView,
          meta: { title: '管理', requiresAdmin: true },
        },
      ],
    },
    { path: '/login', name: 'login', component: LoginView, meta: { title: '登录' } },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: NotFoundView,
      meta: { title: '页面不存在' },
    },
  ],
})

router.beforeEach((to) => {
  const role = localStorage.getItem('soj_role')
  if (to.meta.requiresAdmin && role !== 'root') {
    return { path: '/' }
  }
  if (to.meta.staffOnly && role !== 'admin' && role !== 'root') {
    return { path: '/' }
  }
  if (to.meta.requiresAuth && !localStorage.getItem('soj_token')) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  document.title = to.meta.title ? `${to.meta.title} - SOJ` : 'SOJ'
})

export default router

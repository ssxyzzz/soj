import { createRouter, createWebHistory } from 'vue-router'

import MainLayout from '@/layouts/MainLayout.vue'
import HomeView from '@/views/HomeView.vue'
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
          path: 'submissions',
          name: 'submissions',
          component: SubmissionListView,
          meta: { title: '提交记录' },
        },
        { path: 'rank', name: 'rank', component: RankView, meta: { title: '排行榜' } },
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
  if (to.meta.requiresAuth && !localStorage.getItem('soj_token')) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  document.title = to.meta.title ? `${to.meta.title} - SOJ` : 'SOJ'
})

export default router

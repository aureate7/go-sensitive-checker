// src/router/index.js
import { createRouter, createWebHistory } from 'vue-router'
import Home from '@/pages/Home.vue'

const routes = [
  {
    path: '/',
    name: 'Home',
    component: Home,
  },
  {
    path: '/admin',
    name: 'Admin',
    component: () => import('@/pages/Admin.vue'),
  },
  {
    path: '/tasks',
    name: 'BatchTasks',
    component: () => import('@/pages/Tasks.vue'),
  },
  {
    path: '/evaluations',
    name: 'Evaluations',
    component: () => import('@/pages/Evaluations.vue'),
  },
  { path:'/reviews', name:'Reviews', component:()=>import('@/pages/Reviews.vue') },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router

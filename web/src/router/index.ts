import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'dashboard',
      component: () => import('../views/Dashboard.vue')
    },
    {
      path: '/sources',
      name: 'sources',
      component: () => import('../views/Sources.vue')
    },
    {
      path: '/cpa',
      name: 'cpa',
      component: () => import('../views/Cpa.vue')
    },
    {
      path: '/api-keys',
      name: 'api-keys',
      component: () => import('../views/ApiKeys.vue')
    },
    {
      path: '/logs',
      name: 'logs',
      component: () => import('../views/Logs.vue')
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('../views/Settings.vue')
    }
  ]
})

export default router

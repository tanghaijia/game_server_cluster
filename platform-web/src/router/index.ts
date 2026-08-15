import { createRouter, createWebHistory } from 'vue-router'

import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
    },
    {
      path: '/',
      component: () => import('@/layouts/DefaultLayout.vue'),
      meta: { requiresAuth: true },
      children: [
        { path: '', name: 'launcher', component: () => import('@/views/GameLauncherView.vue') },
        { path: 'games/:gameId/servers', name: 'game-servers', component: () => import('@/views/MyServersView.vue') },
        { path: 'games/:gameId/orders', name: 'game-orders', component: () => import('@/views/MyOrdersView.vue') },
        { path: 'games/:gameId/settings', name: 'game-settings', component: () => import('@/views/GameSettingsView.vue'), meta: { requiresAuth: true, roles: ['admin'] } },
        { path: 'my-servers/:orderId/files', name: 'my-instance-files', component: () => import('@/views/InstanceFilesView.vue') },
        {
          path: 'admin/users',
          name: 'admin-users',
          component: () => import('@/views/AdminUsersView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
        {
          path: 'admin/orders',
          name: 'admin-orders',
          component: () => import('@/views/AdminOrdersView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
        {
          path: 'admin/instances',
          name: 'admin-instances',
          component: () => import('@/views/AdminInstancesView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
        {
          path: 'admin/instances/:instanceId/files',
          name: 'admin-instance-files',
          component: () => import('@/views/InstanceFilesView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
        {
          path: 'admin/games',
          name: 'admin-games',
          component: () => import('@/views/AdminGamesView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
        {
          path: 'admin/games/:gameId/builds',
          name: 'admin-game-builds',
          component: () => import('@/views/AdminGameBuildsView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
        {
          path: 'admin/nodes',
          name: 'admin-nodes',
          component: () => import('@/views/AdminNodesView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
        {
          path: 'admin/node-agents',
          name: 'admin-node-agents',
          component: () => import('@/views/AdminNodeAgentsView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
        {
          path: 'admin/branches',
          name: 'admin-branches',
          component: () => import('@/views/AdminBranchesView.vue'),
          meta: { requiresAuth: true, roles: ['admin'] },
        },
      ],
    },
  ],
})

// 路由守卫：未登录跳登录页；角色不匹配跳回首页
router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.meta.requiresAuth && !auth.isAuthenticated) {
    return { name: 'login' }
  }
  if (to.name === 'login' && auth.isAuthenticated) {
    return { name: 'launcher' }
  }
  const roles = to.meta.roles as string[] | undefined
  if (roles && roles.includes('admin') && !auth.isAdmin) {
    return { name: 'launcher' }
  }
})

export default router

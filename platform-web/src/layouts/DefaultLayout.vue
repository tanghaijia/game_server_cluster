<template>
  <div class="flex min-h-screen">
    <!-- 侧边栏：按角色渲染菜单 -->
    <aside class="w-56 border-r bg-card p-4">
      <div class="mb-6 text-lg font-semibold">Platform Console</div>
      <nav class="space-y-1 text-sm">
        <RouterLink
          v-for="item in menu"
          :key="item.to"
          :to="item.to"
          class="block rounded px-3 py-2 hover:bg-muted"
          :class="{ 'text-muted-foreground': !isActive(item.to) }"
        >
          {{ item.label }}
        </RouterLink>
      </nav>
    </aside>

    <!-- 主内容区 -->
    <main class="flex-1 p-8">
      <div class="mb-6 flex items-center justify-end gap-4">
        <span class="text-sm text-muted-foreground">
          {{ auth.user?.username }}
          <span v-if="auth.isAdmin" class="ml-1 rounded bg-primary px-1.5 py-0.5 text-xs text-primary-foreground">管理员</span>
        </span>
        <button class="text-sm text-muted-foreground hover:underline" @click="onLogout">退出登录</button>
      </div>
      <RouterView />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { useGameTheme } from '@/composables/useGameTheme'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

// 按当前游戏切换主题色（--primary 运行时覆盖）
useGameTheme()

// 当前游戏（路由参数）
const gameId = computed(() => route.params.gameId as string | undefined)

const menu = computed(() => {
  const items = [{ to: '/', label: '游戏列表' }]
  // 游戏空间内动态显示该游戏的入口
  if (gameId.value) {
    items.push(
      { to: '/games/' + gameId.value + '/servers', label: '服务器' },
      { to: '/games/' + gameId.value + '/orders', label: '订单' },
    )
    if (auth.isAdmin) {
      items.push({ to: '/games/' + gameId.value + '/settings', label: '游戏设置' })
    }
  }
  if (auth.isAdmin) {
    items.push(
      { to: '/admin/users', label: '用户管理' },
      { to: '/admin/orders', label: '订单管理' },
      { to: '/admin/instances', label: '实例总览' },
      { to: '/admin/games', label: '游戏管理' },
      { to: '/admin/nodes', label: '节点管理' },
      { to: '/admin/node-agents', label: 'NodeAgent 管理' },
      { to: '/admin/branches', label: '分支管理' },
    )
  }
  return items
})

function isActive(to: string) {
  return route.path === to
}

function onLogout() {
  auth.logout()
  router.push({ name: 'login' })
}
</script>

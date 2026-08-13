<template>
  <div class="flex min-h-screen">
    <!-- 侧边栏（骨架） -->
    <aside class="w-56 border-r bg-card p-4">
      <div class="mb-6 text-lg font-semibold">Platform Console</div>
      <nav class="space-y-1 text-sm">
        <RouterLink to="/" class="block rounded px-3 py-2 hover:bg-muted">仪表盘</RouterLink>
        <RouterLink to="/" class="block rounded px-3 py-2 text-muted-foreground hover:bg-muted">实例</RouterLink>
        <RouterLink to="/" class="block rounded px-3 py-2 text-muted-foreground hover:bg-muted">订单</RouterLink>
        <RouterLink to="/" class="block rounded px-3 py-2 text-muted-foreground hover:bg-muted">账单</RouterLink>
        <RouterLink to="/" class="block rounded px-3 py-2 text-muted-foreground hover:bg-muted">用户</RouterLink>
      </nav>
    </aside>

    <!-- 主内容区 -->
    <main class="flex-1 p-8">
      <div class="mb-6 flex items-center justify-end">
        <button class="text-sm text-muted-foreground hover:underline" @click="onLogout">退出登录</button>
      </div>
      <RouterView />
    </main>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'

import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

function onLogout() {
  auth.logout()
  router.push({ name: 'login' })
}
</script>

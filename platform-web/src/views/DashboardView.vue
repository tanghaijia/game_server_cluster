<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">仪表盘</h1>
      <p class="text-sm text-muted-foreground">
        {{ greeting }}——左侧菜单按角色展示：{{ auth.isAdmin ? '管理员视图（含用户/订单/实例管理）' : '用户视图（我的服务器/我的订单）' }}
      </p>
    </div>
    <div class="grid grid-cols-1 gap-4 md:grid-cols-4">
      <div v-for="card in cards" :key="card.label" class="rounded-lg border p-4">
        <p class="text-sm text-muted-foreground">{{ card.label }}</p>
        <p class="mt-1 text-2xl font-semibold">{{ card.value }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()

const greeting = computed(() => `你好，${auth.user?.username ?? '访客'}`)

const cards = computed(() =>
  auth.isAdmin
    ? [
        { label: '全部订单', value: '--' },
        { label: '全部实例', value: '--' },
        { label: '注册用户', value: '--' },
        { label: '本月营收（分）', value: '--' },
      ]
    : [
        { label: '我的订单', value: '--' },
        { label: '我的实例', value: '--' },
        { label: '账户角色', value: auth.isAdmin ? '管理员' : '普通用户' },
        { label: '账户状态', value: '--' },
      ],
)
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">订单管理</h1>
      <p class="text-sm text-muted-foreground">全部订单（可按用户过滤）。</p>
    </div>
    <div class="flex max-w-xs items-center gap-2">
      <input v-model="filterUser" type="text" placeholder="按 user_id 过滤" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      <button class="rounded-md border px-3 py-2 text-sm hover:bg-muted" @click="load">查询</button>
    </div>
    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">订单</th>
            <th class="px-4 py-3">用户</th>
            <th class="px-4 py-3">游戏</th>
            <th class="px-4 py-3">金额（分）</th>
            <th class="px-4 py-3">实例</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="o in orders" :key="o.ID" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ o.ID }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ o.UserID }}</td>
            <td class="px-4 py-3">{{ o.GameID }}</td>
            <td class="px-4 py-3">{{ o.Amount }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ o.InstanceID || '-' }}</td>
            <td class="px-4 py-3">
              <span class="rounded bg-muted px-2 py-0.5 text-xs">{{ statusText(o.Status) }}</span>
            </td>
            <td class="px-4 py-3">
              <button
                v-if="o.Status === 0 && !o.InstanceID"
                class="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90"
                @click="onProvision(o.ID)"
              >
                直接开服（免支付）
              </button>
            </td>
          </tr>
          <tr v-if="!orders.length">
            <td colspan="7" class="px-4 py-8 text-center text-muted-foreground">暂无订单</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { listOrders, provisionOrder, type Order } from '@/api/orders'

const orders = ref<Order[]>([])
const filterUser = ref('')
const error = ref('')

const statusText = (s: number) => ['created', 'paid', 'cancelled', 'refunded', 'provisioned'][s] ?? 'unknown'

async function load() {
  orders.value = await listOrders(filterUser.value || undefined)
}

async function onProvision(id: string) {
  error.value = ''
  try {
    await provisionOrder(id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '开服失败（controller 是否已启动？）'
  }
}

onMounted(load)
</script>

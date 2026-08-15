<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <div class="flex items-center gap-2">
          <RouterLink to="/" class="text-sm text-muted-foreground hover:underline">← 游戏列表</RouterLink>
          <RouterLink :to="{ name: 'game-servers', params: { gameId } }" class="text-sm text-muted-foreground hover:underline">服务器</RouterLink>
        </div>
        <h1 class="mt-1 text-2xl font-semibold">{{ gameName }} · 订单</h1>
        <p class="text-sm text-muted-foreground">下单 → 支付（占位）→ 创建实例（stopped）→ 到「服务器」开服。</p>
      </div>
    </div>

    <!-- 下单（game_id 自动取自当前游戏） -->
    <form class="flex max-w-md items-end gap-3 rounded-lg border p-4" @submit.prevent="onCreate">
      <div class="w-40">
        <label class="mb-1 block text-sm font-medium">金额（分）</label>
        <input v-model.number="form.amount" type="number" min="1" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">
        下单
      </button>
    </form>

    <!-- 订单列表 -->
    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">订单</th>
            <th class="px-4 py-3">金额（分）</th>
            <th class="px-4 py-3">实例</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="o in orders" :key="o.ID" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ o.ID }}</td>
            <td class="px-4 py-3">{{ o.Amount }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ o.InstanceID || '-' }}</td>
            <td class="px-4 py-3">
              <span class="rounded bg-muted px-2 py-0.5 text-xs">{{ statusText(o.Status) }}</span>
            </td>
            <td class="px-4 py-3">
              <button
                v-if="o.Status === 0"
                class="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90"
                @click="onPay(o.ID)"
              >
                支付并创建实例
              </button>
            </td>
          </tr>
          <tr v-if="!orders.length">
            <td colspan="5" class="px-4 py-8 text-center text-muted-foreground">暂无订单</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'

import { getGame } from '@/api/games'
import { createOrder, listOrders, payOrder, type Order } from '@/api/orders'

const route = useRoute()

const gameId = computed(() => route.params.gameId as string)
const gameName = ref(gameId.value)

const orders = ref<Order[]>([])
const form = reactive({ amount: 100 })
const error = ref('')

const statusText = (s: number) => ['created', 'paid', 'cancelled', 'refunded', 'provisioned', '已下架'][s] ?? 'unknown'

async function load() {
  error.value = ''
  try {
    orders.value = await listOrders(undefined, gameId.value)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载订单失败'
  }
}

async function onCreate() {
  error.value = ''
  if (form.amount <= 0) {
    error.value = '请填写金额'
    return
  }
  try {
    await createOrder({ game_id: gameId.value, amount: form.amount })
    form.amount = 100
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '下单失败'
  }
}

async function onPay(id: string) {
  error.value = ''
  try {
    await payOrder(id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '支付失败（controller 是否已启动？）'
  }
}

onMounted(async () => {
  load()
  try {
    const g = await getGame(gameId.value)
    gameName.value = g.profile?.display_name || g.Name
  } catch {
    /* 保持默认 */
  }
})
</script>

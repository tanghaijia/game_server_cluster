<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <div class="flex items-center gap-2">
          <RouterLink to="/" class="text-sm text-muted-foreground hover:underline">← 游戏列表</RouterLink>
        </div>
        <h1 class="mt-1 text-2xl font-semibold">{{ gameName }} · 服务器</h1>
        <p class="text-sm text-muted-foreground">本游戏的实例列表。</p>
      </div>
      <button class="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50" :disabled="busy" @click="load">
        刷新
      </button>
    </div>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">订单</th>
            <th class="px-4 py-3">实例</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">节点</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="inst in instances" :key="inst.instance_id" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ inst.order_id }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ inst.instance_id }}</td>
            <td class="px-4 py-3">
              <span class="rounded bg-muted px-2 py-0.5 text-xs">{{ statusText(inst.status) }}</span>
            </td>
            <td class="px-4 py-3 text-xs">{{ inst.node_agent ?? '-' }}</td>
            <td class="px-4 py-3">
              <button
                v-if="action(inst.status)"
                class="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90 disabled:opacity-50"
                :disabled="busy"
                @click="onAction(inst, action(inst.status)!)"
              >
                {{ action(inst.status)!.label }}
              </button>
              <span v-else class="text-xs text-muted-foreground">-</span>
              <button class="ml-2 rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="openFiles(inst)">文件</button>
            </td>
          </tr>
          <tr v-if="!instances.length">
            <td colspan="5" class="px-4 py-8 text-center text-muted-foreground">暂无服务器——先去「订单」下单并开服</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { getGame } from '@/api/games'
import {
  instanceActions,
  myInstances,
  startOrderInstance,
  statusText,
  stopOrderInstance,
  type UserInstance,
} from '@/api/instances'

const route = useRoute()
const router = useRouter()

const gameId = computed(() => route.params.gameId as string)
const gameName = ref(gameId.value)

const instances = ref<UserInstance[]>([])
const busy = ref(false)
const error = ref('')

const action = (status: string) => instanceActions(status)

async function load() {
  error.value = ''
  try {
    instances.value = await myInstances(gameId.value)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载实例失败（请确认已登录、后端已启动）'
  }
}

function openFiles(inst: UserInstance) {
  router.push({
    name: 'my-instance-files',
    params: { orderId: inst.order_id },
    query: { running: inst.status === 'running' ? '1' : '0' },
  })
}

async function onAction(inst: UserInstance, act: { label: string; action: 'start' | 'stop' }) {
  busy.value = true
  error.value = ''
  try {
    if (act.action === 'start') {
      await startOrderInstance(inst.order_id)
    } else {
      await stopOrderInstance(inst.order_id)
    }
    setTimeout(load, 800)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败（controller 是否已启动？）'
  } finally {
    busy.value = false
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

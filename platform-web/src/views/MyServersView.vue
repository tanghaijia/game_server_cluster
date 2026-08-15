<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold">我的服务器</h1>
        <p class="text-sm text-muted-foreground">已支付订单关联的游戏实例。</p>
      </div>
      <button
        class="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50"
        :disabled="busy"
        @click="load"
      >
        刷新
      </button>
    </div>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">订单</th>
            <th class="px-4 py-3">实例</th>
            <th class="px-4 py-3">游戏</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">节点</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="inst in instances" :key="inst.instance_id" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ inst.order_id }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ inst.instance_id }}</td>
            <td class="px-4 py-3">{{ inst.game_id }}</td>
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
            <td colspan="6" class="px-4 py-8 text-center text-muted-foreground">暂无实例——先去「我的订单」下单并支付</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import {
  instanceActions,
  myInstances,
  startOrderInstance,
  statusText,
  stopOrderInstance,
  type UserInstance,
} from '@/api/instances'

const router = useRouter()

const instances = ref<UserInstance[]>([])
const busy = ref(false)
const error = ref('')

const action = (status: string) => instanceActions(status)

async function load() {
  error.value = ''
  try {
    instances.value = await myInstances()
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
    // controller 状态流转是异步的，稍后刷新
    setTimeout(load, 800)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败（controller 是否已启动？）'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">实例总览</h1>
      <p class="text-sm text-muted-foreground">全部用户订单关联的实例状态（controller 不可达时显示 unknown）。</p>
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
            <th class="px-4 py-3">连接地址</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="inst in instances" :key="inst.instance_id" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ inst.order_id }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ inst.instance_id }}</td>
            <td class="px-4 py-3">{{ inst.game_id }}</td>
            <td class="px-4 py-3">
              <span
                class="rounded px-2 py-0.5 text-xs"
                :class="inst.status === 'failed' ? 'bg-red-100 text-red-700' : 'bg-muted'"
                :title="inst.fail_reason || undefined"
              >
                {{ statusText(inst.status) }}
              </span>
              <span v-if="inst.status === 'failed' && inst.fail_reason" class="mt-1 block max-w-[240px] truncate text-[11px] text-red-500" :title="inst.fail_reason">
                原因：{{ inst.fail_reason }}
              </span>
            </td>
            <td class="px-4 py-3 text-xs">{{ inst.node_agent ?? '-' }}</td>
            <td class="px-4 py-3 text-xs">
              <span v-if="inst.connect_address" class="inline-flex items-center gap-1 font-mono">{{ inst.connect_address }}
                <button class="rounded border px-1.5 py-0.5 text-[10px] hover:bg-muted" title="复制连接地址" @click="copyAddress(inst.connect_address!)">复制</button>
              </span>
              <span v-else class="text-muted-foreground">-</span>
            </td>
            <td class="px-4 py-3">
              <div class="flex items-center gap-1.5">
                <button
                  class="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                  :disabled="busy || !canStart(inst.status)"
                  :title="canStart(inst.status) ? '启动实例' : actionDisabledReason(inst.status, 'start')"
                  @click="onStart(inst)"
                >启动</button>
                <button
                  class="rounded-md border border-destructive/40 px-3 py-1 text-xs text-destructive hover:bg-destructive/5 disabled:cursor-not-allowed disabled:opacity-40"
                  :disabled="busy || !canStop(inst.status)"
                  :title="canStop(inst.status) ? '停止实例（停止失败可重试）' : actionDisabledReason(inst.status, 'stop')"
                  @click="onStop(inst)"
                >停止</button>
                <button class="ml-1 rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="openFiles(inst)">文件</button>
              </div>
            </td>
          </tr>
          <tr v-if="!instances.length">
            <td colspan="7" class="px-4 py-8 text-center text-muted-foreground">暂无实例</td>
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
  actionDisabledReason,
  allInstances,
  canStart,
  canStop,
  isTransitionalStatus,
  startOrderInstance,
  statusText,
  stopOrderInstance,
  type UserInstance,
} from '@/api/instances'

const router = useRouter()

const instances = ref<UserInstance[]>([])
const busy = ref(false)
const error = ref('')

async function load() {
  error.value = ''
  try {
    instances.value = await allInstances()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载实例失败（请确认已登录、后端已启动）'
  }
}

function copyAddress(addr: string) {
  navigator.clipboard?.writeText(addr).then(
    () => { error.value = '' },
    () => { error.value = '复制失败，请手动复制' },
  )
}

function openFiles(inst: UserInstance) {
  router.push({
    name: 'admin-instance-files',
    params: { instanceId: inst.instance_id },
    query: { running: inst.status === 'running' ? '1' : '0' },
  })
}

// 启动/停止是异步的：轮询等待实例离开中间态（启动、停止失败都会进入 failed 终态，
// 轮询即停止，此时按钮恢复可用，便于停止失败后二次重试）
async function pollUntilSettled(instanceId: string) {
  for (let i = 0; i < 15; i++) {
    await new Promise((r) => setTimeout(r, 1000))
    await load()
    const cur = instances.value.find((x) => x.instance_id === instanceId)
    if (!cur || !isTransitionalStatus(cur.status)) return
  }
}

async function onStart(inst: UserInstance) {
  busy.value = true
  error.value = ''
  try {
    await startOrderInstance(inst.order_id)
    await pollUntilSettled(inst.instance_id)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '启动失败（controller 是否已启动？）'
  } finally {
    busy.value = false
  }
}

async function onStop(inst: UserInstance) {
  busy.value = true
  error.value = ''
  try {
    await stopOrderInstance(inst.order_id)
    await pollUntilSettled(inst.instance_id)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '停止失败（controller 是否已启动？）'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>
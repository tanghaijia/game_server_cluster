<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">节点管理</h1>
      <p class="text-sm text-muted-foreground">服务器节点（IP 是连接 node_agent 的基础）。</p>
    </div>

    <form class="flex max-w-md items-end gap-3 rounded-lg border p-4" @submit.prevent="onCreate">
      <div class="flex-1">
        <label class="mb-1 block text-sm font-medium">节点 IP</label>
        <input v-model="ip" type="text" placeholder="如 192.168.1.10" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">新增</button>
    </form>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">ID</th>
            <th class="px-4 py-3">IP</th>
            <th class="px-4 py-3">核心数</th>
            <th class="px-4 py-3">内存(MB)</th>
            <th class="px-4 py-3">存储(MB)</th>
            <th class="px-4 py-3">地域</th>
            <th class="px-4 py-3">服务商</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="n in nodes" :key="n.Id" class="border-b last:border-0">
            <td class="px-4 py-3">{{ n.Id }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ n.Ip }}</td>
            <td class="px-4 py-3">{{ n.CoreNum || '-' }}</td>
            <td class="px-4 py-3">{{ n.MemorySize || '-' }}</td>
            <td class="px-4 py-3">{{ n.StorageSize || '-' }}</td>
            <td class="px-4 py-3">{{ n.Location || '-' }}</td>
            <td class="px-4 py-3">{{ n.ServiceProvider || '-' }}</td>
          </tr>
          <tr v-if="!nodes.length">
            <td colspan="7" class="px-4 py-8 text-center text-muted-foreground">暂无节点</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { createNode, listNodes, type Node } from '@/api/admin'

const nodes = ref<Node[]>([])
const ip = ref('')
const error = ref('')

async function load() {
  error.value = ''
  try {
    nodes.value = await listNodes()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载失败（controller 是否已启动？）'
  }
}

async function onCreate() {
  error.value = ''
  if (!ip.value) {
    error.value = '请填写节点 IP'
    return
  }
  try {
    await createNode(ip.value)
    ip.value = ''
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '新增失败'
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">NodeAgent 管理</h1>
      <p class="text-sm text-muted-foreground">节点代理：只有 Enabled 的 agent 参与实例调度与缓存循环。</p>
    </div>

    <form class="flex max-w-2xl items-end gap-3 rounded-lg border p-4" @submit.prevent="onCreate">
      <div class="flex-1">
        <label class="mb-1 block text-sm font-medium">名称</label>
        <input v-model="form.name" type="text" placeholder="如 node-agent-1" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div class="w-40">
        <label class="mb-1 block text-sm font-medium">节点 ID</label>
        <input v-model="form.nodeId" type="text" placeholder="对应节点 Id" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div class="w-32">
        <label class="mb-1 block text-sm font-medium">端口</label>
        <input v-model.number="form.port" type="number" placeholder="9090" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">新增</button>
    </form>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">ID</th>
            <th class="px-4 py-3">节点 ID</th>
            <th class="px-4 py-3">端口</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">存活</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in agents" :key="a.ID" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ a.ID }}</td>
            <td class="px-4 py-3">{{ a.NodeId || '-' }}</td>
            <td class="px-4 py-3">{{ a.Port }}</td>
            <td class="px-4 py-3">
              <span class="rounded px-2 py-0.5 text-xs" :class="a.Status === 1 ? 'bg-primary text-primary-foreground' : 'bg-muted'">
                {{ a.Status === 1 ? 'Enabled' : 'Disabled' }}
              </span>
            </td>
            <td class="px-4 py-3">
              <span
                v-if="a.Status === 1"
                class="rounded px-2 py-0.5 text-xs"
                :class="a.Alive ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'"
              >
                {{ a.Alive ? '存活' : '失联' }}
              </span>
              <span v-else class="text-xs text-muted-foreground">-</span>
              <div v-if="a.Alive && a.LastHeartbeatAt" class="mt-0.5 text-xs text-muted-foreground">
                心跳 {{ fmtTime(a.LastHeartbeatAt) }}
              </div>
            </td>
            <td class="px-4 py-3">
              <button
                v-if="a.Status === 1"
                class="rounded-md border px-3 py-1 text-xs hover:bg-muted"
                @click="onToggle(a, false)"
              >
                停用
              </button>
              <button
                v-else
                class="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90"
                @click="onToggle(a, true)"
              >
                启用
              </button>
              <button
                class="ml-1 rounded-md border px-3 py-1 text-xs hover:bg-muted"
                :disabled="!a.NodeId"
                :title="a.NodeId ? '查看该 node_agent 运行日志' : '该 agent 未绑定节点（NodeId 为空），无法定位日志服务地址'"
                @click="openLogs(a)"
              >
                日志
              </button>
            </td>
          </tr>
          <tr v-if="!agents.length">
            <td colspan="6" class="px-4 py-8 text-center text-muted-foreground">暂无 node_agent</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>

    <!-- 日志查看弹层（P2：直连 node_agent 日志端点） -->
    <AgentLogsDialog v-if="logAgent" :agent="logAgent" @close="logAgent = null" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import { createNodeAgent, listNodeAgents, setNodeAgentEnabled, type NodeAgent } from '@/api/admin'
import AgentLogsDialog from './AgentLogsDialog.vue'

const agents = ref<NodeAgent[]>([])
const error = ref('')
const logAgent = ref<NodeAgent | null>(null)

const fmtTime = (t: string) => new Date(t).toLocaleString()
const form = reactive({ name: '', nodeId: '', port: 9090 })

async function load() {
  error.value = ''
  try {
    agents.value = await listNodeAgents()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载失败（controller 是否已启动？）'
  }
}

async function onCreate() {
  error.value = ''
  if (!form.name) {
    error.value = '请填写名称'
    return
  }
  try {
    await createNodeAgent({ name: form.name, node_id: form.nodeId || undefined, port: form.port || undefined })
    form.name = ''
    form.nodeId = ''
    form.port = 9090
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '新增失败'
  }
}

async function onToggle(a: NodeAgent, enabled: boolean) {
  error.value = ''
  try {
    await setNodeAgentEnabled(a.ID, enabled)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败'
  }
}

function openLogs(a: NodeAgent) {
  error.value = ''
  logAgent.value = a
}

onMounted(load)
</script>

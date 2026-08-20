<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">节点管理</h1>
      <p class="text-sm text-muted-foreground">服务器节点（IP 是连接 node_agent 的基础；容量字段参与调度判定，带宽上限用于带宽评分）。</p>
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
            <th class="px-4 py-3">主频(GHz)</th>
            <th class="px-4 py-3">内存(MB)</th>
            <th class="px-4 py-3">存储(MB)</th>
            <th class="px-4 py-3">地域</th>
            <th class="px-4 py-3">带宽上限(Mbps)</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="n in nodes" :key="n.Id" class="border-b last:border-0">
            <td class="px-4 py-3">{{ n.Id }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ n.Ip }}</td>
            <td class="px-4 py-3">{{ n.CoreNum || '-' }}</td>
            <td class="px-4 py-3">{{ n.CoreFrequency || '-' }}</td>
            <td class="px-4 py-3">{{ n.MemorySize || '-' }}</td>
            <td class="px-4 py-3">{{ n.StorageSize || '-' }}</td>
            <td class="px-4 py-3">{{ n.Location || '-' }}</td>
            <td class="px-4 py-3">{{ n.NetRxLimitMbps ?? '-' }}/{{ n.NetTxLimitMbps ?? '-' }}</td>
            <td class="px-4 py-3">
              <div class="flex gap-2">
                <button class="rounded border px-2 py-1 text-xs hover:bg-muted" @click="onEdit(n)">编辑</button>
                <button class="rounded border px-2 py-1 text-xs text-red-600 hover:bg-red-50" @click="onDelete(n)">删除</button>
              </div>
            </td>
          </tr>
          <tr v-if="!nodes.length">
            <td colspan="9" class="px-4 py-8 text-center text-muted-foreground">暂无节点</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>

    <!-- 编辑弹窗 -->
    <div v-if="editing" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="editing = null">
      <div class="w-[480px] rounded-lg border bg-background p-5">
        <div class="mb-4 text-lg font-semibold">编辑节点 #{{ editing.Id }}（{{ editing.Ip }}）</div>
        <form class="grid grid-cols-2 gap-3" @submit.prevent="onSave">
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">IP</label>
            <input v-model="form.ip" type="text" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">地域</label>
            <input v-model="form.location" type="text" placeholder="如 sg" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">核心数</label>
            <input v-model.number="form.core_num" type="number" min="1" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">主频(GHz)</label>
            <input v-model.number="form.core_frequency" type="number" step="0.1" min="0" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">内存(MB)</label>
            <input v-model.number="form.memory_size" type="number" min="0" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">存储(MB)</label>
            <input v-model.number="form.storage_size" type="number" min="0" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">带宽上限 rx(Mbps)</label>
            <input v-model.number="form.net_rx_limit_mbps" type="number" min="0" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">带宽上限 tx(Mbps)</label>
            <input v-model.number="form.net_tx_limit_mbps" type="number" min="0" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div class="col-span-2">
            <label class="mb-1 block text-xs text-muted-foreground">服务商</label>
            <input v-model="form.service_provider" type="text" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div class="col-span-2 flex justify-end gap-2 pt-2">
            <button type="button" class="rounded-md border px-4 py-2 text-sm hover:bg-muted" @click="editing = null">取消</button>
            <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90">保存</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import { createNode, deleteNode, listNodes, updateNode, type Node, type NodeUpdate } from '@/api/admin'

const nodes = ref<Node[]>([])
const ip = ref('')
const error = ref('')

const editing = ref<Node | null>(null)
const form = reactive<NodeUpdate>({})

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

function onEdit(n: Node) {
  editing.value = n
  Object.assign(form, {
    ip: n.Ip,
    core_num: n.CoreNum,
    core_frequency: n.CoreFrequency,
    memory_size: n.MemorySize,
    storage_size: n.StorageSize,
    location: n.Location,
    service_provider: n.ServiceProvider,
    net_rx_limit_mbps: n.NetRxLimitMbps,
    net_tx_limit_mbps: n.NetTxLimitMbps,
  })
}

async function onSave() {
  if (!editing.value) return
  error.value = ''
  try {
    await updateNode(editing.value.Id, form)
    editing.value = null
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '保存失败'
  }
}

async function onDelete(n: Node) {
  if (!window.confirm(`确定删除节点 #${n.Id}（${n.Ip}）？被 node_agent 引用的节点无法删除。`)) return
  error.value = ''
  try {
    await deleteNode(n.Id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '删除失败'
  }
}

onMounted(load)
</script>

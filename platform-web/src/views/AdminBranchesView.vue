<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">分支管理</h1>
      <p class="text-sm text-muted-foreground">Steam 分支同步与节点缓存管理。</p>
    </div>

    <div class="flex max-w-3xl items-end gap-3 rounded-lg border p-4">
      <div class="w-64">
        <label class="mb-1 block text-sm font-medium">游戏</label>
        <select v-model="gameId" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" @change="loadBranches">
          <option value="">请选择游戏</option>
          <option v-for="g in games" :key="g.ID" :value="g.ID">{{ g.Name }} ({{ g.ID }})</option>
        </select>
      </div>
      <div class="flex-1">
        <label class="mb-1 block text-sm font-medium">目标 node_agent（缓存更新用）</label>
        <select v-model="nodeAgentId" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2">
          <option value="">未选择</option>
          <option v-for="a in agents" :key="a.ID" :value="a.ID">{{ a.ID }} ({{ a.Status === 1 ? '启用' : '停用' }})</option>
        </select>
      </div>
      <button class="rounded-md border px-4 py-2 text-sm hover:bg-muted" :disabled="!gameId" @click="onSync">同步分支</button>
    </div>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">分支</th>
            <th class="px-4 py-3">最新构建</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">保底副本</th>
            <th class="px-4 py-3">描述</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in branches" :key="b.Id" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ b.BranchName }}</td>
            <td class="px-4 py-3">{{ b.LastBuildId }}</td>
            <td class="px-4 py-3">
              <span class="rounded px-2 py-0.5 text-xs" :class="branchBadge(b.Status)">
                {{ branchStatus(b.Status) }}
              </span>
            </td>
            <td class="px-4 py-3">
              <div class="flex items-center gap-1.5">
                <input
                  v-model.number="minReplicasEdits[b.BranchName]"
                  type="number"
                  min="0"
                  class="w-16 rounded-md border px-2 py-1 text-xs outline-none focus:ring-2"
                  title="保底副本数：0=按需（实例驱动），N=无实例也常驻 N 份"
                />
                <button
                  class="rounded-md border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
                  :disabled="minReplicasEdits[b.BranchName] === undefined"
                  @click="onSetMinReplicas(b.BranchName)"
                >
                  保存
                </button>
              </div>
            </td>
            <td class="px-4 py-3 text-xs text-muted-foreground">{{ b.Description || '-' }}</td>
            <td class="px-4 py-3">
              <button
                class="rounded-md border px-3 py-1 text-xs hover:bg-muted disabled:opacity-50"
                :disabled="!nodeAgentId"
                @click="onUpdateCache(b.BranchName)"
              >
                更新缓存
              </button>
            </td>
          </tr>
          <tr v-if="!branches.length && gameId">
            <td colspan="6" class="px-4 py-8 text-center text-muted-foreground">暂无分支——点「同步分支」从 asset_service 拉取</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { listBranches, listGames, listNodeAgents, setBranchMinReplicas, syncBranches, updateBranchCache, type Game, type NodeAgent, type SteamBranch } from '@/api/admin'

const games = ref<Game[]>([])
const agents = ref<NodeAgent[]>([])
const branches = ref<SteamBranch[]>([])
const gameId = ref('')
const nodeAgentId = ref('')
const error = ref('')
// 保底副本数编辑态：分支名 → 待保存值（默认取当前 MinReplicas）
const minReplicasEdits = ref<Record<string, number>>({})

const branchStatus = (s: number) => ['Disable', 'Enable', 'Abandoned'][s] ?? 'unknown'
const branchBadge = (s: number) => (s === 1 ? 'bg-primary text-primary-foreground' : 'bg-muted')

async function loadGames() {
  try {
    games.value = await listGames()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载游戏失败（controller 是否已启动？）'
  }
}

async function loadAgents() {
  try {
    agents.value = await listNodeAgents()
  } catch {
    // 忽略：缓存更新下拉无候选时按钮禁用即可
  }
}

async function loadBranches() {
  error.value = ''
  branches.value = []
  if (!gameId.value) return
  try {
    branches.value = await listBranches(gameId.value)
    minReplicasEdits.value = Object.fromEntries(branches.value.map((b) => [b.BranchName, b.MinReplicas ?? 0]))
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载分支失败'
  }
}

async function onSync() {
  error.value = ''
  try {
    await syncBranches(gameId.value)
    await loadBranches()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '同步失败（asset_service 是否可用？）'
  }
}

async function onUpdateCache(branch: string) {
  error.value = ''
  try {
    await updateBranchCache(gameId.value, branch, nodeAgentId.value)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '缓存更新失败'
  }
}

async function onSetMinReplicas(branch: string) {
  error.value = ''
  const v = Math.max(0, Math.floor(minReplicasEdits.value[branch] ?? 0))
  try {
    await setBranchMinReplicas(gameId.value, branch, v)
    minReplicasEdits.value[branch] = v
    await loadBranches() // 刷新（后台对账按新值收敛缓存）
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '设置保底副本数失败'
  }
}

onMounted(() => {
  loadGames()
  loadAgents()
})
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">分支管理</h1>
      <p class="text-sm text-muted-foreground">
        Steam 分支同步与节点缓存管理。后台按「保底副本（min_replicas）+ 运行实例需求」自动对账缓存；
        表格中的「更新缓存」仅用于手动把某分支下载/更新到指定节点。
      </p>
    </div>

    <div class="flex max-w-4xl items-end gap-3 rounded-lg border p-4">
      <div class="w-64">
        <label class="mb-1 block text-sm font-medium">游戏</label>
        <select v-model="gameId" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" @change="loadBranches">
          <option value="">请选择游戏</option>
          <option v-for="g in games" :key="g.ID" :value="g.ID">{{ g.Name }} ({{ g.ID }})</option>
        </select>
      </div>
      <div class="flex-1">
        <label class="mb-1 block text-sm font-medium">手动更新缓存的目标节点（可选）</label>
        <select v-model="nodeAgentId" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2">
          <option value="">未选择（仅查看分布，不做手动操作）</option>
          <option v-for="a in agents" :key="a.ID" :value="a.ID">{{ a.ID }} ({{ a.Status === 1 ? '启用' : '停用' }})</option>
        </select>
        <p class="mt-1 text-[11px] text-muted-foreground">
          只影响表格里「更新缓存」按钮的落点；不选也不会影响后台自动对账（保底副本/实例需求）。
        </p>
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
            <th class="px-4 py-3">缓存分布</th>
            <th class="px-4 py-3">描述</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="b in branches" :key="b.Id">
            <tr class="border-b last:border-0">
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
                <div class="mt-1 text-[10px] text-muted-foreground">
                  目标 {{ b.MinReplicas ?? 0 }} · 实际 {{ branchCacheCount(gameId, b.BranchName) }}
                </div>
              </td>
              <td class="px-4 py-3">
                <button
                  class="rounded-md border px-2 py-1 text-xs hover:bg-muted"
                  :class="expandedBranch === b.BranchName ? 'bg-muted' : ''"
                  @click="toggleBranchCache(b.BranchName)"
                >
                  分布（{{ branchCacheCount(gameId, b.BranchName) }}）
                </button>
              </td>
              <td class="px-4 py-3 text-xs text-muted-foreground">{{ b.Description || '-' }}</td>
              <td class="px-4 py-3">
                <button
                  class="rounded-md border px-3 py-1 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
                  :disabled="!nodeAgentId"
                  :title="nodeAgentId ? `把 ${b.BranchName} 最新版下载/更新到 ${nodeAgentId}` : '请先在上方选择目标节点（手动更新用）'"
                  @click="onUpdateCache(b.BranchName)"
                >
                  更新缓存
                </button>
              </td>
            </tr>
            <tr v-if="expandedBranch === b.BranchName" class="border-b bg-muted/30 last:border-0">
              <td colspan="7" class="px-4 py-2">
                <div class="flex flex-wrap items-center gap-2">
                  <span v-if="!branchCacheItems(gameId, b.BranchName).length" class="text-xs text-muted-foreground">
                    无节点缓存该分支（下载中/保底预热后出现在这里；快照约 30s 刷新）
                  </span>
                  <span
                    v-for="(c, i) in branchCacheItems(gameId, b.BranchName)"
                    :key="i"
                    class="inline-flex items-center gap-1.5 rounded border bg-background px-2 py-1 text-xs"
                    :title="c.last_error ? '失败原因：' + c.last_error : undefined"
                  >
                    <span class="font-mono">{{ c.node_agent_id }}</span>
                    <span :class="cacheStatusClass(c.status)">{{ CACHE_STATUS_LABELS[c.status] ?? c.status }}</span>
                    <span class="text-muted-foreground">build {{ c.build_id || '-' }}</span>
                    <span class="font-mono text-muted-foreground">{{ formatBytes(c.size_bytes ?? 0) }}</span>
                    <span
                      v-if="c.last_error && c.status !== 'available'"
                      class="max-w-[180px] truncate text-red-600"
                      :title="c.last_error"
                    >{{ c.last_error }}</span>
                  </span>
                </div>
              </td>
            </tr>
          </template>
          <tr v-if="!branches.length && gameId">
            <td colspan="7" class="px-4 py-8 text-center text-muted-foreground">暂无分支——点「同步分支」从 asset_service 拉取</td>
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
import { CACHE_STATUS_LABELS, formatBytes, observeCache, type NodeCacheItem } from '@/api/observe'

const games = ref<Game[]>([])
const agents = ref<NodeAgent[]>([])
const branches = ref<SteamBranch[]>([])
const gameId = ref('')
const nodeAgentId = ref('')
const error = ref('')
// 保底副本数编辑态：分支名 → 待保存值（默认取当前 MinReplicas）
const minReplicasEdits = ref<Record<string, number>>({})
// 缓存分布：NodeCacheView 快照（全部节点 × Enable 分支，约 30s 刷新）
const nodeCache = ref<NodeCacheItem[]>([])
// 展开查看缓存分布的分支名
const expandedBranch = ref('')

const branchStatus = (s: number) => ['Disable', 'Enable', 'Abandoned'][s] ?? 'unknown'
const branchBadge = (s: number) => (s === 1 ? 'bg-primary text-primary-foreground' : 'bg-muted')

const cacheStatusClass = (s: string) => {
  const map: Record<string, string> = { available: 'text-green-600', downloading: 'text-amber-600' }
  return map[s] ?? 'text-red-600'
}

// 某分支的缓存分布（node_agent → 状态/build/大小）
function branchCacheItems(gameID: string, branch: string): NodeCacheItem[] {
  return nodeCache.value.filter((c) => c.game_id === gameID && c.branch === branch)
}
function branchCacheCount(gameID: string, branch: string): number {
  return branchCacheItems(gameID, branch).length
}
function toggleBranchCache(branch: string) {
  expandedBranch.value = expandedBranch.value === branch ? '' : branch
}

async function loadNodeCache() {
  try {
    nodeCache.value = await observeCache()
  } catch {
    nodeCache.value = [] // controller 不可达时静默（分布列为 0）
  }
}

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
    await loadNodeCache() // 切换游戏时刷新缓存分布快照
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
  loadNodeCache()
  // 快照约 30s 刷新一次，这里定时同步，保证"保底 1 · 实际 N"对照不过期
  window.setInterval(() => loadNodeCache(), 30000)
})
</script>

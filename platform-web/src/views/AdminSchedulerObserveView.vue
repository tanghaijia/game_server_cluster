<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold">调度观测</h1>
        <p class="text-sm text-muted-foreground">节点资源 / 排队 / 调度事件 / 试调度干跑（管理员排障视角）。</p>
      </div>
      <div class="flex items-center gap-2 text-sm text-muted-foreground">
        <span>刷新间隔 5s</span>
        <button class="rounded-md border px-3 py-1.5 hover:bg-muted" @click="loadAll">立即刷新</button>
      </div>
    </div>

    <!-- 调度统计卡片 -->
    <div class="grid grid-cols-4 gap-4">
      <div class="rounded-lg border p-4">
        <div class="text-xs text-muted-foreground">调度成功</div>
        <div class="mt-1 text-2xl font-semibold text-green-600">{{ stats.attempts?.scheduled ?? 0 }}</div>
      </div>
      <div class="rounded-lg border p-4">
        <div class="text-xs text-muted-foreground">排队（当前）</div>
        <div class="mt-1 text-2xl font-semibold text-amber-600">{{ stats.queue_len ?? 0 }}</div>
      </div>
      <div class="rounded-lg border p-4">
        <div class="text-xs text-muted-foreground">调度失败</div>
        <div class="mt-1 text-2xl font-semibold text-red-600">{{ stats.attempts?.failed ?? 0 }}</div>
      </div>
      <div class="rounded-lg border p-4">
        <div class="text-xs text-muted-foreground">事件数（缓冲）</div>
        <div class="mt-1 text-2xl font-semibold">{{ stats.event_count ?? 0 }}</div>
      </div>
    </div>

    <!-- 节点总览 + 事件流 -->
    <div class="grid grid-cols-3 gap-6">
      <!-- 节点资源总览 -->
      <div class="col-span-2 rounded-lg border">
        <div class="flex items-baseline justify-between border-b px-4 py-3">
          <span class="text-sm font-medium">节点资源总览</span>
          <span class="text-[11px] text-muted-foreground">
            占用 = 心跳实际值 · 预留 = 已调度实例 request 之和（逻辑账本）· 可用 = 容量×80% − 预留（逻辑可分配量，不含实际占用，实际负载由压力状态兜底）
          </span>
        </div>
        <div class="max-h-96 overflow-auto">
          <table class="w-full text-xs">
            <thead class="sticky top-0 bg-background">
              <tr class="border-b text-left text-muted-foreground">
                <th class="px-3 py-2">节点</th>
                <th class="px-3 py-2">IP/地域</th>
                <th class="px-3 py-2">健康</th>
                <th class="px-3 py-2">压力</th>
                <th class="px-3 py-2" title="格式：可用 / 占用 / 预留。可用=容量×80%−预留（逻辑可分配量）；占用=心跳实际值；预留=已调度实例 request 之和">CPU 可用/占用/预留</th>
                <th class="px-3 py-2" title="格式：可用 / 占用 / 预留。可用=容量×80%−预留（逻辑可分配量）；占用=心跳实际值；预留=已调度实例 request 之和">内存 可用/占用/预留</th>
                <th class="px-3 py-2" title="带宽余量 = min(上限−已预留带宽, 上限−当前实际bps) / 上限（百分比）">带宽余量</th>
                <th class="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="n in nodes" :key="n.node_id" class="border-b last:border-0">
                <td class="px-3 py-2 font-mono">{{ n.node_id }}<span v-if="n.node_agent_id" class="ml-1 text-muted-foreground">({{ n.node_agent_id }})</span></td>
                <td class="px-3 py-2">{{ n.ip }}<span class="block text-muted-foreground">{{ n.location || '-' }}</span></td>
                <td class="px-3 py-2">
                  <span :class="healthClass(n.health)">{{ n.health }}</span>
                  <span v-if="!n.enabled" class="ml-1 rounded bg-muted px-1 text-xs">disabled</span>
                </td>
                <td class="px-3 py-2">
                  <span :class="pressureClass(n.pressure)">{{ n.pressure }}</span>
                </td>
                <td class="px-3 py-2">
                  <span class="font-mono">{{ formatMilliCpu(n.cpu_allocatable_milli) }}</span>
                  <span class="text-muted-foreground">/ {{ formatMilliCpu(n.cpu_used_milli) }} / {{ formatMilliCpu(n.cpu_reserved_milli) }}</span>
                </td>
                <td class="px-3 py-2">
                  <span class="font-mono">{{ formatBytes(n.mem_allocatable_bytes) }}</span>
                  <span class="text-muted-foreground">/ {{ formatBytes(n.mem_used_bytes) }} / {{ formatBytes(n.mem_reserved_bytes) }}</span>
                </td>
                <td class="px-3 py-2 font-mono">{{ (n.bandwidth_ratio * 100).toFixed(0) }}%</td>
                <td class="px-3 py-2">
                  <button class="rounded border px-2 py-0.5 text-xs hover:bg-muted" @click="showHistory(n)">曲线</button>
                </td>
              </tr>
              <tr v-if="!nodes.length">
                <td colspan="8" class="px-3 py-8 text-center text-muted-foreground">暂无节点</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 事件流 -->
      <div class="rounded-lg border">
        <div class="flex items-center justify-between border-b px-4 py-3 text-sm font-medium">
          <span>调度事件流</span>
          <select v-model="eventFilter" class="rounded border bg-transparent px-2 py-1 text-xs" @change="loadEvents">
            <option value="">全部</option>
            <option v-for="(label, key) in EVENT_LABELS" :key="key" :value="key">{{ label }}</option>
          </select>
        </div>
        <div class="max-h-96 overflow-auto">
          <div v-for="e in events" :key="e.occurred_at + e.type + e.instance_id" class="border-b px-4 py-2 text-xs last:border-0">
            <div class="flex items-center justify-between">
              <span :class="eventClass(e.type)">{{ EVENT_LABELS[e.type] ?? e.type }}</span>
              <span class="text-muted-foreground">{{ fmtTime(e.occurred_at) }}</span>
            </div>
            <div v-if="e.instance_id || e.detail" class="mt-0.5 text-muted-foreground">
              <span v-if="e.instance_id" class="font-mono">{{ e.instance_id }}</span>
              <span v-if="e.detail"> · {{ e.detail }}</span>
            </div>
          </div>
          <div v-if="!events.length" class="px-4 py-8 text-center text-muted-foreground">暂无事件</div>
        </div>
      </div>
    </div>

    <!-- 排队队列 -->
    <div class="rounded-lg border">
      <div class="border-b px-4 py-3 text-sm font-medium">排队队列（{{ queue.length }}）</div>
      <div class="overflow-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="border-b text-left text-muted-foreground">
              <th class="px-3 py-2">实例</th>
              <th class="px-3 py-2">游戏</th>
              <th class="px-3 py-2">优先级</th>
              <th class="px-3 py-2">原因</th>
              <th class="px-3 py-2">重试</th>
              <th class="px-3 py-2">已等待</th>
              <th class="px-3 py-2">下次唤醒</th>
              <th class="px-3 py-2">剩余超时</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="q in queue" :key="q.instance_id" class="border-b last:border-0">
              <td class="px-3 py-2 font-mono">{{ q.instance_id }}</td>
              <td class="px-3 py-2">{{ q.game_id }}</td>
              <td class="px-3 py-2">{{ q.priority }}</td>
              <td class="px-3 py-2">{{ q.reason }}</td>
              <td class="px-3 py-2">{{ q.attempts }}</td>
              <td class="px-3 py-2">{{ Math.floor(q.wait_seconds / 60) }}m{{ q.wait_seconds % 60 }}s</td>
              <td class="px-3 py-2">{{ fmtTime(q.wake_at) }}</td>
              <td class="px-3 py-2" :class="q.remaining_seconds < 60 ? 'text-red-500' : ''">
                {{ Math.max(0, Math.floor(q.remaining_seconds / 60)) }}m
              </td>
            </tr>
            <tr v-if="!queue.length">
              <td colspan="8" class="px-3 py-8 text-center text-muted-foreground">队列为空</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 节点 game-cache 状态 -->
    <div class="rounded-lg border">
      <div class="border-b px-4 py-3 text-sm font-medium">
        节点 game-cache 状态（{{ cache.length }}）
        <span class="ml-2 text-xs font-normal text-muted-foreground">
          · 可用缓存共 {{ formatBytes(cacheTotalAvailable) }} · 下载中 {{ formatBytes(cacheTotalDownloading) }} · 保底分支可在「分支管理」设置 min_replicas
        </span>
      </div>
      <div class="overflow-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="border-b text-left text-muted-foreground">
              <th class="px-3 py-2">node_agent</th>
              <th class="px-3 py-2">node</th>
              <th class="px-3 py-2">游戏</th>
              <th class="px-3 py-2">分支</th>
              <th class="px-3 py-2">状态</th>
              <th class="px-3 py-2">build_id</th>
              <th class="px-3 py-2">进度</th>
              <th class="px-3 py-2" title="缓存内容实测大小（node 下载完成后上报；0 = 未知）">大小</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(c, i) in cache" :key="c.node_agent_id + '-' + c.game_id + '-' + c.branch + '-' + i" class="border-b last:border-0">
              <td class="px-3 py-2 font-mono">{{ c.node_agent_id }}</td>
              <td class="px-3 py-2 font-mono">{{ c.node_id || '-' }}</td>
              <td class="px-3 py-2">{{ c.game_id }}</td>
              <td class="px-3 py-2">{{ c.branch }}</td>
              <td class="px-3 py-2">
                <span :class="cacheStatusClass(c.status)">{{ CACHE_STATUS_LABELS[c.status] ?? c.status }}</span>
              </td>
              <td class="px-3 py-2 font-mono">{{ c.build_id || '-' }}</td>
              <td class="px-3 py-2 font-mono">{{ c.download_progress > 0 ? (c.download_progress * 100).toFixed(0) + '%' : '-' }}</td>
              <td class="px-3 py-2 font-mono">{{ formatBytes(c.size_bytes ?? 0) }}</td>
            </tr>
            <tr v-if="!cache.length">
              <td colspan="8" class="px-3 py-8 text-center text-muted-foreground">暂无缓存数据（NodeCacheView 快照周期刷新，需 node_agent 在线）</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 试调度干跑 -->
    <div class="rounded-lg border">
      <div class="border-b px-4 py-3 text-sm font-medium">试调度干跑（不预留、不落库，查看约束与评分）</div>
      <form class="flex flex-wrap items-end gap-3 p-4" @submit.prevent="onPreview">
        <div>
          <label class="mb-1 block text-xs text-muted-foreground">游戏</label>
          <input v-model="pv.game_id" placeholder="game_id，如 343050" class="rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
        </div>
        <div>
          <label class="mb-1 block text-xs text-muted-foreground">区域</label>
          <input v-model="pv.region" placeholder="如 sg（可选）" class="rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
        </div>
        <div>
          <label class="mb-1 block text-xs text-muted-foreground">CPU（核，可选）</label>
          <input v-model.number="pv.cpu" type="number" min="0.5" step="0.5" placeholder="默认取游戏配置" class="w-28 rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
        </div>
        <div>
          <label class="mb-1 block text-xs text-muted-foreground">内存（GB，可选）</label>
          <input v-model.number="pv.memGb" type="number" min="0.5" step="0.5" placeholder="默认取游戏配置" class="w-28 rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
        </div>
        <button type="submit" class="rounded-md bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:opacity-90">试调度</button>
      </form>

      <div v-if="preview" class="border-t p-4">
        <div class="mb-2 text-xs">
          结果：<span :class="outcomeClass(preview.outcome)">{{ preview.outcome }}</span>
          <span class="ml-2 text-muted-foreground">{{ preview.reason }}</span>
          <span v-if="preview.selected" class="ml-2">选中节点：<span class="font-mono">{{ preview.selected }}</span></span>
        </div>
        <div class="max-h-72 overflow-auto">
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b text-left text-muted-foreground">
                <th class="px-3 py-2">节点</th>
                <th class="px-3 py-2">IP</th>
                <th class="px-3 py-2">地域</th>
                <th class="px-3 py-2">候选</th>
                <th class="px-3 py-2">得分</th>
                <th class="px-3 py-2">排除原因</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="nd in preview.nodes" :key="nd.node_agent_id" class="border-b last:border-0">
                <td class="px-3 py-2 font-mono">{{ nd.node_agent_id }}</td>
                <td class="px-3 py-2 font-mono">{{ nd.ip }}</td>
                <td class="px-3 py-2">{{ nd.location || '-' }}</td>
                <td class="px-3 py-2">
                  <span :class="nd.eligible ? 'text-green-600' : 'text-red-500'">{{ nd.eligible ? '✓' : '✗' }}</span>
                </td>
                <td class="px-3 py-2 font-mono">{{ nd.score ? nd.score.toFixed(2) : '-' }}</td>
                <td class="px-3 py-2 text-muted-foreground">{{ (nd.reasons || []).join('；') || '-' }}</td>
              </tr>
              <tr v-if="!preview.nodes.length">
                <td colspan="6" class="px-3 py-8 text-center text-muted-foreground">无候选节点</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- 节点历史曲线弹层 -->
    <div v-if="historyNode" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="historyNode = null">
      <div class="w-[720px] rounded-lg border bg-background p-4">
        <div class="mb-3 flex items-center justify-between">
          <span class="text-sm font-medium">节点 {{ historyNode.node_id }} 资源曲线（近 1h）</span>
          <button class="text-sm text-muted-foreground hover:underline" @click="historyNode = null">关闭</button>
        </div>
        <div v-if="historySamples.length">
          <div class="mb-1 text-xs text-muted-foreground">CPU 占用（核）</div>
          <svg :viewBox="'0 0 ' + W + ' ' + H" class="w-full rounded border">
            <polyline :points="cpuPoints" fill="none" stroke="#22c55e" stroke-width="1.5" />
          </svg>
          <div class="mb-1 mt-3 text-xs text-muted-foreground">内存占用（GB）</div>
          <svg :viewBox="'0 0 ' + W + ' ' + H" class="w-full rounded border">
            <polyline :points="memPoints" fill="none" stroke="#3b82f6" stroke-width="1.5" />
          </svg>
        </div>
        <div v-else class="py-8 text-center text-sm text-muted-foreground">暂无采样数据（node_agent 心跳上报后生成）</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, computed } from 'vue'

import {
  CACHE_STATUS_LABELS,
  EVENT_LABELS,
  formatBytes,
  formatMilliCpu,
  observeCache,
  observeEvents,
  observeNodeHistory,
  observeNodes,
  observeQueue,
  observeStats,
  previewSchedule,
  type NodeCacheItem,
  type NodeOverview,
  type PreviewResult,
  type QueueItem,
  type ResourceSample,
  type SchedulerEvent,
  type SchedulerStats,
} from '@/api/observe'

const nodes = ref<NodeOverview[]>([])
const queue = ref<QueueItem[]>([])
const cache = ref<NodeCacheItem[]>([])
const events = ref<SchedulerEvent[]>([])
const stats = ref<SchedulerStats>({ attempts: {}, queue_len: 0, event_count: 0 })
const eventFilter = ref('')
const error = ref('')

// P2-B：集群缓存磁盘占用汇总（可用 / 下载中）
const cacheTotalAvailable = computed(() =>
  cache.value.filter((c) => c.status === 'available').reduce((s, c) => s + (c.size_bytes ?? 0), 0),
)
const cacheTotalDownloading = computed(() =>
  cache.value.filter((c) => c.status === 'downloading').reduce((s, c) => s + (c.size_bytes ?? 0), 0),
)

// 试调度
const pv = ref({ game_id: '', region: '', cpu: 0, memGb: 0 })
const preview = ref<PreviewResult | null>(null)

// 历史曲线
const historyNode = ref<NodeOverview | null>(null)
const historySamples = ref<ResourceSample[]>([])
const W = 640
const H = 120

let timer: number | undefined

function fmtTime(s?: string) {
  if (!s) return '-'
  const d = new Date(s)
  return d.toLocaleTimeString('zh-CN', { hour12: false }) + '.' + String(d.getMilliseconds()).padStart(3, '0')
}

function healthClass(h: string) {
  const map: Record<string, string> = {
    healthy: 'text-green-600',
    degraded: 'text-amber-600',
    unhealthy: 'text-red-600',
    unknown: 'text-muted-foreground',
    no_agent: 'text-muted-foreground',
  }
  return map[h] ?? 'text-muted-foreground'
}

function pressureClass(p: string) {
  const map: Record<string, string> = { Normal: 'text-green-600', Warning: 'text-amber-600', Critical: 'text-red-600' }
  return map[p] ?? ''
}

function eventClass(t: string) {
  if (t.includes('fail') || t.includes('timeout')) return 'text-red-600'
  if (t.includes('queued')) return 'text-amber-600'
  return 'text-green-600'
}

function cacheStatusClass(s: string) {
  const map: Record<string, string> = {
    available: 'text-green-600',
    downloading: 'text-amber-600',
    missing: 'text-muted-foreground',
  }
  return map[s] ?? 'text-red-600'
}

function outcomeClass(o: string) {
  return o === 'scheduled' ? 'text-green-600' : o === 'queued' ? 'text-amber-600' : 'text-red-600'
}

// 折线点
function toPoints(fn: (s: ResourceSample) => number): string {
  const n = historySamples.value.length
  if (!n) return ''
  const vals = historySamples.value.map(fn)
  const max = Math.max(...vals, 1)
  return vals
    .map((v, i) => `${((i / (n - 1)) * W).toFixed(1)},${(H - 8 - (v / max) * (H - 16)).toFixed(1)}`)
    .join(' ')
}
const cpuPoints = computed(() => toPoints((s) => s.cpu_used_milli / 1000))
const memPoints = computed(() => toPoints((s) => s.memory_used_bytes / 1e9))

async function loadAll() {
  try {
    const [n, q, st, c] = await Promise.all([observeNodes(), observeQueue(), observeStats(), observeCache()])
    nodes.value = n
    queue.value = q
    stats.value = st
    cache.value = c
    error.value = ''
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载失败（controller 是否已启动？）'
  }
}

async function loadEvents() {
  try {
    // 默认查最近 24h 持久化历史（重启后仍可回溯，S30）
    events.value = await observeEvents(100, eventFilter.value || undefined, 24)
  } catch {
    /* 忽略 */
  }
}

async function showHistory(n: NodeOverview) {
  historyNode.value = n
  try {
    historySamples.value = await observeNodeHistory(n.node_id, '1h')
  } catch (e: any) {
    historySamples.value = []
    error.value = e.response?.data?.error ?? '历史加载失败'
  }
}

async function onPreview() {
  error.value = ''
  try {
    const resources =
      pv.value.cpu > 0 || pv.value.memGb > 0
        ? {
            cpu_milli: pv.value.cpu > 0 ? Math.round(pv.value.cpu * 1000) : undefined,
            memory_bytes: pv.value.memGb > 0 ? Math.round(pv.value.memGb * 1e9) : undefined,
          }
        : undefined
    preview.value = await previewSchedule({
      game_id: pv.value.game_id,
      region: pv.value.region || undefined,
      resources,
    })
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '试调度失败'
  }
}

onMounted(() => {
  loadAll()
  loadEvents()
  timer = window.setInterval(() => {
    loadAll()
    loadEvents()
  }, 5000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

import { http } from './http'

// ---- 调度观测（/api/admin/observe/*，经 platform-service 转发 controller，admin 鉴权） ----

export interface NodeOverview {
  node_id: string
  node_agent_id?: string
  ip: string
  location: string
  enabled: boolean
  health: string // unknown/healthy/degraded/unhealthy/no_agent
  alive: boolean
  pressure: string // Normal/Warning/Critical
  cpu_capacity_milli: number
  cpu_allocatable_milli: number
  cpu_used_milli: number
  cpu_reserved_milli: number
  mem_capacity_bytes: number
  mem_allocatable_bytes: number
  mem_used_bytes: number
  mem_reserved_bytes: number
  disk_allocatable_bytes: number
  bandwidth_ratio: number
}

export interface ResourceSample {
  id: number
  node_id: string
  sampled_at: string
  cpu_used_milli: number
  memory_used_bytes: number
  disk_used_bytes: number
  net_rx_bps: number
  net_tx_bps: number
}

export interface NodeCacheItem {
  node_agent_id: string
  node_id?: string
  game_id: string
  branch: string
  status: string // available/downloading/removed/unavailable/missing
  build_id: string
  download_progress: number
  size_bytes?: number // P2-B：缓存内容实测字节数（0/undefined = 未知）
  last_error?: string // P4：最近一次失败原因（空/undefined = 无失败或成功）
}

export interface QueueItem {
  instance_id: string
  game_id: string
  priority: number
  reason: string
  attempts: number
  queued_at: string
  wake_at: string
  wait_seconds: number
  remaining_seconds: number
}

export interface SchedulerEvent {
  type: string
  occurred_at: string
  instance_id?: string
  node_agent_id?: string
  detail?: string
}

export interface SchedulerStats {
  attempts: Record<string, number>
  queue_len: number
  event_count: number
}

export interface PreviewNode {
  node_agent_id: string
  node_id: string
  ip: string
  location: string
  eligible: boolean
  reasons?: string[]
  score: number
}

export interface PreviewResult {
  outcome: string // scheduled/queued/failed
  reason: string
  selected?: string
  nodes: PreviewNode[]
}

export async function observeNodes(): Promise<NodeOverview[]> {
  const resp = await http.get('/admin/observe/nodes')
  return resp.data.nodes
}

export async function observeNodeHistory(nodeId: string, window = '1h'): Promise<ResourceSample[]> {
  const resp = await http.get('/admin/observe/nodes/' + nodeId + '/history', { params: { window } })
  return resp.data.samples
}

export async function observeCache(): Promise<NodeCacheItem[]> {
  const resp = await http.get('/admin/observe/cache')
  return resp.data.cache
}

export async function observeQueue(): Promise<QueueItem[]> {
  const resp = await http.get('/admin/observe/queue')
  return resp.data.queue
}

export async function observeEvents(limit = 100, type?: string, hours?: number): Promise<SchedulerEvent[]> {
  const resp = await http.get('/admin/observe/events', {
    params: { limit, ...(type ? { type } : {}), ...(hours ? { hours } : {}) },
  })
  return resp.data.events
}

export async function observeStats(): Promise<SchedulerStats> {
  const resp = await http.get('/admin/observe/scheduler/stats')
  return resp.data
}

export async function previewSchedule(req: {
  game_id: string
  game_build_id?: string
  region?: string
  resources?: { cpu_milli?: number; memory_bytes?: number }
}): Promise<PreviewResult> {
  const resp = await http.post('/admin/observe/scheduler/preview', req)
  return resp.data
}

// ---- 展示辅助 ----

export const EVENT_LABELS: Record<string, string> = {
  instance_scheduled: '调度成功',
  instance_queued: '排队',
  instance_schedule_failed: '调度失败',
  instance_queue_timeout: '排队超时',
  instance_queued_cancelled: '取消排队',
  instance_stopped: '实例停止',
  instance_failed: '实例失败',
  node_pressure_changed: '节点压力变化',
  node_health_changed: '节点健康变化',
  reservation_released: '预留释放',
  cache_ready: '缓存就绪',
}

export const CACHE_STATUS_LABELS: Record<string, string> = {
  available: '可用',
  downloading: '下载中',
  removed: '已移除',
  unavailable: '不可用',
  missing: '缺失',
}

export function formatBytes(n: number): string {
  if (!n) return '0'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return n.toFixed(1) + units[i]
}

export function formatMilliCpu(n: number): string {
  if (!n) return '0'
  return (n / 1000).toFixed(1) + ' 核'
}

import { http } from './http'

export interface UserInstance {
  order_id: string
  instance_id: string
  game_id: string
  status: string
  node_agent?: string
  connect_address?: string
}

export async function myInstances(gameId?: string): Promise<UserInstance[]> {
  const resp = await http.get('/me/instances', { params: gameId ? { game_id: gameId } : {} })
  return resp.data.instances
}

export async function allInstances(gameId?: string): Promise<UserInstance[]> {
  const resp = await http.get('/instances', { params: gameId ? { game_id: gameId } : {} })
  return resp.data.instances
}

// 启动/停止订单关联的实例（POST /api/orders/:id/instance/start|stop）
export async function startOrderInstance(orderId: string): Promise<void> {
  await http.post('/orders/' + orderId + '/instance/start')
}

export async function stopOrderInstance(orderId: string): Promise<void> {
  await http.post('/orders/' + orderId + '/instance/stop')
}

// 实例状态 → 可执行动作
export function instanceActions(status: string | undefined): { label: string; action: 'start' | 'stop' } | null {
  const s = (status ?? '').trim().toLowerCase()
  // stopped/failed/状态未知 → 允许开服（start 幂等，controller 会拒绝非法状态并报错）
  if (s === '' || s === 'stopped' || s === 'failed' || s === 'unknown' || s === 'unavailable') {
    return { label: '开服', action: 'start' }
  }
  if (s === 'running') {
    return { label: '停服', action: 'stop' }
  }
  return null // 中间态（pending/scheduling/starting/...）不可操作
}

// 状态展示文案（含中间态中文提示，便于诊断）
export function statusText(status: string | undefined): string {
  const s = (status ?? '').trim().toLowerCase()
  const map: Record<string, string> = {
    pending: '等待调度',
    scheduling: '调度中',
    preparing_build: '准备构建',
    restoring_snapshot: '还原快照',
    starting: '启动中',
    running: '运行中',
    stopping: '停止中',
    cleaning: '清理中',
    stopped: '已停止',
    failed: '失败',
    unknown: '状态未知（controller 不可达）',
  }
  return map[s] ?? (status || '未知')
}
import { http } from './http'

export interface UserInstance {
  order_id: string
  instance_id: string
  game_id: string
  /** 实例使用的资产版本（game_build_id，controller 创建/启动时解析落库；未知时为空） */
  game_build_id?: string
  status: string
  node_agent?: string
  connect_address?: string
  fail_reason?: string
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

// 更新订单关联实例的配置（PUT /api/me/instances/:orderId/config；重启生效）
export async function updateInstanceConfig(orderId: string, config: Record<string, string>): Promise<void> {
  await http.put('/me/instances/' + orderId + '/config', { config })
}

// 状态归一化：统一小写并去首尾空白
function normStatus(status: string | undefined): string {
  return (status ?? '').trim().toLowerCase()
}

// 启动是否允许（对齐后端 StartGameInstance 状态守卫：仅 stopped/failed 可启动）。
// 空 / unknown / unavailable：controller 不可达或状态未知时的兜底，保留启动入口
//（操作会被后端拒绝并报错，不阻断页面其余功能）。
export function canStart(status: string | undefined): boolean {
  const s = normStatus(status)
  return s === '' || s === 'stopped' || s === 'failed' || s === 'unknown' || s === 'unavailable'
}

// 停止是否允许（对齐后端 StopGameInstance 状态守卫：仅 running/failed 可停止）。
// failed 状态允许「停止重试」：停止可能失败而容器仍残留在 node_agent 上，
// 状态回落 failed 后仍可再次发起停止，二次清理残留容器。
export function canStop(status: string | undefined): boolean {
  const s = normStatus(status)
  return s === 'running' || s === 'failed'
}

// 按钮禁用原因文案（title 提示）；当前状态可操作时返回空串。
export function actionDisabledReason(status: string | undefined, action: 'start' | 'stop'): string {
  const s = normStatus(status)
  if (isTransitionalStatus(s)) return '实例处于中间态，请等待当前流程完成'
  if (action === 'start') {
    if (s === 'running') return '实例运行中，请先停止'
    return '当前状态不可启动（仅 stopped / failed 可启动）'
  }
  if (s === 'stopped') return '实例已停止，无需停止'
  if (s === 'unknown' || s === 'unavailable') return '无法确认实例状态（controller 不可达）'
  return '当前状态不可停止（仅 running / failed 可停止）'
}

// 状态展示文案（含中间态中文提示，便于诊断）
export function statusText(status: string | undefined): string {
  const s = (status ?? '').trim().toLowerCase()
  const map: Record<string, string> = {
    pending: '等待调度',
    scheduling: '调度中',
    queued: '排队中',
    cache_warming: '缓存预热',
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

// 是否中间态（启动后轮询等待的目标：离开中间态即停）
export function isTransitionalStatus(status: string | undefined): boolean {
  const s = (status ?? '').trim().toLowerCase()
  return ['pending', 'scheduling', 'queued', 'cache_warming', 'preparing_build', 'restoring_snapshot', 'starting', 'stopping', 'cleaning'].includes(s)
}
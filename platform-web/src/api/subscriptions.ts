import { http } from './http'
import type { ServerPlan } from './admin'

// ---------- M9/M12：订阅（用户侧） ----------

export type SubscriptionStatus = 'active' | 'expired' | 'cancelled' | 'suspended'

export interface Subscription {
  ID: string
  UserID: string
  PlanID: string
  Status: SubscriptionStatus
  ExpiresAt?: string
  BasketSnapshot: Array<{ game_id: string; config?: Record<string, string> }>
  MaxInstances: number // 实例数量上限（0 = 不限）
  CreateTime: string
  UpdateTime: string
}

export interface SubscriptionInstance {
  ID: string
  GameID: string
  NodeAgentID?: string
  Status: string
  GameBuildId: string
  FailReason?: string
  SubscriptionID?: string
}

// B-04/P1-1：实例运行时统计（健康 + 在线人数，controller 探针心跳数据）
export interface InstanceRuntime {
  instance_id: string
  running: boolean
  player_count: number
  max_players: number
  healthy: boolean
  probe_mode: string // "a2s" | "script" | "unknown"
  probe_error?: string
  sampled_at?: string
}

export async function listMySubscriptions(): Promise<Subscription[]> {
  const resp = await http.get('/me/subscriptions')
  return resp.data.subscriptions
}

export async function purchaseSubscription(planId: string): Promise<Subscription> {
  const resp = await http.post('/me/subscriptions', { plan_id: planId })
  return resp.data
}

export async function renewSubscription(id: string): Promise<Subscription> {
  const resp = await http.post('/me/subscriptions/' + id + '/renew')
  return resp.data
}

export async function cancelSubscription(id: string): Promise<Subscription> {
  const resp = await http.post('/me/subscriptions/' + id + '/cancel')
  return resp.data
}

// 在售套餐（用户购买入口）
export async function listEnabledPlans(): Promise<ServerPlan[]> {
  const resp = await http.get('/me/plans')
  return resp.data.plans
}

// 订阅内实例
export async function listSubscriptionInstances(id: string): Promise<SubscriptionInstance[]> {
  const resp = await http.get('/me/subscriptions/' + id + '/instances')
  return resp.data.instances
}

export async function createSubscriptionInstance(id: string, gameId: string, config?: Record<string, string>): Promise<SubscriptionInstance> {
  const resp = await http.post('/me/subscriptions/' + id + '/instances', {
    game_id: gameId,
    config: config && Object.keys(config).length ? config : undefined,
  })
  return resp.data
}

export async function startSubscriptionInstance(id: string, instanceId: string): Promise<void> {
  await http.post('/me/subscriptions/' + id + '/instances/' + instanceId + '/start')
}

export async function stopSubscriptionInstance(id: string, instanceId: string): Promise<void> {
  await http.post('/me/subscriptions/' + id + '/instances/' + instanceId + '/stop')
}

// B-04/P1-1：实例运行时统计（健康 + 在线人数）
export async function getSubscriptionInstanceRuntime(id: string, instanceId: string): Promise<InstanceRuntime> {
  const resp = await http.get('/me/subscriptions/' + id + '/instances/' + instanceId + '/runtime')
  return resp.data
}

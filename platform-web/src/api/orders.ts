import { http } from './http'

export interface Order {
  ID: string
  UserID: string
  GameID: string
  InstanceID: string
  Amount: number
  Status: number // 0=created 1=paid 2=cancelled 3=refunded
  CreateTime: string
  UpdateTime: string
  Config?: Record<string, string>
}

export async function listOrders(userId?: string, gameId?: string): Promise<Order[]> {
  const params: Record<string, string> = {}
  if (userId) params.user_id = userId
  if (gameId) params.game_id = gameId
  const resp = await http.get('/orders', { params })
  return resp.data.orders
}

export async function createOrder(data: { game_id: string; amount: number; config?: Record<string, string> }): Promise<Order> {
  const resp = await http.post('/orders', data)
  return resp.data
}

export async function payOrder(id: string): Promise<Order> {
  const resp = await http.post('/orders/' + id + '/pay')
  return resp.data
}

// 管理员免支付直接开服（POST /api/orders/:id/provision）
export async function provisionOrder(id: string): Promise<Order> {
  const resp = await http.post('/orders/' + id + '/provision')
  return resp.data
}

// ---------- 配置 schema（下单表单数据源） ----------

// 与 controller/adapter_schema.go / asset_service AdapterSchema 契约一致
export interface ConfigSetting {
  key: string
  type: 'string' | 'int' | 'bool' | 'enum'
  control: 'player' | 'platform' | 'locked'
  apply: 'always' | 'on_first_start'
  render: string
  default?: string
  min?: number
  max?: number
  enum_values?: string[]
  secret?: boolean
  label_key?: string
  description_key?: string
  group_key?: string
}

export interface ConfigSchema {
  adapter_id: string
  game_id: string
  settings: ConfigSetting[]
  render_file?: string
  i18n?: {
    fallback?: string
    en?: Record<string, string>
    zh?: Record<string, string>
    [lang: string]: any
  }
}

// 获取游戏配置 schema（GET /api/games/:id/config-schema，platform-service 透传）
export async function getConfigSchema(gameId: string): Promise<ConfigSchema | null> {
  try {
    const resp = await http.get('/games/' + gameId + '/config-schema')
    const schemaJson = resp.data?.schema_json
    if (!schemaJson) return null
    return JSON.parse(schemaJson)
  } catch (e: any) {
    // 404（游戏无 schema）视为无配置
    if (e?.response?.status === 404) return null
    throw e
  }
}

// 按当前语言解析 i18n key（查找链：当前语言 → fallback → key 原文）
export function i18nText(schema: ConfigSchema | null, key: string | undefined, fallback: string): string {
  if (!key || !schema?.i18n) return fallback
  const i18n = schema.i18n
  const lang: string = (navigator.language || 'zh').toLowerCase().startsWith('zh') ? 'zh' : 'en'
  const table = i18n[lang] ?? i18n[i18n.fallback ?? 'en']
  if (table && table[key]) return table[key]
  if (lang !== (i18n.fallback ?? 'en')) {
    const fb = i18n[i18n.fallback ?? 'en']
    if (fb && fb[key]) return fb[key]
  }
  return fallback
}

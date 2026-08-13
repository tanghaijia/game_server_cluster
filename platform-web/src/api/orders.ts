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
}

export async function listOrders(userId?: string): Promise<Order[]> {
  const resp = await http.get('/orders', { params: userId ? { user_id: userId } : {} })
  return resp.data.orders
}

export async function createOrder(data: { game_id: string; amount: number }): Promise<Order> {
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

import { http } from './http'

export interface User {
  ID: string
  Username: string
  Role: number
  Status: number
  CreateTime: string
  UpdateTime: string
}

export async function listUsers(): Promise<User[]> {
  const resp = await http.get('/users')
  return resp.data.users
}

export async function me(): Promise<User> {
  const resp = await http.get('/users/me')
  return resp.data
}

export async function setUserRole(id: string, role: number): Promise<User> {
  const resp = await http.patch('/users/' + id + '/role', { role })
  return resp.data
}

export async function setUserStatus(id: string, status: number): Promise<User> {
  const resp = await http.patch('/users/' + id + '/status', { status })
  return resp.data
}

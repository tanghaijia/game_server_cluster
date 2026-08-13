import { http } from './http'

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  access_token: string
  refresh_token?: string
  user?: {
    id: string
    username: string
    role: number
  }
}

export async function login(data: LoginRequest): Promise<LoginResponse> {
  const resp = await http.post<LoginResponse>('/auth/login', data)
  return resp.data
}

// 注册（platform-service POST /api/users，开放接口）
export async function register(data: LoginRequest): Promise<void> {
  await http.post('/users', data)
}

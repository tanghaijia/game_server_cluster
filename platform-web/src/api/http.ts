import axios from 'axios'

// 全局 axios 实例：统一 baseURL、JWT 附加、401 处理（ADR-0004）
export const http = axios.create({
  baseURL: import.meta.env.VITE_API_BASE ?? '/api',
  timeout: 10000,
})

// 请求拦截：附加 access token
http.interceptors.request.use((config) => {
  const token = localStorage.getItem('access_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 响应拦截：401 处理（骨架：清 token 跳登录页；后续接入 refresh token 流程）
http.interceptors.response.use(
  (resp) => resp,
  async (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('access_token')
      if (window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  },
)

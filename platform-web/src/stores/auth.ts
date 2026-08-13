import { defineStore } from 'pinia'

export interface UserInfo {
  id: string
  username: string
  role: number // 0=user 1=admin
}

function loadUser(): UserInfo | null {
  try {
    return JSON.parse(localStorage.getItem('user') ?? 'null')
  } catch {
    return null
  }
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('access_token') ?? '',
    user: loadUser() as UserInfo | null,
  }),
  getters: {
    isAuthenticated: (state) => !!state.token,
    isAdmin: (state) => state.user?.role === 1,
  },
  actions: {
    setAuth(token: string, user: UserInfo) {
      this.token = token
      this.user = user
      localStorage.setItem('access_token', token)
      localStorage.setItem('user', JSON.stringify(user))
    },
    logout() {
      this.token = ''
      this.user = null
      localStorage.removeItem('access_token')
      localStorage.removeItem('user')
    },
  },
})

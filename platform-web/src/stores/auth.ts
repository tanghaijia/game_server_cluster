import { defineStore } from 'pinia'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('access_token') ?? '',
  }),
  getters: {
    isAuthenticated: (state) => !!state.token,
  },
  actions: {
    setToken(token: string) {
      this.token = token
      localStorage.setItem('access_token', token)
    },
    logout() {
      this.token = ''
      localStorage.removeItem('access_token')
    },
  },
})

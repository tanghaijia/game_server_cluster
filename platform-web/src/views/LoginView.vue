<template>
  <div class="flex min-h-screen items-center justify-center bg-muted">
    <div class="w-full max-w-sm rounded-lg border bg-card p-8 shadow-sm">
      <h1 class="mb-1 text-center text-2xl font-semibold">Platform Console</h1>
      <p class="mb-6 text-center text-sm text-muted-foreground">
        {{ mode === 'login' ? '登录平台' : '创建新账号' }}
      </p>
      <form class="space-y-4" @submit.prevent="onSubmit">
        <div>
          <label class="mb-1 block text-sm font-medium">用户名</label>
          <input
            v-model="form.username"
            type="text"
            autocomplete="username"
            class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2"
          />
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium">密码</label>
          <input
            v-model="form.password"
            type="password"
            :autocomplete="mode === 'login' ? 'current-password' : 'new-password'"
            class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2"
          />
        </div>
        <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
        <button
          type="submit"
          :disabled="loading"
          class="w-full rounded-md bg-primary py-2 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
        >
          {{ loading ? '请稍候...' : (mode === 'login' ? '登录' : '注册并登录') }}
        </button>
      </form>
      <p class="mt-4 text-center text-sm text-muted-foreground">
        <button class="hover:underline" type="button" @click="toggleMode">
          {{ mode === 'login' ? '没有账号？去注册' : '已有账号？去登录' }}
        </button>
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

import { login, register } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

const mode = ref<'login' | 'register'>('login')
const form = reactive({ username: '', password: '' })
const loading = ref(false)
const error = ref('')

function toggleMode() {
  mode.value = mode.value === 'login' ? 'register' : 'login'
  error.value = ''
}

async function onSubmit() {
  if (!form.username || !form.password) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    if (mode.value === 'register') {
      await register(form)
    }
    const resp = await login(form)
    auth.setAuth(resp.access_token, resp.user)
    router.push({ name: 'dashboard' })
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败'
  } finally {
    loading.value = false
  }
}
</script>

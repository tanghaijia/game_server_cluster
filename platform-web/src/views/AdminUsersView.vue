<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">用户管理</h1>
      <p class="text-sm text-muted-foreground">仅管理员可见。可调整角色与启用状态（不能操作自己）。</p>
    </div>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">ID</th>
            <th class="px-4 py-3">用户名</th>
            <th class="px-4 py-3">角色</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.ID" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ u.ID }}</td>
            <td class="px-4 py-3">{{ u.Username }}</td>
            <td class="px-4 py-3">
              <span class="rounded px-2 py-0.5 text-xs" :class="u.Role === 1 ? 'bg-primary text-primary-foreground' : 'bg-muted'">
                {{ u.Role === 1 ? '管理员' : '普通用户' }}
              </span>
            </td>
            <td class="px-4 py-3">{{ u.Status === 0 ? '正常' : '已禁用' }}</td>
            <td class="px-4 py-3 space-x-2">
              <button class="rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="onToggleRole(u)">
                {{ u.Role === 1 ? '降为普通用户' : '设为管理员' }}
              </button>
              <button class="rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="onToggleStatus(u)">
                {{ u.Status === 0 ? '禁用' : '启用' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { listUsers, setUserRole, setUserStatus, type User } from '@/api/users'

const users = ref<User[]>([])
const error = ref('')

async function load() {
  users.value = await listUsers()
}

async function onToggleRole(u: User) {
  try {
    await setUserRole(u.ID, u.Role === 1 ? 0 : 1)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败'
  }
}

async function onToggleStatus(u: User) {
  try {
    await setUserStatus(u.ID, u.Status === 0 ? 1 : 0)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败'
  }
}

onMounted(load)
</script>

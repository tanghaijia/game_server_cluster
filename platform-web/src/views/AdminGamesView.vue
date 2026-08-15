<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">游戏管理</h1>
      <p class="text-sm text-muted-foreground">游戏的增删改查（写操作会同步到 asset_service）。</p>
    </div>

    <!-- 新增/编辑表单 -->
    <form class="flex max-w-2xl items-end gap-3 rounded-lg border p-4" @submit.prevent="onSubmit">
      <div class="flex-1">
        <label class="mb-1 block text-sm font-medium">游戏名称</label>
        <input v-model="form.name" type="text" placeholder="如 7 Days to Die" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div class="w-44">
        <label class="mb-1 block text-sm font-medium">App ID</label>
        <input v-model="form.appId" type="text" placeholder="如 343050" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">
        {{ editingId ? '保存' : '新增' }}
      </button>
      <button v-if="editingId" type="button" class="rounded-md border px-4 py-2 text-sm hover:bg-muted" @click="resetForm">取消</button>
    </form>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">ID</th>
            <th class="px-4 py-3">名称</th>
            <th class="px-4 py-3">App ID</th>
            <th class="px-4 py-3">容器配置</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="g in games" :key="g.ID" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ g.ID }}</td>
            <td class="px-4 py-3">{{ g.Name }}</td>
            <td class="px-4 py-3">{{ g.AppId }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ g.ContainerConfigID || '-' }}</td>
            <td class="px-4 py-3 space-x-2">
              <button class="rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="startEdit(g)">编辑</button>
              <button class="rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="openBuilds(g.ID)">构建</button>
              <button class="rounded-md border px-3 py-1 text-xs text-red-500 hover:bg-muted" @click="onDelete(g.ID)">删除</button>
            </td>
          </tr>
          <tr v-if="!games.length">
            <td colspan="5" class="px-4 py-8 text-center text-muted-foreground">暂无游戏（controller 不可达或还没有数据）</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import { useRouter } from 'vue-router'

import { createGame, deleteGame, listGames, updateGame, type Game } from '@/api/admin'

const games = ref<Game[]>([])
const editingId = ref('')
const error = ref('')
const form = reactive({ name: '', appId: '' })

async function load() {
  error.value = ''
  try {
    games.value = await listGames()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载失败（controller 是否已启动？）'
  }
}

function resetForm() {
  editingId.value = ''
  form.name = ''
  form.appId = ''
}

function startEdit(g: Game) {
  editingId.value = g.ID
  form.name = g.Name
  form.appId = g.AppId
}

async function onSubmit() {
  error.value = ''
  if (!form.name) {
    error.value = '请填写游戏名称'
    return
  }
  try {
    if (editingId.value) {
      await updateGame(editingId.value, { name: form.name, app_id: form.appId })
    } else {
      await createGame({ name: form.name, app_id: form.appId })
    }
    resetForm()
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败'
  }
}

const router = useRouter()

function openBuilds(id: string) {
  router.push({ name: 'admin-game-builds', params: { gameId: id } })
}

async function onDelete(id: string) {
  if (!confirm('确认删除游戏 ' + id + '？')) return
  error.value = ''
  try {
    await deleteGame(id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '删除失败'
  }
}

onMounted(load)
</script>

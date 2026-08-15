<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">选择游戏</h1>
      <p class="text-sm text-muted-foreground">选择一款游戏，创建和管理你的服务器。</p>
    </div>

    <div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
      <button
        v-for="g in games"
        :key="g.ID"
        class="rounded-lg border bg-card p-6 text-left transition hover:shadow-md"
        :style="g.profile?.accent_color ? { borderColor: g.profile.accent_color } : {}"
        @click="enter(g.ID)"
      >
        <div class="flex items-center gap-3">
          <!-- 图标：加载失败时显示游戏色首字母占位 -->
          <span
            v-if="failedIcons.has(g.ID) || !g.profile?.icon_url"
            class="flex h-12 w-12 items-center justify-center rounded text-lg font-bold text-white"
            :style="{ backgroundColor: g.profile?.accent_color || '#6366f1' }"
          >
            {{ (g.profile?.display_name || g.Name).slice(0, 1).toUpperCase() }}
          </span>
          <img
            v-else
            :src="g.profile.icon_url"
            class="h-12 w-12 rounded object-contain"
            :alt="g.Name"
            @error="markFailed(g.ID)"
          />
          <div>
            <div class="text-lg font-semibold" :style="g.profile?.accent_color ? { color: g.profile.accent_color } : {}">
              {{ g.profile?.display_name || g.Name }}
            </div>
            <div class="text-xs text-muted-foreground">{{ g.Name }} ({{ g.AppId }})</div>
          </div>
        </div>
        <p v-if="g.profile?.description" class="mt-3 text-sm text-muted-foreground">{{ g.profile.description }}</p>
        <div class="mt-4 flex items-center justify-between">
          <span class="text-sm font-medium" :style="g.profile?.accent_color ? { color: g.profile.accent_color } : {}">进入管理 →</span>
          <span v-if="isAdmin" class="rounded border px-2 py-0.5 text-xs hover:bg-muted" @click.stop="openSettings(g.ID)">设置</span>
        </div>
      </button>

      <div v-if="!games.length" class="col-span-full rounded-lg border p-8 text-center text-muted-foreground">
        暂无可用游戏（管理员需先在「游戏管理」配置游戏资料）
      </div>
    </div>

    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { listGames, type GameView } from '@/api/games'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const games = ref<GameView[]>([])
const failedIcons = ref(new Set<string>())
const error = ref('')

const isAdmin = () => auth.isAdmin

function enter(gameId: string) {
  router.push({ name: 'game-servers', params: { gameId } })
}

function openSettings(gameId: string) {
  router.push({ name: 'game-settings', params: { gameId } })
}

function markFailed(id: string) {
  failedIcons.value.add(id)
}

onMounted(async () => {
  error.value = ''
  try {
    games.value = await listGames()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载游戏列表失败'
  }
})
</script>

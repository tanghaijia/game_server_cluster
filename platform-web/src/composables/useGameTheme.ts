import { watch } from 'vue'
import { useRoute } from 'vue-router'

import { getGame } from '@/api/games'

// 默认主色（与 src/assets/main.css 的 --primary 一致）
const DEFAULT_PRIMARY = 'oklch(0.585 0.233 277.117)'

export function applyGameColor(color?: string) {
  const el = document.documentElement
  if (color && color.trim()) {
    el.style.setProperty('--primary', color.trim())
  } else {
    el.style.setProperty('--primary', DEFAULT_PRIMARY)
  }
}

/**
 * 路由级主题：进入 /games/:gameId/* 时应用该游戏的 accent_color（覆盖 --primary），
 * 离开游戏空间恢复默认色。Tailwind v4 @theme inline 变量实时生效，现有组件零改动。
 */
export function useGameTheme() {
  const route = useRoute()
  watch(
    () => [route.params.gameId, route.path] as const,
    async () => {
      const gameId = route.params.gameId as string | undefined
      if (gameId) {
        try {
          const game = await getGame(gameId)
          applyGameColor(game.profile?.accent_color)
          return
        } catch {
          /* 游戏加载失败时保持默认色 */
        }
      }
      applyGameColor()
    },
    { immediate: true },
  )
}

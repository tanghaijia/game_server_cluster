<template>
  <div class="mx-auto max-w-2xl space-y-6">
    <div>
      <RouterLink to="/" class="text-sm text-muted-foreground hover:underline">← 游戏列表</RouterLink>
      <h1 class="mt-1 text-2xl font-semibold">游戏设置 · {{ form.display_name || game?.Name }}</h1>
      <p class="text-sm text-muted-foreground">配置游戏展示资料与主题色（仅管理员）。</p>
    </div>

    <div v-if="game" class="flex items-center gap-4 rounded-lg border p-4" :style="previewStyle">
      <img v-if="form.icon_url" :src="form.icon_url" class="h-16 w-16 rounded object-contain" @error="hideImg" />
      <div>
        <div class="text-lg font-semibold">{{ form.display_name || game.Name }}</div>
        <div class="text-xs text-muted-foreground">{{ game.Name }} ({{ game.AppId }})</div>
      </div>
    </div>

    <form class="space-y-4 rounded-lg border p-6" @submit.prevent="onSave">
      <div>
        <label class="mb-1 block text-sm font-medium">展示名称</label>
        <input v-model="form.display_name" type="text" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>

      <div>
        <label class="mb-1 block text-sm font-medium">图标（≤1MB，png/jpg/svg）</label>
        <div class="flex items-center gap-3">
          <input ref="iconInput" type="file" accept="image/*" class="text-sm" @change="onIconPicked" />
          <span v-if="iconUploading" class="text-xs text-muted-foreground">上传中...</span>
        </div>
      </div>

      <div>
        <label class="mb-1 block text-sm font-medium">主题色</label>
        <div class="flex items-center gap-3">
          <input v-model="form.accent_color" type="color" class="h-9 w-14 cursor-pointer rounded border" />
          <input v-model="form.accent_color" type="text" class="w-32 rounded-md border px-3 py-1.5 text-sm font-mono outline-none focus:ring-2" />
          <div class="flex gap-1">
            <button
              v-for="c in palette"
              :key="c"
              type="button"
              class="h-7 w-7 rounded-full border"
              :style="{ backgroundColor: c }"
              :class="form.accent_color === c ? 'ring-2 ring-offset-1' : ''"
              @click="form.accent_color = c"
            ></button>
          </div>
        </div>
        <p v-if="contrastWarn" class="mt-1 text-xs text-yellow-600">提示：该颜色较浅，可能与深色文字对比度不足，建议换深色。</p>
      </div>

      <div>
        <label class="mb-1 block text-sm font-medium">描述</label>
        <textarea v-model="form.description" rows="3" class="w-full resize-none rounded-md border px-3 py-2 text-sm outline-none focus:ring-2"></textarea>
      </div>

      <div class="flex items-center justify-between">
        <label class="flex items-center gap-2 text-sm">
          <input v-model="form.enabled" type="checkbox" class="h-4 w-4" />
          对用户开放（在游戏列表显示）
        </label>
        <div class="flex items-center gap-2">
          <label class="text-sm">排序</label>
          <input v-model.number="form.sort_order" type="number" class="w-20 rounded-md border px-2 py-1 text-sm outline-none focus:ring-2" />
        </div>
      </div>

      <div class="flex justify-end">
        <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90" :disabled="saving">
          {{ saving ? '保存中...' : '保存' }}
        </button>
      </div>
    </form>

    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'

import { updateGameProfile, uploadGameIcon, type GameProfileInput } from '@/api/admin'
import { getGame, type GameView } from '@/api/games'

const route = useRoute()
const gameId = route.params.gameId as string

const game = ref<GameView | null>(null)
const iconInput = ref<HTMLInputElement>()
const iconUploading = ref(false)
const saving = ref(false)
const error = ref('')

const form = reactive({
  display_name: '',
  icon_url: '',
  accent_color: '#6366f1',
  description: '',
  enabled: true,
  sort_order: 0,
})

const palette = ['#E63946', '#7C3AED', '#2563EB', '#059669', '#EA580C', '#DB2777', '#0F766E', '#B45309']

// 相对亮度（WCAG 近似），用于对比度提示
function luminance(hex: string): number {
  const h = hex.replace('#', '')
  const c = [0, 2, 4].map((i) => {
    const v = parseInt(h.slice(i, i + 2), 16) / 255
    return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)
  })
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
}
const contrastWarn = computed(() => {
  if (!/^#[0-9a-fA-F]{6}$/.test(form.accent_color)) return false
  return luminance(form.accent_color) > 0.6
})

const previewStyle = computed(() => ({
  borderColor: /^#/.test(form.accent_color) ? form.accent_color : undefined,
  backgroundColor: /^#/.test(form.accent_color) ? form.accent_color + '14' : undefined,
}))

function hideImg(e: Event) {
  ;(e.target as HTMLImageElement).style.display = 'none'
}

async function onIconPicked(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  iconUploading.value = true
  error.value = ''
  try {
    const r = await uploadGameIcon(gameId, file)
    form.icon_url = r.icon_url
    if (iconInput.value) iconInput.value.value = ''
  } catch (err: any) {
    error.value = err.response?.data?.error ?? '图标上传失败'
  } finally {
    iconUploading.value = false
  }
}

async function onSave() {
  saving.value = true
  error.value = ''
  const payload: GameProfileInput = {
    display_name: form.display_name,
    icon_url: form.icon_url,
    accent_color: form.accent_color,
    description: form.description,
    enabled: form.enabled,
    sort_order: form.sort_order,
  }
  try {
    const r = await updateGameProfile(gameId, payload)
    form.display_name = r.display_name ?? form.display_name
    form.icon_url = r.icon_url ?? form.icon_url
    form.accent_color = r.accent_color ?? form.accent_color
    form.description = r.description ?? form.description
    form.enabled = r.enabled
    form.sort_order = r.sort_order
  } catch (err: any) {
    error.value = err.response?.data?.error ?? '保存失败'
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try {
    game.value = await getGame(gameId)
    const p = game.value.profile
    if (p) {
      form.display_name = p.display_name ?? ''
      form.icon_url = p.icon_url ?? ''
      form.accent_color = p.accent_color ?? '#6366f1'
      form.description = p.description ?? ''
      form.enabled = p.enabled
      form.sort_order = p.sort_order
    }
  } catch (err: any) {
    error.value = err.response?.data?.error ?? '加载游戏失败'
  }
})
</script>

<template>
  <div class="space-y-6">
    <div>
      <RouterLink to="/admin/games" class="text-sm text-muted-foreground hover:underline">← 游戏管理</RouterLink>
      <RouterLink :to="{ name: 'admin-game-builds', params: { gameId } }" class="ml-3 text-sm text-muted-foreground hover:underline">构建版本</RouterLink>
      <h1 class="mt-1 text-2xl font-semibold">平台配置 · {{ gameId }}</h1>
      <p class="text-sm text-muted-foreground">
        平台运营方配置（control=platform 项）：按游戏全局，玩家不可见；启动实例时与玩家配置合并（玩家配置覆盖）。
        修改后对下次启动的实例生效（已运行实例需重启）。
      </p>
    </div>

    <form v-if="loaded" class="max-w-xl space-y-4 rounded-lg border p-4" @submit.prevent="onSave">
      <p v-if="!platformSettings.length" class="text-sm text-muted-foreground">
        该游戏未注册配置 schema 或无平台可配置项（先到「构建版本」注册带 schema.json 的构建）。
      </p>
      <template v-for="s in platformSettings" :key="s.key">
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0 flex-1">
            <label class="block text-sm font-medium" :for="'pc-' + s.key">{{ label(s) }}</label>
            <p v-if="desc(s)" class="text-xs text-muted-foreground">{{ desc(s) }}</p>
            <p v-if="s.default !== undefined && config[s.key] === undefined" class="text-[11px] text-muted-foreground">
              默认：{{ s.default }}
            </p>
          </div>
          <div class="w-52 shrink-0">
            <input
              v-if="s.type === 'string'"
              :id="'pc-' + s.key"
              v-model="config[s.key]"
              :type="s.secret ? 'password' : 'text'"
              class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
            />
            <input
              v-else-if="s.type === 'int'"
              :id="'pc-' + s.key"
              v-model="config[s.key]"
              type="number"
              :min="s.min"
              :max="s.max"
              class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
            />
            <select
              v-else-if="s.type === 'enum'"
              :id="'pc-' + s.key"
              v-model="config[s.key]"
              class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
            >
              <option v-for="v in s.enum_values" :key="v" :value="v">{{ enumLabel(s, v) }}</option>
            </select>
            <input
              v-else-if="s.type === 'bool'"
              :id="'pc-' + s.key"
              v-model="config[s.key]"
              type="checkbox"
              class="mt-2 h-4 w-4 rounded border"
              true-value="true"
              false-value="false"
            />
          </div>
        </div>
      </template>

      <div v-if="platformSettings.length" class="flex items-center gap-3 border-t pt-4">
        <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">
          保存平台配置
        </button>
        <span v-if="saved" class="text-sm text-green-600">已保存（下次启动生效）</span>
      </div>
    </form>

    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'

import { getPlatformConfig, updatePlatformConfig } from '@/api/admin'
import { getConfigSchema, i18nText, type ConfigSchema, type ConfigSetting } from '@/api/orders'

const route = useRoute()
const gameId = route.params.gameId as string

const schema = ref<ConfigSchema | null>(null)
const loaded = ref(false)
const saved = ref(false)
const error = ref('')
const config = reactive<Record<string, string>>({})

// 平台可配置项（control=platform）
const platformSettings = computed<ConfigSetting[]>(
  () => schema.value?.settings.filter((s) => s.control === 'platform') ?? [],
)

const label = (s: ConfigSetting) => i18nText(schema.value, s.label_key, s.key)
const desc = (s: ConfigSetting) => (s.description_key ? i18nText(schema.value, s.description_key, '') : '')
const enumLabel = (s: ConfigSetting, v: string) => {
  const key = 'enum.' + s.key + '.' + v
  const t = i18nText(schema.value, key, v)
  return t === key ? v : t
}

async function load() {
  error.value = ''
  try {
    schema.value = await getConfigSchema(gameId)
    const pc = await getPlatformConfig(gameId)
    // 预填：平台配置值优先，缺失项用 schema 默认值
    const defaults: Record<string, string> = {}
    for (const s of platformSettings.value) {
      if (s.default !== undefined) defaults[s.key] = s.default
    }
    Object.assign(config, defaults, pc.Config ?? {})
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载失败'
  } finally {
    loaded.value = true
  }
}

async function onSave() {
  error.value = ''
  saved.value = false
  // 只提交已填写的 platform 项
  const cfg: Record<string, string> = {}
  for (const s of platformSettings.value) {
    const v = config[s.key]
    if (v !== undefined && v !== '') cfg[s.key] = v
  }
  try {
    await updatePlatformConfig(gameId, cfg)
    saved.value = true
    setTimeout(() => (saved.value = false), 3000)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '保存失败'
  }
}

onMounted(load)
</script>

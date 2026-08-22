<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <div class="flex items-center gap-2">
          <RouterLink to="/" class="text-sm text-muted-foreground hover:underline">← 游戏列表</RouterLink>
        </div>
        <h1 class="mt-1 text-2xl font-semibold">{{ gameName }} · 服务器</h1>
        <p class="text-sm text-muted-foreground">本游戏的实例列表。</p>
      </div>
      <button class="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50" :disabled="busy" @click="load">
        刷新
      </button>
    </div>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">订单</th>
            <th class="px-4 py-3">实例</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">节点</th>
            <th class="px-4 py-3">连接地址</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="inst in instances" :key="inst.instance_id" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ inst.order_id }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ inst.instance_id }}</td>
            <td class="px-4 py-3">
              <span
                class="rounded px-2 py-0.5 text-xs"
                :class="inst.status === 'failed' ? 'bg-red-100 text-red-700' : 'bg-muted'"
                :title="inst.fail_reason || undefined"
              >
                {{ statusText(inst.status) }}
              </span>
              <span v-if="inst.status === 'failed' && inst.fail_reason" class="mt-1 block max-w-[240px] truncate text-[11px] text-red-500" :title="inst.fail_reason">
                原因：{{ inst.fail_reason }}
              </span>
            </td>
            <td class="px-4 py-3 text-xs">{{ inst.node_agent ?? '-' }}</td>
            <td class="px-4 py-3 text-xs">
              <span v-if="inst.connect_address" class="inline-flex items-center gap-1 font-mono">{{ inst.connect_address }}
                <button class="rounded border px-1.5 py-0.5 text-[10px] hover:bg-muted" title="复制连接地址" @click="copyAddress(inst.connect_address!)">复制</button>
              </span>
              <span v-else class="text-muted-foreground">-</span>
            </td>
            <td class="px-4 py-3">
              <button
                v-if="action(inst.status)"
                class="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90 disabled:opacity-50"
                :disabled="busy"
                @click="onAction(inst, action(inst.status)!)"
              >
                {{ action(inst.status)!.label }}
              </button>
              <span v-else class="text-xs text-muted-foreground">-</span>
              <button class="ml-2 rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="openFiles(inst)">文件</button>
              <button class="ml-2 rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="openConfig(inst)">配置</button>
            </td>
          </tr>
          <tr v-if="!instances.length">
            <td colspan="6" class="px-4 py-8 text-center text-muted-foreground">暂无服务器——先去「订单」下单并开服</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>

    <!-- 配置编辑弹层（player 项，schema 驱动；重启生效） -->
    <div v-if="configOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="configOpen = false">
      <div class="max-h-[80vh] w-full max-w-lg overflow-y-auto rounded-lg border bg-white p-5 shadow-lg">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-lg font-semibold">实例配置 · {{ configInstance?.instance_id }}</h2>
          <button class="text-muted-foreground hover:text-foreground" @click="configOpen = false">✕</button>
        </div>
        <p class="mb-3 text-xs text-muted-foreground">修改后对下次启动生效（已运行实例需重启）。</p>
        <p v-if="!configSettings.length" class="text-sm text-muted-foreground">该游戏暂无平台可配置项。</p>
        <form v-else class="space-y-3" @submit.prevent="onSaveConfig">
          <div v-for="s in configSettings" :key="s.key" class="flex items-start justify-between gap-4">
            <div class="min-w-0 flex-1">
              <label class="block text-sm font-medium" :for="'ic-' + s.key">{{ label(s) }}</label>
              <p v-if="desc(s)" class="text-xs text-muted-foreground">{{ desc(s) }}</p>
            </div>
            <div class="w-48 shrink-0">
              <input v-if="s.type === 'string'" :id="'ic-' + s.key" v-model="configForm[s.key]" :type="s.secret ? 'password' : 'text'" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
              <input v-else-if="s.type === 'int'" :id="'ic-' + s.key" v-model="configForm[s.key]" type="number" :min="s.min" :max="s.max" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
              <select v-else-if="s.type === 'enum'" :id="'ic-' + s.key" v-model="configForm[s.key]" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2">
                <option v-for="v in s.enum_values" :key="v" :value="v">{{ enumLabel(s, v) }}</option>
              </select>
              <input v-else-if="s.type === 'bool'" :id="'ic-' + s.key" v-model="configForm[s.key]" type="checkbox" class="mt-2 h-4 w-4 rounded border" true-value="true" false-value="false" />
            </div>
          </div>
          <div class="flex items-center gap-3 border-t pt-3">
            <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">保存</button>
            <span v-if="configSaved" class="text-sm text-green-600">已保存（重启生效）</span>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { getGame } from '@/api/games'
import {
  instanceActions,
  myInstances,
  startOrderInstance,
  statusText,
  stopOrderInstance,
  updateInstanceConfig,
  type UserInstance,
} from '@/api/instances'
import { getConfigSchema, i18nText, type ConfigSchema, type ConfigSetting } from '@/api/orders'

const route = useRoute()
const router = useRouter()

const gameId = computed(() => route.params.gameId as string)
const gameName = ref(gameId.value)

const instances = ref<UserInstance[]>([])
const busy = ref(false)
const error = ref('')

// 配置弹层状态
const configOpen = ref(false)
const configInstance = ref<UserInstance | null>(null)
const configSchema = ref<ConfigSchema | null>(null)
const configForm = reactive<Record<string, string>>({})
const configSaved = ref(false)

const action = (status: string) => instanceActions(status)

const configSettings = computed<ConfigSetting[]>(
  () => configSchema.value?.settings.filter((s) => s.control === 'player') ?? [],
)
const label = (s: ConfigSetting) => i18nText(configSchema.value, s.label_key, s.key)
const desc = (s: ConfigSetting) => (s.description_key ? i18nText(configSchema.value, s.description_key, '') : '')
const enumLabel = (s: ConfigSetting, v: string) => {
  const key = 'enum.' + s.key + '.' + v
  const t = i18nText(configSchema.value, key, v)
  return t === key ? v : t
}

async function load() {
  error.value = ''
  try {
    instances.value = await myInstances(gameId.value)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载实例失败（请确认已登录、后端已启动）'
  }
}

function copyAddress(addr: string) {
  navigator.clipboard?.writeText(addr).then(
    () => { error.value = '' },
    () => { error.value = '复制失败，请手动复制' },
  )
}

function openFiles(inst: UserInstance) {
  router.push({
    name: 'my-instance-files',
    params: { orderId: inst.order_id },
    query: { running: inst.status === 'running' ? '1' : '0' },
  })
}

// 打开配置弹层：加载游戏配置 schema 并预填默认值
async function openConfig(inst: UserInstance) {
  configOpen.value = true
  configSaved.value = false
  configInstance.value = inst
  error.value = ''
  try {
    configSchema.value = await getConfigSchema(inst.game_id)
    Object.keys(configForm).forEach((k) => delete configForm[k])
    for (const s of configSettings.value) {
      if (s.default !== undefined) configForm[s.key] = s.default
    }
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载配置项失败'
    configOpen.value = false
  }
}

// 保存实例配置（重启生效）
async function onSaveConfig() {
  configSaved.value = false
  error.value = ''
  if (!configInstance.value) return
  const cfg: Record<string, string> = {}
  for (const s of configSettings.value) {
    const v = configForm[s.key]
    if (v !== undefined && v !== '') cfg[s.key] = v
  }
  try {
    await updateInstanceConfig(configInstance.value.order_id, cfg)
    configSaved.value = true
    setTimeout(() => (configSaved.value = false), 3000)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '保存失败'
  }
}

async function onAction(inst: UserInstance, act: { label: string; action: 'start' | 'stop' }) {
  busy.value = true
  error.value = ''
  try {
    if (act.action === 'start') {
      await startOrderInstance(inst.order_id)
    } else {
      await stopOrderInstance(inst.order_id)
    }
    setTimeout(load, 800)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败（controller 是否已启动？）'
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  load()
  try {
    const g = await getGame(gameId.value)
    gameName.value = g.profile?.display_name || g.Name
  } catch {
    /* 保持默认 */
  }
})
</script>
<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <div class="flex items-center gap-2">
          <RouterLink to="/" class="text-sm text-muted-foreground hover:underline">← 游戏列表</RouterLink>
          <RouterLink :to="{ name: 'game-servers', params: { gameId } }" class="text-sm text-muted-foreground hover:underline">服务器</RouterLink>
        </div>
        <h1 class="mt-1 text-2xl font-semibold">{{ gameName }} · 订单</h1>
        <p class="text-sm text-muted-foreground">下单 → 支付（占位）→ 创建实例（stopped）→ 到「服务器」开服。</p>
      </div>
    </div>

    <!-- 下单（game_id 自动取自当前游戏；配置表单由游戏 schema 驱动） -->
    <form class="max-w-xl space-y-4 rounded-lg border p-4" @submit.prevent="onCreate">
      <div class="flex max-w-md items-end gap-3">
        <div class="w-40">
          <label class="mb-1 block text-sm font-medium">金额（分）</label>
          <input v-model.number="form.amount" type="number" min="1" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
        </div>
      </div>

      <!-- 服务器配置（player 项，schema 驱动） -->
      <div v-if="playerSettings.length" class="space-y-4 border-t pt-4">
        <div class="text-sm font-medium">服务器配置</div>
        <p v-if="!schemaLoaded" class="text-xs text-muted-foreground">正在加载配置项…</p>
        <template v-for="group in groupedSettings" :key="group.name">
          <div class="rounded-md border p-3">
            <div class="mb-2 text-xs font-medium text-muted-foreground">{{ group.name }}</div>
            <div class="space-y-3">
              <div v-for="s in group.items" :key="s.key" class="flex items-start justify-between gap-4">
                <div class="min-w-0 flex-1">
                  <label class="block text-sm font-medium" :for="'cfg-' + s.key">{{ label(s) }}</label>
                  <p v-if="desc(s)" class="text-xs text-muted-foreground">{{ desc(s) }}</p>
                </div>
                <div class="w-52 shrink-0">
                  <input
                    v-if="s.type === 'string'"
                    :id="'cfg-' + s.key"
                    v-model="config[s.key]"
                    :type="s.secret ? 'password' : 'text'"
                    class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
                  />
                  <input
                    v-else-if="s.type === 'int'"
                    :id="'cfg-' + s.key"
                    v-model="config[s.key]"
                    type="number"
                    :min="s.min"
                    :max="s.max"
                    class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
                  />
                  <select
                    v-else-if="s.type === 'enum'"
                    :id="'cfg-' + s.key"
                    v-model="config[s.key]"
                    class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
                  >
                    <option v-for="v in s.enum_values" :key="v" :value="v">{{ enumLabel(s, v) }}</option>
                  </select>
                  <input
                    v-else-if="s.type === 'bool'"
                    :id="'cfg-' + s.key"
                    v-model="config[s.key]"
                    type="checkbox"
                    class="mt-2 h-4 w-4 rounded border"
                    true-value="true"
                    false-value="false"
                  />
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>

      <div class="flex items-center gap-3">
        <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">
          下单
        </button>
        <span v-if="schemaLoaded && !playerSettings.length" class="text-xs text-muted-foreground">该游戏暂无平台可配置项</span>
      </div>
    </form>

    <!-- 订单列表 -->
    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">订单</th>
            <th class="px-4 py-3">金额（分）</th>
            <th class="px-4 py-3">实例</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="o in orders" :key="o.ID" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ o.ID }}</td>
            <td class="px-4 py-3">{{ o.Amount }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ o.InstanceID || '-' }}</td>
            <td class="px-4 py-3">
              <span class="rounded bg-muted px-2 py-0.5 text-xs">{{ statusText(o.Status) }}</span>
            </td>
            <td class="px-4 py-3">
              <button
                v-if="o.Status === 0"
                class="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90"
                @click="onPay(o.ID)"
              >
                支付并创建实例
              </button>
            </td>
          </tr>
          <tr v-if="!orders.length">
            <td colspan="5" class="px-4 py-8 text-center text-muted-foreground">暂无订单</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'

import { getGame } from '@/api/games'
import {
  createOrder,
  getConfigSchema,
  i18nText,
  listOrders,
  payOrder,
  type ConfigSchema,
  type ConfigSetting,
  type Order,
} from '@/api/orders'

const route = useRoute()

const gameId = computed(() => route.params.gameId as string)
const gameName = ref(gameId.value)

const orders = ref<Order[]>([])
const form = reactive({ amount: 100 })
const error = ref('')

// 配置表单状态
const schema = ref<ConfigSchema | null>(null)
const schemaLoaded = ref(false)
const config = reactive<Record<string, string>>({})

const statusText = (s: number) => ['created', 'paid', 'cancelled', 'refunded', 'provisioned', '已下架'][s] ?? 'unknown'

// 玩家可见配置项（control=player）
const playerSettings = computed<ConfigSetting[]>(() =>
  schema.value?.settings.filter((s) => s.control === 'player') ?? [],
)

// 按 group_key 分组（无组名归入"通用"）
const groupedSettings = computed(() => {
  const groups: { name: string; items: ConfigSetting[] }[] = []
  const byGroup = new Map<string, ConfigSetting[]>()
  for (const s of playerSettings.value) {
    const g = s.group_key ?? 'grp.general'
    if (!byGroup.has(g)) byGroup.set(g, [])
    byGroup.get(g)!.push(s)
  }
  for (const [g, items] of byGroup) {
    groups.push({ name: i18nText(schema.value, g, g), items })
  }
  return groups
})

const label = (s: ConfigSetting) => i18nText(schema.value, s.label_key, s.key)
const desc = (s: ConfigSetting) => (s.description_key ? i18nText(schema.value, s.description_key, '') : '')
const enumLabel = (s: ConfigSetting, v: string) => {
  // 枚举选项显示名：schema.i18n 里按 "enum." 前缀或 key 名查找
  const key = 'enum.' + s.key + '.' + v
  const t = i18nText(schema.value, key, v)
  return t === key ? v : t
}

async function loadSchema() {
  schemaLoaded.value = false
  try {
    schema.value = await getConfigSchema(gameId.value)
    // 预填默认值
    if (schema.value) {
      for (const s of schema.value.settings) {
        if (s.control === 'player' && s.default !== undefined && config[s.key] === undefined) {
          config[s.key] = s.default
        }
      }
    }
  } catch {
    schema.value = null
  } finally {
    schemaLoaded.value = true
  }
}

async function load() {
  error.value = ''
  try {
    orders.value = await listOrders(undefined, gameId.value)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载订单失败'
  }
}

async function onCreate() {
  error.value = ''
  if (form.amount <= 0) {
    error.value = '请填写金额'
    return
  }
  try {
    // 只提交已填写的配置项
    const cfg: Record<string, string> = {}
    for (const [k, v] of Object.entries(config)) {
      if (v !== undefined && v !== '' && v !== null) cfg[k] = v
    }
    await createOrder({ game_id: gameId.value, amount: form.amount, config: Object.keys(cfg).length ? cfg : undefined })
    form.amount = 100
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '下单失败'
  }
}

async function onPay(id: string) {
  error.value = ''
  try {
    await payOrder(id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '支付失败（controller 是否已启动？）'
  }
}

onMounted(async () => {
  load()
  loadSchema()
  try {
    const g = await getGame(gameId.value)
    gameName.value = g.profile?.display_name || g.Name
  } catch {
    /* 保持默认 */
  }
})
</script>

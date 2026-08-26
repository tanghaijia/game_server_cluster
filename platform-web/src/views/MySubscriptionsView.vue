<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold">我的订阅</h1>
        <p class="text-sm text-muted-foreground">
          一次购买、随时切换：订阅内可创建多个游戏的实例，同一时间仅一个实例运行。
        </p>
      </div>
      <button class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90" @click="openBuy">
        购买订阅
      </button>
    </div>

    <!-- 订阅卡片 -->
    <div v-if="!subs.length && loaded" class="rounded-lg border p-8 text-center text-muted-foreground">
      还没有订阅，点击右上角「购买订阅」开始。
    </div>
    <div class="space-y-6">
      <div v-for="sub in subs" :key="sub.ID" class="rounded-lg border p-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-3">
            <span class="text-lg font-semibold">{{ planName(sub.PlanID) }}</span>
            <span class="rounded px-2 py-0.5 text-xs" :class="statusClass(sub.Status)">{{ statusText(sub.Status) }}</span>
            <span class="text-xs text-muted-foreground">{{ sub.ID }}</span>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-xs text-muted-foreground">{{ expiryText(sub) }}</span>
            <button
              v-if="sub.Status === 'active' || sub.Status === 'expired'"
              class="rounded-md border px-3 py-1 text-xs hover:bg-muted"
              @click="onRenew(sub)"
            >
              续费
            </button>
            <button
              v-if="sub.Status === 'active' || sub.Status === 'suspended'"
              class="rounded-md border px-3 py-1 text-xs text-red-500 hover:bg-muted"
              @click="onCancel(sub)"
            >
              取消
            </button>
          </div>
        </div>

        <!-- 篮子 -->
        <div class="mt-3 flex flex-wrap items-center gap-1.5">
          <span class="text-xs text-muted-foreground">可玩游戏：</span>
          <span v-for="g in sub.BasketSnapshot" :key="g.game_id" class="rounded bg-muted px-2 py-0.5 text-xs">{{ gameName(g.game_id) }}</span>
          <span class="ml-2 text-xs text-muted-foreground">· 实例上限：{{ sub.MaxInstances > 0 ? sub.MaxInstances : '不限' }}</span>
        </div>

        <!-- 实例 -->
        <div class="mt-4">
          <div class="mb-2 flex items-center justify-between">
            <span class="text-sm font-medium">实例（{{ subInstances(sub.ID).length }}）</span>
            <button
              v-if="sub.Status === 'active'"
              class="rounded-md border px-3 py-1 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-40"
              :disabled="atInstanceLimit(sub)"
              :title="atInstanceLimit(sub) ? '已达到实例数量上限，请先删除不再使用的实例' : ''"
              @click="openCreate(sub)"
            >
              + 创建实例
            </button>
          </div>
          <div v-if="!subInstances(sub.ID).length" class="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
            暂无实例。创建后启动即可开服（同一时间仅一个实例运行）。
          </div>
          <table v-else class="w-full text-sm">
            <thead>
              <tr class="border-b text-left text-muted-foreground">
                <th class="px-3 py-2">游戏</th>
                <th class="px-3 py-2">实例</th>
                <th class="px-3 py-2">状态</th>
                <th class="px-3 py-2">在线</th>
                <th class="px-3 py-2">失败原因</th>
                <th class="px-3 py-2">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="inst in subInstances(sub.ID)" :key="inst.ID" class="border-b last:border-0">
                <td class="px-3 py-2">{{ gameName(inst.GameID) }}</td>
                <td class="px-3 py-2 font-mono text-xs">{{ inst.ID }}</td>
                <td class="px-3 py-2">
                  <span class="rounded px-2 py-0.5 text-xs" :class="instStatusClass(inst.Status)">{{ inst.Status }}</span>
                </td>
                <td class="px-3 py-2">
                  <!-- B-04/P1-1：在线人数 + 健康（探针数据，15s 轮询） -->
                  <template v-if="inst.Status === 'running'">
                    <span v-if="rt(inst.ID)" class="flex items-center gap-1.5 text-xs" :title="rt(inst.ID)!.probe_error || ''">
                      <span class="h-2 w-2 shrink-0 rounded-full" :class="healthDotClass(inst.ID)"></span>
                      <span>{{ rtText(inst.ID) }}</span>
                    </span>
                    <span v-else class="text-xs text-muted-foreground">探测中…</span>
                  </template>
                  <span v-else class="text-muted-foreground">-</span>
                </td>
                <td class="max-w-[220px] px-3 py-2 text-xs text-red-500">
                  <span v-if="inst.FailReason" :title="inst.FailReason" class="line-clamp-1">{{ inst.FailReason }}</span>
                  <span v-else class="text-muted-foreground">-</span>
                </td>
                <td class="px-3 py-2">
                  <button
                    v-if="inst.Status === 'running' || inst.Status === 'starting' || inst.Status === 'pending' || (inst.Status === 'failed' && inst.NodeAgentID)"
                    class="rounded-md border px-3 py-1 text-xs hover:bg-muted"
                    @click="onStop(sub, inst)"
                  >
                    {{ inst.Status === 'failed' ? '停止（清理残留）' : '停止' }}
                  </button>
                  <button
                    v-else-if="inst.Status === 'stopped' || inst.Status === 'failed'"
                    class="rounded-md border px-3 py-1 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-40"
                    :disabled="hasActive(sub)"
                    :title="hasActive(sub) ? '订阅内已有实例在运行，请先停止它' : ''"
                    @click="onStart(sub, inst)"
                  >
                    启动
                  </button>
                  <span v-else class="text-xs text-muted-foreground">调度中…</span>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="hasActive(sub)" class="mt-2 text-xs text-muted-foreground">
            当前活跃：{{ activeInstanceLabel(sub) }}。启动其他实例前请先停止它（单活跃约束）。
          </p>
        </div>
      </div>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>

    <!-- 购买订阅弹窗 -->
    <div v-if="buyOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="buyOpen = false">
      <div class="max-h-[90vh] w-[560px] overflow-auto rounded-lg border bg-background p-5">
        <div class="mb-4 flex items-center justify-between">
          <span class="text-lg font-semibold">购买订阅</span>
          <button class="text-sm text-muted-foreground hover:underline" @click="buyOpen = false">关闭</button>
        </div>
        <div v-if="!plans.length" class="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
          暂无在售套餐（请管理员先在「套餐管理」创建）
        </div>
        <div class="space-y-3">
          <div v-for="p in plans" :key="p.ID" class="flex items-center justify-between rounded-md border p-3">
            <div>
              <div class="text-sm font-medium">{{ p.DisplayName }}</div>
              <div class="mt-0.5 text-xs text-muted-foreground">
                {{ (p.PriceCents / 100).toFixed(2) }} 元 / {{ formatDuration(p.DurationHours) }}
                · {{ p.Basket.map((g) => gameName(g.game_id)).join('、') }}
              </div>
            </div>
            <button class="rounded-md bg-primary px-3 py-1.5 text-xs text-primary-foreground hover:opacity-90" @click="onBuy(p.ID)">购买</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 创建实例弹窗 -->
    <div v-if="createSub" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="createSub = null">
      <div class="max-h-[90vh] w-[640px] overflow-auto rounded-lg border bg-background p-5">
        <div class="mb-4 flex items-center justify-between">
          <span class="text-lg font-semibold">创建实例（{{ planName(createSub.PlanID) }}）</span>
          <button class="text-sm text-muted-foreground hover:underline" @click="createSub = null">关闭</button>
        </div>

        <label class="mb-1 block text-xs text-muted-foreground">游戏</label>
        <select v-model="createGameId" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" @change="onGameChanged">
          <option value="" disabled>选择游戏</option>
          <option v-for="g in createSub.BasketSnapshot" :key="g.game_id" :value="g.game_id">{{ gameName(g.game_id) }}</option>
        </select>

        <!-- 配置表单（schema 驱动） -->
        <div v-if="createGameId" class="mt-4 space-y-3">
          <div class="text-sm font-medium">实例配置（可选，默认取套餐预设）</div>
          <p v-if="!schemaLoaded" class="text-xs text-muted-foreground">正在加载配置项…</p>
          <p v-else-if="!playerSettings.length" class="text-xs text-muted-foreground">该游戏无可配置项，将使用套餐预设直接创建。</p>
          <template v-for="group in groupedSettings" :key="group.name">
            <div class="rounded-md border p-3">
              <div class="mb-2 text-xs font-medium text-muted-foreground">{{ group.name }}</div>
              <div class="space-y-3">
                <div v-for="s in group.items" :key="s.key" class="flex items-start justify-between gap-4">
                  <div class="min-w-0 flex-1">
                    <label class="block text-sm font-medium">{{ label(s) }}</label>
                    <p v-if="desc(s)" class="text-xs text-muted-foreground">{{ desc(s) }}</p>
                  </div>
                  <div class="w-52 shrink-0">
                    <input
                      v-if="s.type === 'string'"
                      v-model="createConfig[s.key]"
                      :type="s.secret ? 'password' : 'text'"
                      class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
                    />
                    <input
                      v-else-if="s.type === 'int'"
                      v-model="createConfig[s.key]"
                      type="number"
                      :min="s.min"
                      :max="s.max"
                      class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
                    />
                    <select
                      v-else-if="s.type === 'enum'"
                      v-model="createConfig[s.key]"
                      class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
                    >
                      <option v-for="v in s.enum_values" :key="v" :value="v">{{ enumLabel(s, v) }}</option>
                    </select>
                    <input
                      v-else-if="s.type === 'bool'"
                      v-model="createConfig[s.key]"
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

        <div class="mt-4 flex justify-end gap-2">
          <button class="rounded-md border px-4 py-2 text-sm hover:bg-muted" @click="createSub = null">取消</button>
          <button class="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90" :disabled="!createGameId" @click="onCreateInstance">
            创建
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'

import { listGames, type GameView } from '@/api/games'
import { getConfigSchema, i18nText, type ConfigSchema, type ConfigSetting } from '@/api/orders'
import {
  cancelSubscription,
  createSubscriptionInstance,
  getSubscriptionInstanceRuntime,
  listEnabledPlans,
  listMySubscriptions,
  listSubscriptionInstances,
  purchaseSubscription,
  renewSubscription,
  startSubscriptionInstance,
  stopSubscriptionInstance,
  type InstanceRuntime,
  type Subscription,
  type SubscriptionInstance,
} from '@/api/subscriptions'
import type { ServerPlan } from '@/api/admin'

const subs = ref<Subscription[]>([])
const instances = ref<Record<string, SubscriptionInstance[]>>({})
const plans = ref<ServerPlan[]>([])
const games = ref<GameView[]>([])
const loaded = ref(false)
const error = ref('')

const gameNameMap = ref<Record<string, string>>({})
function gameName(id: string) {
  return gameNameMap.value[id] || id
}

const planNameMap = computed(() => Object.fromEntries(plans.value.map((p) => [p.ID, p.DisplayName])))
function planName(id: string) {
  return planNameMap.value[id] || id
}

function subInstances(id: string) {
  return instances.value[id] ?? []
}

// ---------- B-04/P1-1：实例运行时统计（健康 + 在线人数）轮询 ----------

const runtimeMap = ref<Record<string, InstanceRuntime>>({})
function rt(id: string) {
  return runtimeMap.value[id]
}
function healthDotClass(id: string) {
  const r = runtimeMap.value[id]
  if (!r || r.probe_mode === 'unknown') return 'bg-gray-300'
  return r.healthy ? 'bg-green-500' : 'bg-red-500'
}
function rtText(id: string) {
  const r = runtimeMap.value[id]
  if (!r || r.probe_mode === 'unknown') return '探测中'
  return `${r.player_count}/${r.max_players}`
}

// 抓取所有运行中实例的运行时统计（单实例失败忽略，下一轮重试）
async function refreshRuntimes() {
  const targets: { subId: string; instId: string }[] = []
  for (const sub of subs.value) {
    for (const inst of subInstances(sub.ID)) {
      if (inst.Status === 'running') targets.push({ subId: sub.ID, instId: inst.ID })
    }
  }
  const next: Record<string, InstanceRuntime> = {}
  await Promise.allSettled(
    targets.map(async (t) => {
      try {
        next[t.instId] = await getSubscriptionInstanceRuntime(t.subId, t.instId)
      } catch {
        /* 忽略 */
      }
    }),
  )
  runtimeMap.value = next
}

function hasActive(sub: Subscription) {
  return subInstances(sub.ID).some((i) => !['stopped', 'failed'].includes(i.Status))
}

function atInstanceLimit(sub: Subscription) {
  return sub.MaxInstances > 0 && subInstances(sub.ID).length >= sub.MaxInstances
}

function activeInstanceLabel(sub: Subscription) {
  const a = subInstances(sub.ID).find((i) => !['stopped', 'failed'].includes(i.Status))
  return a ? `${gameName(a.GameID)}（${a.ID}）` : ''
}

function statusText(s: string) {
  return { active: '有效', expired: '已到期', cancelled: '已取消', suspended: '已停用' }[s] ?? s
}
function statusClass(s: string) {
  return {
    active: 'bg-primary text-primary-foreground',
    expired: 'bg-muted text-muted-foreground',
    cancelled: 'bg-muted text-muted-foreground',
    suspended: 'bg-amber-100 text-amber-700',
  }[s] ?? 'bg-muted'
}
function instStatusClass(s: string) {
  const active = ['running', 'starting', 'pending', 'scheduling', 'queued', 'cache_warming', 'stopping', 'cleaning', 'preparing_build', 'restoring_snapshot']
  if (s === 'failed') return 'bg-red-100 text-red-600'
  if (s === 'running') return 'bg-green-100 text-green-700'
  if (active.includes(s)) return 'bg-blue-100 text-blue-700'
  return 'bg-muted text-muted-foreground'
}

function formatDuration(hours: number) {
  if (hours <= 0) return '永久'
  if (hours % 24 === 0) return hours / 24 + ' 天'
  return hours + ' 小时'
}

function expiryText(sub: Subscription) {
  if (sub.Status === 'expired') return '已到期'
  if (!sub.ExpiresAt) return '永久有效'
  const remain = new Date(sub.ExpiresAt).getTime() - Date.now()
  if (remain <= 0) return '已到期'
  const days = Math.ceil(remain / 86400000)
  return `剩余 ${days} 天`
}

// ---------- 实例加载 ----------

async function loadInstances(sub: Subscription) {
  try {
    instances.value[sub.ID] = await listSubscriptionInstances(sub.ID)
  } catch {
    instances.value[sub.ID] = []
  }
}

async function load() {
  error.value = ''
  try {
    const [s, p, g] = await Promise.all([listMySubscriptions(), listEnabledPlans(), listGames()])
    subs.value = s
    plans.value = p
    games.value = g
    gameNameMap.value = Object.fromEntries(g.map((game) => [game.ID, game.profile?.display_name || game.Name]))
    await Promise.all(s.map((sub) => loadInstances(sub)))
    await refreshRuntimes()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载失败'
  } finally {
    loaded.value = true
  }
}

// ---------- 购买 / 续费 / 取消 ----------

const buyOpen = ref(false)
function openBuy() {
  error.value = ''
  buyOpen.value = true
}

async function onBuy(planId: string) {
  error.value = ''
  try {
    await purchaseSubscription(planId)
    buyOpen.value = false
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '购买失败'
  }
}

async function onRenew(sub: Subscription) {
  error.value = ''
  try {
    await renewSubscription(sub.ID)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '续费失败'
  }
}

async function onCancel(sub: Subscription) {
  if (!confirm('确认取消订阅「' + planName(sub.PlanID) + '」？将停止其中所有运行的实例。')) return
  error.value = ''
  try {
    await cancelSubscription(sub.ID)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '取消失败'
  }
}

// ---------- 实例启停 / 创建 ----------

async function onStart(sub: Subscription, inst: SubscriptionInstance) {
  error.value = ''
  try {
    await startSubscriptionInstance(sub.ID, inst.ID)
    await loadInstances(sub)
    await refreshRuntimes()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '启动失败'
  }
}

async function onStop(sub: Subscription, inst: SubscriptionInstance) {
  error.value = ''
  try {
    await stopSubscriptionInstance(sub.ID, inst.ID)
    await loadInstances(sub)
    await refreshRuntimes()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '停止失败'
  }
}

// ---------- 创建实例（schema 驱动配置表单） ----------

const createSub = ref<Subscription | null>(null)
const createGameId = ref('')
const schema = ref<ConfigSchema | null>(null)
const schemaLoaded = ref(false)
const createConfig = reactive<Record<string, string>>({})

const playerSettings = computed<ConfigSetting[]>(() =>
  schema.value?.settings.filter((s) => s.control === 'player') ?? [],
)
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
  const key = 'enum.' + s.key + '.' + v
  const t = i18nText(schema.value, key, v)
  return t === key ? v : t
}

function openCreate(sub: Subscription) {
  error.value = ''
  createSub.value = sub
  createGameId.value = ''
  Object.keys(createConfig).forEach((k) => delete createConfig[k])
  schema.value = null
  schemaLoaded.value = false
}

async function onGameChanged() {
  schema.value = null
  schemaLoaded.value = false
  Object.keys(createConfig).forEach((k) => delete createConfig[k])
  if (!createGameId.value) return
  try {
    schema.value = await getConfigSchema(createGameId.value)
    if (schema.value) {
      for (const s of schema.value.settings) {
        if (s.control === 'player' && s.default !== undefined && createConfig[s.key] === undefined) {
          createConfig[s.key] = s.default
        }
      }
    }
  } catch {
    schema.value = null
  } finally {
    schemaLoaded.value = true
  }
}

async function onCreateInstance() {
  if (!createSub.value || !createGameId.value) return
  error.value = ''
  const cfg: Record<string, string> = {}
  for (const [k, v] of Object.entries(createConfig)) {
    if (v !== undefined && v !== '' && v !== null) cfg[k] = v
  }
  const subId = createSub.value.ID
  try {
    await createSubscriptionInstance(subId, createGameId.value, Object.keys(cfg).length ? cfg : undefined)
    createSub.value = null
    const sub = subs.value.find((s) => s.ID === subId)
    if (sub) await loadInstances(sub)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '创建实例失败'
  }
}

let runtimeTimer: number | undefined

onMounted(() => {
  load()
  // B-04/P1-1：运行时统计 15s 轮询（controller 心跳 10s / 探针 20s）
  runtimeTimer = window.setInterval(refreshRuntimes, 15000)
})
onUnmounted(() => {
  if (runtimeTimer !== undefined) window.clearInterval(runtimeTimer)
})
</script>

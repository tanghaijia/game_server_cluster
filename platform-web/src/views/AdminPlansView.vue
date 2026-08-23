<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold">套餐管理</h1>
        <p class="text-sm text-muted-foreground">
          订阅制商品（SKU）：定义价格、时长与允许的游戏篮子。编辑只影响新购订阅，已购订阅按购买时快照。
        </p>
      </div>
      <button class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90" @click="openCreate">
        + 新增套餐
      </button>
    </div>

    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">名称</th>
            <th class="px-4 py-3">价格</th>
            <th class="px-4 py-3">时长</th>
            <th class="px-4 py-3">篮子</th>
            <th class="px-4 py-3">资源上限</th>
            <th class="px-4 py-3">实例上限</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in plans" :key="p.ID" class="border-b last:border-0">
            <td class="px-4 py-3">
              <div class="font-medium">{{ p.DisplayName }}</div>
              <div v-if="p.Description" class="text-xs text-muted-foreground">{{ p.Description }}</div>
            </td>
            <td class="px-4 py-3">{{ (p.PriceCents / 100).toFixed(2) }} 元</td>
            <td class="px-4 py-3">{{ formatDuration(p.DurationHours) }}</td>
            <td class="px-4 py-3">
              <div class="flex flex-wrap gap-1">
                <span v-for="g in p.Basket" :key="g.game_id" class="rounded bg-muted px-1.5 py-0.5 text-xs" :title="gameName(g.game_id)">
                  {{ gameName(g.game_id) }}
                </span>
              </div>
            </td>
            <td class="px-4 py-3 text-xs text-muted-foreground">
              {{ (p.ResourceCPUMilli / 1000).toFixed(1) }} 核 / {{ (p.ResourceMemoryBytes / 1e9).toFixed(1) }} GB / {{ (p.ResourceDiskBytes / 1e9).toFixed(1) }} GB
            </td>
            <td class="px-4 py-3">{{ p.MaxInstances > 0 ? p.MaxInstances + ' 个' : '不限' }}</td>
            <td class="px-4 py-3">
              <span class="rounded px-2 py-0.5 text-xs" :class="p.Enabled ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'">
                {{ p.Enabled ? '在售' : '已下架' }}
              </span>
            </td>
            <td class="px-4 py-3 space-x-2">
              <button class="rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="openEdit(p)">编辑</button>
              <button class="rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="onToggleEnabled(p)">
                {{ p.Enabled ? '下架' : '上架' }}
              </button>
              <button class="rounded-md border px-3 py-1 text-xs text-red-500 hover:bg-muted" @click="onDelete(p)">删除</button>
            </td>
          </tr>
          <tr v-if="!plans.length">
            <td colspan="8" class="px-4 py-8 text-center text-muted-foreground">暂无套餐，点击右上角「新增套餐」创建第一个</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>

    <!-- 新增/编辑弹窗 -->
    <div v-if="dialogOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="dialogOpen = false">
      <div class="max-h-[90vh] w-[760px] overflow-auto rounded-lg border bg-background p-5">
        <div class="mb-4 flex items-center justify-between">
          <span class="text-lg font-semibold">{{ editingId ? '编辑套餐' : '新增套餐' }}</span>
          <button class="text-sm text-muted-foreground hover:underline" @click="dialogOpen = false">关闭</button>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div class="col-span-2">
            <label class="mb-1 block text-xs text-muted-foreground">套餐名称（必填）</label>
            <input v-model="form.displayName" type="text" placeholder="如 DST 双人包" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div class="col-span-2">
            <label class="mb-1 block text-xs text-muted-foreground">描述</label>
            <input v-model="form.description" type="text" placeholder="一句话介绍" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">价格（元）</label>
            <input v-model.number="form.priceYuan" type="number" min="0" step="0.01" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">时长（小时，0 = 永久）</label>
            <input v-model.number="form.durationHours" type="number" min="0" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">CPU 上限（核）</label>
            <input v-model.number="form.cpuCores" type="number" min="0" step="0.5" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">内存上限（GB）</label>
            <input v-model.number="form.memGb" type="number" min="0" step="0.5" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div class="col-span-2">
            <label class="mb-1 block text-xs text-muted-foreground">磁盘上限（GB）</label>
            <input v-model.number="form.diskGb" type="number" min="0" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div class="col-span-2">
            <label class="mb-1 block text-xs text-muted-foreground">订阅内实例数量上限（0 = 不限）</label>
            <input v-model.number="form.maxInstances" type="number" min="0" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
        </div>

        <!-- 篮子编辑器 -->
        <div class="mt-4">
          <div class="mb-2 flex items-center justify-between">
            <span class="text-sm font-medium">允许的游戏篮子（至少 1 个）</span>
            <span class="text-xs text-muted-foreground">凭证类配置（如 DST cluster_token）不在 preset 中，走凭证池注入</span>
          </div>
          <div v-if="!games.length" class="rounded-md border border-dashed p-3 text-center text-xs text-muted-foreground">
            暂无游戏（请先在游戏管理中创建游戏，并确保 controller 可达）
          </div>
          <div v-else class="space-y-2">
            <div v-for="g in games" :key="g.ID" class="rounded-md border p-2.5">
              <label class="flex cursor-pointer items-center gap-2 text-sm">
                <input :checked="isInBasket(g.ID)" type="checkbox" class="h-4 w-4" @change="onToggleGame(g.ID, ($event.target as HTMLInputElement).checked)" />
                <span>{{ gameName(g.ID) }}</span>
                <span class="text-xs text-muted-foreground">（{{ g.ID }}）</span>
              </label>
              <div v-if="isInBasket(g.ID)" class="mt-2 pl-6">
                <label class="mb-1 block text-xs text-muted-foreground">该游戏默认配置（JSON 对象，可留空）</label>
                <textarea
                  v-model="basketConfigs[g.ID]"
                  rows="2"
                  placeholder='如 {"world_name": "MyWorld"}'
                  class="w-full rounded-md border px-3 py-1.5 font-mono text-xs outline-none focus:ring-2"
                />
                <p v-if="configError[g.ID]" class="mt-1 text-xs text-red-500">{{ configError[g.ID] }}</p>
              </div>
            </div>
          </div>
        </div>

        <div class="mt-4 flex justify-end gap-2">
          <button class="rounded-md border px-4 py-2 text-sm hover:bg-muted" @click="dialogOpen = false">取消</button>
          <button class="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90" @click="onSubmit">保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import { createPlan, deletePlan, listGames, listPlans, updatePlan, type Game, type PlanBasketItem, type ServerPlan } from '@/api/admin'

const plans = ref<ServerPlan[]>([])
const games = ref<Game[]>([])
const error = ref('')

const dialogOpen = ref(false)
const editingId = ref('')
const form = reactive({
  displayName: '',
  description: '',
  priceYuan: 0,
  durationHours: 720,
  cpuCores: 1,
  memGb: 2,
  diskGb: 20,
  maxInstances: 10,
})
const basketConfigs = reactive<Record<string, string>>({}) // game_id -> JSON 文本
const configError = reactive<Record<string, string>>({})

const gameNameMap = ref<Record<string, string>>({})
function gameName(id: string) {
  return gameNameMap.value[id] || id
}

function formatDuration(hours: number) {
  if (hours <= 0) return '永久'
  if (hours % 24 === 0) return hours / 24 + ' 天'
  return hours + ' 小时'
}

function isInBasket(gameId: string) {
  return gameId in basketConfigs
}

function onToggleGame(gameId: string, checked: boolean) {
  if (checked) {
    basketConfigs[gameId] = ''
  } else {
    delete basketConfigs[gameId]
    delete configError[gameId]
  }
}

function openCreate() {
  error.value = ''
  editingId.value = ''
  form.displayName = ''
  form.description = ''
  form.priceYuan = 0
  form.durationHours = 720
  form.cpuCores = 1
  form.memGb = 2
  form.diskGb = 20
  form.maxInstances = 10
  Object.keys(basketConfigs).forEach((k) => delete basketConfigs[k])
  Object.keys(configError).forEach((k) => delete configError[k])
  dialogOpen.value = true
}

function openEdit(p: ServerPlan) {
  error.value = ''
  editingId.value = p.ID
  form.displayName = p.DisplayName
  form.description = p.Description || ''
  form.priceYuan = p.PriceCents / 100
  form.durationHours = p.DurationHours
  form.cpuCores = p.ResourceCPUMilli / 1000
  form.memGb = p.ResourceMemoryBytes / 1e9
  form.diskGb = p.ResourceDiskBytes / 1e9
  form.maxInstances = p.MaxInstances
  Object.keys(basketConfigs).forEach((k) => delete basketConfigs[k])
  Object.keys(configError).forEach((k) => delete configError[k])
  for (const item of p.Basket) {
    basketConfigs[item.game_id] = item.config && Object.keys(item.config).length ? JSON.stringify(item.config, null, 2) : ''
  }
  dialogOpen.value = true
}

// 把表单组装成后端请求体；parseConfigs 校验 JSON 并返回（无效时写入 configError）
function toPlanInput(): { data: ReturnType<typeof buildInput>; ok: boolean } {
  const basket: PlanBasketItem[] = []
  let ok = true
  for (const gameId of Object.keys(basketConfigs)) {
    const raw = (basketConfigs[gameId] || '').trim()
    let config: Record<string, string> | undefined
    if (raw) {
      try {
        const parsed = JSON.parse(raw)
        if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
          throw new Error('必须是 JSON 对象')
        }
        config = Object.fromEntries(Object.entries(parsed).map(([k, v]) => [k, String(v)]))
        delete configError[gameId]
      } catch (e: any) {
        configError[gameId] = '配置 JSON 无效：' + e.message
        ok = false
        continue
      }
    }
    basket.push({ game_id: gameId, config })
  }
  return { data: buildInput(basket), ok }
}

function buildInput(basket: PlanBasketItem[]) {
  return {
    display_name: form.displayName,
    description: form.description,
    price_cents: Math.round(form.priceYuan * 100),
    duration_hours: form.durationHours,
    resource_cpu_milli: Math.round(form.cpuCores * 1000),
    resource_memory_bytes: Math.round(form.memGb * 1e9),
    resource_disk_bytes: Math.round(form.diskGb * 1e9),
    max_instances: form.maxInstances,
    basket,
  }
}

async function onSubmit() {
  error.value = ''
  if (!form.displayName) {
    error.value = '请填写套餐名称'
    return
  }
  const { data, ok } = toPlanInput()
  if (!ok) return
  if (!data.basket.length) {
    error.value = '篮子至少选择一个游戏'
    return
  }
  try {
    if (editingId.value) {
      await updatePlan(editingId.value, data)
    } else {
      await createPlan(data)
    }
    dialogOpen.value = false
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '保存失败'
  }
}

// 快速上架/下架：整体提交当前套餐 + enabled 翻转
async function onToggleEnabled(p: ServerPlan) {
  error.value = ''
  try {
    await updatePlan(p.ID, {
      display_name: p.DisplayName,
      description: p.Description,
      price_cents: p.PriceCents,
      duration_hours: p.DurationHours,
      resource_cpu_milli: p.ResourceCPUMilli,
      resource_memory_bytes: p.ResourceMemoryBytes,
      resource_disk_bytes: p.ResourceDiskBytes,
      max_instances: p.MaxInstances,
      basket: p.Basket,
      enabled: !p.Enabled,
    })
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败'
  }
}

async function onDelete(p: ServerPlan) {
  if (!confirm('确认删除套餐「' + p.DisplayName + '」？\n（已被订阅引用的套餐将自动下架而非删除）')) return
  error.value = ''
  try {
    await deletePlan(p.ID)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '删除失败'
  }
}

async function load() {
  error.value = ''
  try {
    const [p, g] = await Promise.all([listPlans(), listGames()])
    plans.value = p
    games.value = g
    gameNameMap.value = Object.fromEntries(g.map((game) => [game.ID, game.Name]))
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载失败（platform/controller 是否已启动？）'
  }
}

onMounted(load)
</script>

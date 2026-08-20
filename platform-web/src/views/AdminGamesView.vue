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
              <button class="rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="openConfig(g)">容器配置</button>
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

    <!-- 容器配置弹窗 -->
    <div v-if="cfgGameId" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="cfgGameId = ''">
      <div class="max-h-[90vh] w-[720px] overflow-auto rounded-lg border bg-background p-5">
        <div class="mb-4 flex items-center justify-between">
          <span class="text-lg font-semibold">容器配置（游戏 {{ cfgGameId }}）</span>
          <button class="text-sm text-muted-foreground hover:underline" @click="cfgGameId = ''">关闭</button>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div class="col-span-2">
            <label class="mb-1 block text-xs text-muted-foreground">容器挂载路径（/server）</label>
            <input v-model="cfg.serverPath" type="text" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">端口模式</label>
            <select v-model.number="cfg.portMode" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2">
              <option :value="0">NAT（动态映射）</option>
              <option :value="1">HOST（直用宿主端口）</option>
            </select>
          </div>
          <div class="flex items-end pb-2">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="cfg.injectGamePort" type="checkbox" class="h-4 w-4" />
              端口注入（游戏端口 = 宿主端口，env 通告）
            </label>
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">CPU 请求（核）</label>
            <input v-model.number="cfg.cpuCores" type="number" min="0.5" step="0.5" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">内存请求（GB）</label>
            <input v-model.number="cfg.memGb" type="number" min="0.5" step="0.5" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">磁盘请求（GB）</label>
            <input v-model.number="cfg.diskGb" type="number" min="1" class="w-full rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">带宽 rx/tx（Mbps）</label>
            <div class="flex gap-2">
              <input v-model.number="cfg.bwRx" type="number" min="0" class="w-1/2 rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
              <input v-model.number="cfg.bwTx" type="number" min="0" class="w-1/2 rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
            </div>
          </div>
          <div class="flex items-end pb-2">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="cfg.singleThreaded" type="checkbox" class="h-4 w-4" />
              单核应用（CPU 整核校验 + 主频偏好）
            </label>
          </div>
        </div>

        <div class="mt-4">
          <div class="mb-2 flex items-center justify-between">
            <span class="text-sm font-medium">端口片段（{{ cfg.excerpts.length }}）</span>
            <button class="rounded-md border px-2 py-1 text-xs hover:bg-muted" @click="addExcerpt">+ 添加片段</button>
          </div>
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b text-left text-muted-foreground">
                <th class="py-1 pr-2">协议</th>
                <th class="py-1 pr-2">起始端口</th>
                <th class="py-1 pr-2">长度</th>
                <th class="py-1 pr-2">游戏端口</th>
                <th class="py-1"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(e, i) in cfg.excerpts" :key="i" class="border-b last:border-0">
                <td class="py-1 pr-2">
                  <select v-model.number="e.protocol" class="rounded-md border px-2 py-1 outline-none">
                    <option :value="0">tcp</option>
                    <option :value="1">udp</option>
                  </select>
                </td>
                <td class="py-1 pr-2"><input v-model.number="e.begin_port" type="number" min="1" max="65535" class="w-24 rounded-md border px-2 py-1 outline-none" /></td>
                <td class="py-1 pr-2"><input v-model.number="e.length" type="number" min="1" class="w-16 rounded-md border px-2 py-1 outline-none" /></td>
                <td class="py-1 pr-2"><input v-model="e.is_game_port" type="checkbox" class="h-4 w-4" /></td>
                <td class="py-1 text-right"><button class="text-red-500 hover:underline" @click="removeExcerpt(i)">删除</button></td>
              </tr>
              <tr v-if="!cfg.excerpts.length">
                <td colspan="5" class="py-3 text-center text-muted-foreground">无端口片段（游戏将无法分配端口）</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="mt-4 flex justify-end gap-2">
          <button class="rounded-md border px-4 py-2 text-sm hover:bg-muted" @click="cfgGameId = ''">取消</button>
          <button class="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90" @click="onSaveConfig">保存配置</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import { useRouter } from 'vue-router'

import { createGame, deleteGame, getContainerConfig, listGames, updateContainerConfig, updateGame, type Game } from '@/api/admin'

const games = ref<Game[]>([])
const editingId = ref('')
const error = ref('')
const form = reactive({ name: '', appId: '' })

// 容器配置弹窗
const cfgGameId = ref('')
const cfg = reactive({
  serverPath: '/server',
  portMode: 0,
  injectGamePort: false,
  cpuCores: 1,
  memGb: 1,
  diskGb: 10,
  bwRx: 50,
  bwTx: 50,
  singleThreaded: false,
  excerpts: [] as Array<{ protocol: number; begin_port: number; length: number; is_game_port: boolean }>,
})

async function openConfig(g: Game) {
  error.value = ''
  try {
    const c = await getContainerConfig(g.ID)
    cfgGameId.value = g.ID
    cfg.serverPath = c.ContainerServerPath || '/server'
    cfg.portMode = c.PortMode ?? 0
    cfg.injectGamePort = c.InjectGamePort
    cfg.cpuCores = c.CPURequestMilli ? c.CPURequestMilli / 1000 : 1
    cfg.memGb = c.MemoryRequestBytes ? c.MemoryRequestBytes / 1e9 : 1
    cfg.diskGb = c.DiskRequestBytes ? c.DiskRequestBytes / 1e9 : 10
    cfg.bwRx = c.BandwidthRxMbps ?? 50
    cfg.bwTx = c.BandwidthTxMbps ?? 50
    cfg.singleThreaded = c.SingleThreaded
    cfg.excerpts = (c.PortExcerpt || []).map((e) => ({
      protocol: e.Protocol,
      begin_port: e.BeginPort,
      length: e.ExcerptLength,
      is_game_port: e.IsGamePort,
    }))
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载容器配置失败（游戏需已关联 container_config）'
  }
}

function addExcerpt() {
  cfg.excerpts.push({ protocol: 1, begin_port: 27015, length: 1, is_game_port: false })
}

function removeExcerpt(i: number) {
  cfg.excerpts.splice(i, 1)
}

async function onSaveConfig() {
  if (!cfgGameId.value) return
  error.value = ''
  try {
    await updateContainerConfig(cfgGameId.value, {
      container_server_path: cfg.serverPath,
      port_mode: cfg.portMode,
      inject_game_port: cfg.injectGamePort,
      cpu_request_milli: Math.round(cfg.cpuCores * 1000),
      memory_request_bytes: Math.round(cfg.memGb * 1e9),
      disk_request_bytes: Math.round(cfg.diskGb * 1e9),
      bandwidth_rx_mbps: cfg.bwRx,
      bandwidth_tx_mbps: cfg.bwTx,
      single_threaded: cfg.singleThreaded,
      port_excerpts: cfg.excerpts.map((e) => ({
        protocol: e.protocol,
        begin_port: e.begin_port,
        excerpt_length: e.length,
        is_game_port: e.is_game_port,
      })),
    })
    cfgGameId.value = ''
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '保存容器配置失败'
  }
}

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

<template>
  <div class="space-y-6">
    <div>
      <RouterLink to="/admin/games" class="text-sm text-muted-foreground hover:underline">← 游戏管理</RouterLink>
      <RouterLink :to="{ name: 'admin-game-platform-config', params: { gameId } }" class="ml-3 text-sm text-muted-foreground hover:underline">平台配置</RouterLink>
      <h1 class="mt-1 text-2xl font-semibold">构建版本 · {{ gameId }}</h1>
      <p class="text-sm text-muted-foreground">管理游戏的资产构建版本（channel 分组、历史版本、注册新构建）。</p>
    </div>

    <!-- 注册新构建 -->
    <form class="grid max-w-3xl grid-cols-2 gap-3 rounded-lg border p-4" @submit.prevent="onRegister">
      <div>
        <label class="mb-1 block text-sm font-medium">build_id *</label>
        <input v-model="form.build_id" type="text" placeholder="如 294420-public-0.3.0" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">channel</label>
        <input v-model="form.channel" type="text" placeholder="如 public / beta" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">adapter_version</label>
        <input v-model="form.adapter_version" type="text" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">upstream_version</label>
        <input v-model="form.upstream_version" type="text" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">artifact_image_name</label>
        <input v-model="form.artifact_image_name" type="text" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">artifact_image_tag</label>
        <input v-model="form.artifact_image_tag" type="text" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div class="col-span-2">
        <label class="mb-1 block text-sm font-medium">artifact_uri</label>
        <input v-model="form.artifact_uri" type="text" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <!-- M5：配置 schema / 适配器元数据（gen_manifest.py 产物，可选） -->
      <div class="col-span-2 rounded-md border border-dashed p-3">
        <div class="mb-2 text-xs font-medium text-muted-foreground">
          配置能力（可选）：上传 <code class="rounded bg-muted px-1">schema.json</code> 与
          <code class="rounded bg-muted px-1">metadata.json</code>（
          <code class="rounded bg-muted px-1">python adapters/tools/gen_manifest.py …</code> 产物），
          注册后创建实例即可用平台配置表单。
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="mb-1 block text-sm font-medium">schema.json</label>
            <input type="file" accept=".json" class="w-full text-sm" @change="readJson($event, 'schema_json')" />
            <p v-if="form.schema_json" class="mt-1 truncate text-[11px] text-green-600">已载入（{{ schemaItems }} 个配置项）</p>
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium">metadata.json</label>
            <input type="file" accept=".json" class="w-full text-sm" @change="readJson($event, 'adapter_metadata')" />
            <p v-if="form.adapter_metadata" class="mt-1 truncate text-[11px] text-green-600">已载入</p>
          </div>
        </div>
      </div>
      <div class="col-span-2 flex justify-end">
        <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">
          注册构建
        </button>
      </div>
    </form>

    <!-- 构建列表 -->
    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">build_id</th>
            <th class="px-4 py-3">channel</th>
            <th class="px-4 py-3">适配器版本</th>
            <th class="px-4 py-3">上游版本</th>
            <th class="px-4 py-3">镜像</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">创建时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in builds" :key="b.build_id" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ b.build_id }}</td>
            <td class="px-4 py-3">{{ b.channel || '-' }}</td>
            <td class="px-4 py-3">{{ b.adapter_version || '-' }}</td>
            <td class="px-4 py-3">{{ b.upstream_version || '-' }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ b.artifact_image_name }}:{{ b.artifact_image_tag || '-' }}</td>
            <td class="px-4 py-3">
              <span class="rounded px-2 py-0.5 text-xs" :class="statusClass(b.status ?? 0)">
                {{ BUILD_STATUS[b.status ?? 0] ?? 'unknown' }}
              </span>
            </td>
            <td class="px-4 py-3 text-xs text-muted-foreground">{{ b.created_at ? new Date(b.created_at).toLocaleString() : '-' }}</td>
          </tr>
          <tr v-if="!builds.length">
            <td colspan="7" class="px-4 py-8 text-center text-muted-foreground">暂无构建——asset_service 中还没有该游戏的构建</td>
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

import { BUILD_STATUS, listGameBuilds, registerGameBuild, type GameBuild } from '@/api/admin'

const route = useRoute()
const gameId = route.params.gameId as string

const builds = ref<GameBuild[]>([])
const error = ref('')
// form 支持平铺字符串字段 + schema_json（JSON 字符串）+ adapter_metadata（对象）
const form = reactive<Record<string, any>>({})

// schema.json 已载入时的配置项数量（展示用）
const schemaItems = computed(() => {
  if (!form.schema_json) return 0
  try {
    return JSON.parse(form.schema_json).settings?.length ?? 0
  } catch {
    return 0
  }
})

function statusClass(s: number) {
  const st = BUILD_STATUS[s] ?? 'unknown'
  if (st === 'available') return 'bg-green-100 text-green-700'
  if (st === 'deprecated') return 'bg-yellow-100 text-yellow-700'
  if (st === 'deleted' || st === 'unavailable') return 'bg-red-100 text-red-700'
  return 'bg-muted'
}

// 读取上传的 JSON 文件：schema_json 存字符串，adapter_metadata 存对象
function readJson(e: Event, field: 'schema_json' | 'adapter_metadata') {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const text = String(reader.result ?? '')
    try {
      const parsed = JSON.parse(text)
      if (field === 'schema_json') {
        form.schema_json = text // 字符串（asset_service 校验契约）
      } else {
        form.adapter_metadata = parsed // 对象（port_inject + lifecycle）
      }
      error.value = ''
    } catch {
      error.value = 'JSON 解析失败：' + file.name
    }
  }
  reader.readAsText(file)
}

async function load() {
  error.value = ''
  try {
    builds.value = await listGameBuilds(gameId)
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载构建失败（asset_service 是否可用？）'
  }
}

async function onRegister() {
  error.value = ''
  if (!form.build_id) {
    error.value = '请填写 build_id'
    return
  }
  try {
    await registerGameBuild(gameId, { ...form })
    Object.keys(form).forEach((k) => (form[k] = ''))
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '注册失败'
  }
}

onMounted(load)
</script>

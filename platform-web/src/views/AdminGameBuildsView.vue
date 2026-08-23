<template>
  <div class="space-y-6">
    <div>
      <RouterLink to="/admin/games" class="text-sm text-muted-foreground hover:underline">← 游戏管理</RouterLink>
      <RouterLink :to="{ name: 'admin-game-platform-config', params: { gameId } }" class="ml-3 text-sm text-muted-foreground hover:underline">平台配置</RouterLink>
      <RouterLink :to="{ name: 'admin-game-credentials', params: { gameId } }" class="ml-3 text-sm text-muted-foreground hover:underline">凭证池</RouterLink>
      <h1 class="mt-1 text-2xl font-semibold">构建版本 · {{ gameId }}</h1>
      <p class="text-sm text-muted-foreground">管理游戏的资产构建版本（channel 分组、历史版本、增量迭代注册）。</p>
    </div>

    <!-- 增量迭代注册：build_id 系统生成；选择基准后只填需要更新的字段 -->
    <form class="grid max-w-3xl grid-cols-2 gap-3 rounded-lg border p-4" @submit.prevent="onRegister">
      <div class="col-span-2">
        <label class="mb-1 block text-sm font-medium">迭代基准（可选）</label>
        <select v-model="form.base_build_id" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2">
          <option value="">全新注册（无基准，需填写全部必填项）</option>
          <option v-for="b in builds" :key="b.build_id" :value="b.build_id">
            {{ b.build_id }}（{{ b.channel || '无 channel' }} · {{ statusText(b.status ?? 0) }}）
          </option>
        </select>
        <p class="mt-1 text-[11px] text-muted-foreground">
          选择基准后，未填写的字段（上游版本 / 镜像 / 适配器版本 / 配置 schema / 元数据）自动继承该版本，无需重复录入。
        </p>
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">channel</label>
        <input v-model="form.channel" type="text" :placeholder="baseBuild ? baseBuild.channel || '如 public' : '如 public'" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
        <p v-if="baseBuild" class="mt-1 text-[11px] text-muted-foreground">留空继承基准：{{ baseBuild.channel || '（无）' }}</p>
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">artifact_image_tag *</label>
        <input v-model="form.artifact_image_tag" type="text" placeholder="新版本身份，如 0.4.1" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
        <p class="mt-1 text-[11px] text-muted-foreground">新版本必须更换 tag，build_id 由系统按 game-channel-tag 生成</p>
      </div>
      <div class="col-span-2">
        <label class="mb-1 block text-sm font-medium">build_id（系统生成，只读预览）</label>
        <input :value="generatedBuildId" readonly class="w-full cursor-not-allowed rounded-md border bg-muted px-3 py-2 font-mono text-sm outline-none" />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">upstream_version（留空继承）</label>
        <input v-model="form.upstream_version" type="text" :placeholder="baseBuild?.upstream_version || '留空继承'" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">artifact_image_name（留空继承）</label>
        <input v-model="form.artifact_image_name" type="text" :placeholder="baseBuild?.artifact_image_name || '如 7daystodie-adapter'" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">adapter_version（留空继承）</label>
        <input v-model="form.adapter_version" type="text" :placeholder="baseBuild?.adapter_version || '如 0.4.0'" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div class="col-span-2">
        <label class="mb-1 block text-sm font-medium">artifact_uri（留空继承）</label>
        <input v-model="form.artifact_uri" type="text" :placeholder="baseBuild?.artifact_uri || '镜像仓库地址'" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <!-- 配置能力：不重新上传则继承基准的 schema/metadata（gen_manifest.py 产物） -->
      <div class="col-span-2 rounded-md border border-dashed p-3">
        <div class="mb-2 text-xs font-medium text-muted-foreground">
          配置能力（可选）：重新上传 <code class="rounded bg-muted px-1">schema.json</code> 与
          <code class="rounded bg-muted px-1">metadata.json</code>（
          <code class="rounded bg-muted px-1">python adapters/tools/gen_manifest.py …</code> 产物）以覆盖基准版本，
          不传则继承基准的配置能力。
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="mb-1 block text-sm font-medium">schema.json</label>
            <input type="file" accept=".json" class="w-full text-sm" @change="readJson($event, 'schema_json')" />
            <p v-if="form.schema_json" class="mt-1 truncate text-[11px] text-green-600">已载入覆盖（{{ schemaItems }} 个配置项）</p>
            <p v-else-if="baseBuild?.schema_json" class="mt-1 text-[11px] text-muted-foreground">将继承基准的 schema</p>
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium">metadata.json</label>
            <input type="file" accept=".json" class="w-full text-sm" @change="readJson($event, 'adapter_metadata')" />
            <p v-if="form.adapter_metadata" class="mt-1 truncate text-[11px] text-green-600">已载入覆盖</p>
            <p v-else-if="baseBuild?.adapter_metadata" class="mt-1 text-[11px] text-muted-foreground">将继承基准的元数据</p>
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
                {{ statusText(b.status ?? 0) }}
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
// 增量表单：空字段不提交，服务端从迭代基准继承
const form = reactive<Record<string, any>>({})

// 当前选中的迭代基准
const baseBuild = computed<GameBuild | undefined>(() => builds.value.find((b) => b.build_id === form.base_build_id))

// build_id 预览：系统按 {game_id}-{channel}-{tag} 生成
const generatedBuildId = computed(() => {
  const ch = (form.channel || baseBuild.value?.channel || '').trim()
  const tag = (form.artifact_image_tag || '').trim()
  return ch ? `${gameId}-${ch}-${tag}` : `${gameId}-${tag}`
})

// schema.json 已载入时的配置项数量（展示用）
const schemaItems = computed(() => {
  if (!form.schema_json) return 0
  try {
    return JSON.parse(form.schema_json).settings?.length ?? 0
  } catch {
    return 0
  }
})

function statusText(s: number) {
  return BUILD_STATUS[s] ?? 'unknown'
}

function statusClass(s: number) {
  const st = statusText(s)
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
        // 预检：schema 的 adapter_id 必须与当前游戏匹配（防止上传错游戏的 schema.json）
        const schemaAdapterId = parsed?.adapter_id
        const knownAdapterIds = new Set(builds.value.map((b) => b.adapter_id).filter(Boolean))
        if (typeof schemaAdapterId === 'string' && schemaAdapterId && knownAdapterIds.size > 0 && !knownAdapterIds.has(schemaAdapterId)) {
          error.value = `警告：该 schema.json 属于 "${schemaAdapterId}"，而当前游戏 ${gameId} 的构建适配器是 ${[...knownAdapterIds].join(' / ')}——请确认没选错文件`
        } else {
          error.value = ''
        }
        form.schema_json = text // 字符串（asset_service 校验契约）
      } else {
        form.adapter_metadata = parsed // 对象（port_inject + lifecycle）
        error.value = ''
      }
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

// 提交增量：只发送非空字段（schema_json/adapter_metadata 只有重新上传才携带）
function buildPayload(): Record<string, any> {
  const payload: Record<string, any> = {}
  const fields: Array<[string, string]> = [
    ['channel', form.channel],
    ['base_build_id', form.base_build_id],
    ['artifact_image_tag', form.artifact_image_tag],
    ['upstream_version', form.upstream_version],
    ['artifact_uri', form.artifact_uri],
    ['adapter_version', form.adapter_version],
    ['artifact_image_name', form.artifact_image_name],
  ]
  for (const [key, value] of fields) {
    if (typeof value === 'string' && value.trim() !== '') payload[key] = value.trim()
  }
  if (form.schema_json) payload.schema_json = form.schema_json
  if (form.adapter_metadata) payload.adapter_metadata = form.adapter_metadata
  return payload
}

async function onRegister() {
  error.value = ''
  if (!form.artifact_image_tag || !String(form.artifact_image_tag).trim()) {
    error.value = '请填写 artifact_image_tag（新版本身份）'
    return
  }
  try {
    await registerGameBuild(gameId, buildPayload())
    Object.keys(form).forEach((k) => (form[k] = ''))
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '注册失败'
  }
}

onMounted(load)
</script>

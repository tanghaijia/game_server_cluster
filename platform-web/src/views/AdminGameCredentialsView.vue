<template>
  <div class="space-y-6">
    <div>
      <RouterLink to="/admin/games" class="text-sm text-muted-foreground hover:underline">← 游戏管理</RouterLink>
      <RouterLink :to="{ name: 'admin-game-builds', params: { gameId } }" class="ml-3 text-sm text-muted-foreground hover:underline">构建版本</RouterLink>
      <RouterLink :to="{ name: 'admin-game-platform-config', params: { gameId } }" class="ml-3 text-sm text-muted-foreground hover:underline">平台配置</RouterLink>
      <h1 class="mt-1 text-2xl font-semibold">凭证池 · {{ gameId }}</h1>
      <p class="text-sm text-muted-foreground">
        外部受限凭证（如 DST 的 Klei cluster token）池化管理：实例启动时分配、停止/失败时释放复用。
      </p>
    </div>

    <!-- 录入凭证 -->
    <form class="grid max-w-3xl gap-3 rounded-lg border p-4" @submit.prevent="onCreate">
      <div>
        <label class="mb-1 block text-sm font-medium">资源类型（resource_type）</label>
        <select v-model="form.resource_type" :disabled="!credentialTypes.length" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2">
          <option v-for="t in credentialTypes" :key="t" :value="t">{{ t }}</option>
        </select>
        <p v-if="!credentialTypes.length && buildsCount === 0" class="mt-1 text-[11px] text-amber-600">
          该游戏尚无任何构建——请先到「构建版本」页注册一个构建，此处才能选择类型。
        </p>
        <p v-else-if="!credentialTypes.length" class="mt-1 text-[11px] text-amber-600">
          该游戏已有 {{ buildsCount }} 个构建，但均未携带凭证声明——请确认注册时上传的是
          <b>最新生成</b>的 metadata.json（含 credentials 数组，可用
          <code class="rounded bg-muted px-1">python adapters/tools/gen_manifest.py …</code> 重新生成），
          旧版 metadata.json 没有 credentials 字段，重新上传后迭代注册一次即可。
        </p>
        <p v-else class="mt-1 text-[11px] text-muted-foreground">
          类型来自该游戏构建的声明（不可手填，防止拼写错误录了用不上）。
        </p>
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">凭证内容（每行一个，可批量粘贴）</label>
        <textarea v-model="form.secrets_text" rows="6" placeholder="pds-g^... 每行一个" class="w-full rounded-md border px-3 py-2 font-mono text-sm outline-none focus:ring-2"></textarea>
        <p class="mt-1 text-[11px] text-muted-foreground">DST token 在 https://accounts.klei.com 的「GAME SERVERS」页创建；正式 token 不会写回页面展示。</p>
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium">备注（可选）</label>
        <input v-model="form.remark" type="text" placeholder="如：生产集群 token" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div class="flex justify-end">
        <button type="submit" :disabled="!credentialTypes.length" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50">
          录入凭证
        </button>
      </div>
    </form>

    <!-- 凭证列表 -->
    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">类型</th>
            <th class="px-4 py-3">凭证（脱敏）</th>
            <th class="px-4 py-3">状态</th>
            <th class="px-4 py-3">占用实例</th>
            <th class="px-4 py-3">备注</th>
            <th class="px-4 py-3">录入时间</th>
            <th class="px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="cred in credentials" :key="cred.id" class="border-b last:border-0">
            <td class="px-4 py-3 font-mono text-xs">{{ cred.resource_type }}</td>
            <td class="px-4 py-3 font-mono text-xs">{{ cred.secret_masked }}</td>
            <td class="px-4 py-3">
              <span class="rounded px-2 py-0.5 text-xs" :class="statusClass(cred.status)">
                {{ statusText(cred.status) }}
              </span>
            </td>
            <td class="px-4 py-3 font-mono text-xs">{{ cred.instance_id || '-' }}</td>
            <td class="px-4 py-3 text-xs text-muted-foreground">{{ cred.remark || '-' }}</td>
            <td class="px-4 py-3 text-xs text-muted-foreground">{{ cred.create_time ? new Date(cred.create_time).toLocaleString() : '-' }}</td>
            <td class="px-4 py-3">
              <button v-if="cred.status !== 'available'" @click="onForceRelease(cred)" class="mr-2 text-xs text-blue-600 hover:underline">
                {{ cred.status === 'orphan' ? '强制释放' : '释放' }}
              </button>
              <button @click="onDelete(cred)" class="text-xs text-red-600 hover:underline">删除</button>
            </td>
          </tr>
          <tr v-if="!credentials.length">
            <td colspan="7" class="px-4 py-8 text-center text-muted-foreground">暂无凭证——录入后实例启动时才能分配</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'

import {
  createCredentials, deleteCredential, forceReleaseCredential, listCredentialTypes,
  listCredentials,
  type Credential,
} from '@/api/admin'
import { listGameBuilds } from '@/api/admin'

const route = useRoute()
const gameId = route.params.gameId as string

const credentials = ref<Credential[]>([])
const credentialTypes = ref<string[]>([])
const buildsCount = ref(0)
const error = ref('')
const form = reactive({ resource_type: '', secrets_text: '', remark: '' })

function statusText(s: string) {
  return { available: 'available', in_use: 'in_use', orphan: 'orphan' }[s] ?? s
}

function statusClass(s: string) {
  if (s === 'available') return 'bg-green-100 text-green-700'
  if (s === 'in_use') return 'bg-blue-100 text-blue-700'
  if (s === 'orphan') return 'bg-red-100 text-red-700'
  return 'bg-muted'
}

async function load() {
  error.value = ''
  try {
    const [rows, types, builds] = await Promise.all([
      listCredentials(gameId),
      listCredentialTypes(gameId),
      listGameBuilds(gameId),
    ])
    credentials.value = rows
    credentialTypes.value = types
    buildsCount.value = builds.length
    if (!form.resource_type && types.length) form.resource_type = types[0]
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载凭证失败'
  }
}

async function onCreate() {
  error.value = ''
  const secrets = form.secrets_text.split('\n').map((s) => s.trim()).filter(Boolean)
  if (!form.resource_type) {
    error.value = '请选择资源类型（需先注册带凭证声明的构建）'
    return
  }
  if (!secrets.length) {
    error.value = '请至少粘贴一个凭证'
    return
  }
  try {
    const n = await createCredentials(gameId, form.resource_type, secrets, form.remark)
    form.secrets_text = ''
    form.remark = ''
    error.value = `已录入 ${n} 个凭证`
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '录入失败'
  }
}

async function onForceRelease(cred: Credential) {
  error.value = ''
  try {
    await forceReleaseCredential(gameId, cred.id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '释放失败'
  }
}

async function onDelete(cred: Credential) {
  error.value = ''
  if (!window.confirm(`删除凭证 ${cred.secret_masked}？`)) return
  try {
    await deleteCredential(gameId, cred.id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '删除失败'
  }
}

onMounted(load)
</script>

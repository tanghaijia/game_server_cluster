<template>
  <div class="space-y-6">
    <!-- 头部 -->
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold">实例文件</h1>
        <p class="text-sm text-muted-foreground">{{ clientId }} · 数据目录 /data</p>
      </div>
      <button class="rounded-md border px-3 py-2 text-sm hover:bg-muted" @click="load">刷新</button>
    </div>

    <!-- 运行中提示 -->
    <div v-if="runningHint" class="rounded-md border border-yellow-200 bg-yellow-50 px-4 py-3 text-sm text-yellow-800">
      实例正在运行：修改配置文件可能需要重启实例才生效；正在被游戏写入的文件请勿编辑。
    </div>

    <!-- 工具栏 -->
    <div class="flex flex-wrap items-center gap-3 rounded-lg border p-4">
      <!-- 面包屑 -->
      <div class="flex flex-1 flex-wrap items-center gap-1 text-sm">
        <button class="rounded px-2 py-1 hover:bg-muted" @click="cd('')">/data</button>
        <template v-for="(seg, i) in segments" :key="i">
          <span class="text-muted-foreground">/</span>
          <button class="rounded px-2 py-1 hover:bg-muted" @click="cd(join([...crumbs.slice(0, i + 1)]))">{{ seg }}</button>
        </template>
      </div>
      <input
        v-model="newDir"
        placeholder="新目录名"
        class="w-40 rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2"
        @keyup.enter="onMkdir"
      />
      <button class="rounded-md border px-3 py-1.5 text-sm hover:bg-muted" @click="onMkdir">新建目录</button>
      <button class="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:opacity-90" @click="pickFiles">
        上传文件
      </button>
      <input ref="fileInput" type="file" multiple class="hidden" @change="onFilesPicked" />
    </div>

    <!-- 文件表格 -->
    <div class="rounded-lg border">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="px-4 py-3">名称</th>
            <th class="w-28 px-4 py-3">大小</th>
            <th class="w-44 px-4 py-3">修改时间</th>
            <th class="w-64 px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in entries" :key="e.name" class="border-b last:border-0">
            <td class="px-4 py-2.5">
              <button v-if="e.is_dir" class="font-medium hover:underline" @click="cd(cpath(e.name))">📁 {{ e.name }}</button>
              <span v-else>📄 {{ e.name }}</span>
            </td>
            <td class="px-4 py-2.5 text-xs text-muted-foreground">{{ e.is_dir ? '-' : fmtSize(e.size) }}</td>
            <td class="px-4 py-2.5 text-xs text-muted-foreground">{{ fmtTime(e.modified) }}</td>
            <td class="px-4 py-2.5 space-x-2 text-xs">
              <template v-if="!e.is_dir">
                <button class="rounded border px-2 py-1 hover:bg-muted" @click="onDownload(e.name)">下载</button>
                <button class="rounded border px-2 py-1 hover:bg-muted" @click="onEdit(e.name)">编辑</button>
              </template>
              <button class="rounded border px-2 py-1 hover:bg-muted" @click="onRename(e.name)">重命名</button>
              <button class="rounded border px-2 py-1 text-red-500 hover:bg-muted" @click="onDelete(e.name)">删除</button>
            </td>
          </tr>
          <tr v-if="!entries.length">
            <td colspan="4" class="px-4 py-8 text-center text-muted-foreground">空目录</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 上传进度 -->
    <div v-for="u in uploads" :key="u.name" class="rounded-lg border px-4 py-2">
      <div class="mb-1 flex justify-between text-xs">
        <span class="truncate">{{ u.name }}</span>
        <span>{{ u.pct }}%</span>
      </div>
      <div class="h-1.5 w-full rounded bg-muted">
        <div class="h-full rounded bg-primary transition-all" :style="{ width: u.pct + '%' }"></div>
      </div>
    </div>

    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>

    <!-- 文本编辑弹窗 -->
    <div v-if="editing" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-6" @click.self="editing = null">
      <div class="flex h-[70vh] w-full max-w-3xl flex-col rounded-lg bg-card shadow-lg">
        <div class="flex items-center justify-between border-b px-4 py-3">
          <div class="truncate text-sm font-medium">{{ editingPath }} <span class="ml-2 text-xs text-muted-foreground">(≤2MB)</span></div>
          <div class="space-x-2">
            <button class="rounded-md border px-3 py-1 text-sm hover:bg-muted" @click="editing = null">取消</button>
            <button class="rounded-md bg-primary px-3 py-1 text-sm font-medium text-primary-foreground hover:opacity-90" :disabled="saving" @click="onSaveText">
              {{ saving ? '保存中...' : '保存' }}
            </button>
          </div>
        </div>
        <textarea
          v-model="editContent"
          class="flex-1 resize-none bg-background p-4 font-mono text-sm outline-none"
          spellcheck="false"
        ></textarea>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'

import { FileClient, fetchAdminSession, fetchUserSession, type FileEntry } from '@/api/files'

const route = useRoute()

// 路由两种形态：/my-servers/:orderId/files（用户） /admin/instances/:instanceId/files（管理员）
const isAdmin = computed(() => route.name === 'admin-instance-files')
const clientId = computed(() => (route.params.orderId as string) || (route.params.instanceId as string))

let client: FileClient
function buildClient(): FileClient {
  const ref = clientId.value
  return isAdmin.value ? new FileClient(fetchAdminSession, ref) : new FileClient(fetchUserSession, ref)
}

const entries = ref<FileEntry[]>([])
const cwd = ref('') // 当前路径（data 根内相对路径）
const newDir = ref('')
const fileInput = ref<HTMLInputElement>()
const uploads = ref<{ name: string; pct: number }[]>([])
const error = ref('')
const editing = ref<string | null>(null)
const editingPath = ref('')
const editContent = ref('')
const saving = ref(false)

const crumbs = computed(() => cwd.value.split('/').filter(Boolean))
const segments = computed(() => crumbs.value)
const runningHint = computed(() => route.query.running === '1')

const join = (parts: string[]) => parts.join('/')
const cpath = (name: string) => (cwd.value ? cwd.value + '/' + name : name)
const fmtSize = (n: number) => (n >= 1048576 ? (n / 1048576).toFixed(1) + ' MB' : n >= 1024 ? (n / 1024).toFixed(1) + ' KB' : n + ' B')
const fmtTime = (t: string) => new Date(t).toLocaleString()

async function load() {
  error.value = ''
  try {
    const data = await client.list(cwd.value)
    entries.value = data.entries
  } catch (e: any) {
    error.value = e?.message ?? '加载失败'
  }
}

function cd(path: string) {
  cwd.value = path
  load()
}

function pickFiles() {
  fileInput.value?.click()
}

async function onFilesPicked(e: Event) {
  const files = Array.from((e.target as HTMLInputElement).files ?? [])
  ;(e.target as HTMLInputElement).value = ''
  for (const f of files) {
    uploads.value.push({ name: f.name, pct: 0 })
  }
  // 逐文件上传
  for (const f of files) {
    const item = uploads.value.find((u) => u.name === f.name)!
    try {
      await client.upload(cpath(f.name), f, (pct) => (item.pct = pct))
    } catch (err: any) {
      if (err?.status === 401 || err?.status === 403) {
        // token 过期：刷新会话后重试一次
        client = buildClient()
        await client.upload(cpath(f.name), f, (pct) => (item.pct = pct))
      } else {
        error.value = '上传 ' + f.name + ' 失败: ' + (err?.message ?? '')
      }
    }
  }
  setTimeout(() => {
    uploads.value = []
    load()
  }, 500)
}

async function onMkdir() {
  const name = newDir.value.trim()
  if (!name) return
  try {
    await client.mkdir(cpath(name))
    newDir.value = ''
    load()
  } catch (e: any) {
    error.value = e?.message ?? '创建失败'
  }
}

async function onRename(name: string) {
  const to = prompt('重命名为：', name)
  if (!to || to === name) return
  try {
    await client.rename(cpath(name), cpath(to))
    load()
  } catch (e: any) {
    error.value = e?.message ?? '重命名失败'
  }
}

async function onDelete(name: string) {
  if (!confirm('确认删除 ' + name + '？')) return
  try {
    await client.del(cpath(name))
    load()
  } catch (e: any) {
    error.value = e?.message ?? '删除失败'
  }
}

async function onDownload(name: string) {
  try {
    const url = await client.downloadUrl(cpath(name))
    const a = document.createElement('a')
    a.href = url
    a.download = name
    a.click()
  } catch (e: any) {
    error.value = e?.message ?? '下载失败'
  }
}

async function onEdit(name: string) {
  try {
    const content = await client.readText(cpath(name))
    editingPath.value = cpath(name)
    editContent.value = content
    editing.value = name
  } catch (e: any) {
    error.value = e?.message ?? '读取失败（非文本或超过 2MB？）'
  }
}

async function onSaveText() {
  saving.value = true
  error.value = ''
  try {
    await client.writeText(editingPath.value, editContent.value)
    editing.value = null
  } catch (e: any) {
    error.value = e?.message ?? '保存失败'
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  client = buildClient()
  load()
})
</script>

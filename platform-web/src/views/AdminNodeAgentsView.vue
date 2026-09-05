<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">NodeAgent 管理</h1>
      <p class="text-sm text-muted-foreground">
        节点代理：只有 Enabled 的 agent 参与实例调度与缓存循环。
        一键更新依赖节点由 systemd 托管（<span class="font-mono">node_agent.service</span>），见部署说明。
      </p>
    </div>

    <form class="flex max-w-2xl items-end gap-3 rounded-lg border p-4" @submit.prevent="onCreate">
      <div class="flex-1">
        <label class="mb-1 block text-sm font-medium">名称</label>
        <input v-model="form.name" type="text" placeholder="如 node-agent-1" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div class="w-40">
        <label class="mb-1 block text-sm font-medium">节点 ID</label>
        <input v-model="form.nodeId" type="text" placeholder="对应节点 Id" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <div class="w-32">
        <label class="mb-1 block text-sm font-medium">端口</label>
        <input v-model.number="form.port" type="number" placeholder="9090" class="w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2" />
      </div>
      <button type="submit" class="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90">新增</button>
    </form>

    <!-- 上传新版本（P1/P5：发布清单 + 一键更新入口） -->
    <div class="rounded-lg border">
      <div class="border-b px-4 py-3 text-sm font-medium">发布新版本</div>
      <div class="p-4">
        <div class="flex flex-wrap items-end gap-3">
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">版本号（如 v0.1.1）</label>
            <input v-model="relForm.version" type="text" placeholder="v0.1.1" class="w-36 rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">OS</label>
            <select v-model="relForm.os" class="rounded-md border px-3 py-1.5 text-sm">
              <option value="linux">linux</option>
              <option value="windows">windows</option>
            </select>
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">架构</label>
            <select v-model="relForm.arch" class="rounded-md border px-3 py-1.5 text-sm">
              <option value="amd64">amd64</option>
              <option value="arm64">arm64</option>
            </select>
          </div>
          <div class="min-w-[280px] flex-1">
            <label class="mb-1 block text-xs text-muted-foreground">二进制文件</label>
            <div class="flex items-center gap-2">
              <button
                type="button"
                class="rounded-md border px-3 py-1.5 text-sm hover:bg-muted disabled:cursor-not-allowed disabled:opacity-40"
                :disabled="busy"
                @click="onClickPickFile"
              >
                选择文件…
              </button>
              <span v-if="relForm.file" class="truncate font-mono text-xs text-foreground" :title="relForm.file.name">
                ✓ {{ relForm.file.name }}（{{ formatSize(relForm.file.size) }}）
              </span>
              <span v-else class="text-xs text-muted-foreground">未选择</span>
            </div>
            <!-- 原生 file input 隐藏，由按钮触发；每次打开前清空 value 以支持重复选择同一文件 -->
            <input ref="relFileInput" type="file" class="hidden" @change="onPickFile" />
          </div>
          <div>
            <label class="mb-1 block text-xs text-muted-foreground">备注（可选）</label>
            <input v-model="relForm.note" type="text" placeholder="本次更新内容" class="w-48 rounded-md border px-3 py-1.5 text-sm outline-none focus:ring-2" />
          </div>
          <button
            class="rounded-md bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:opacity-90 disabled:opacity-40"
            :disabled="busy || !relForm.file || !relForm.version"
            @click="onUploadRelease"
          >
            上传
          </button>
        </div>
        <div class="mt-2 flex flex-wrap gap-2 text-[11px] text-muted-foreground">
          <span v-if="releases.length" class="font-medium text-foreground">已发布：</span>
          <span v-for="r in releases" :key="r.ID" class="rounded bg-muted px-1.5 py-0.5 font-mono">
            {{ r.Version }} {{ r.OS }}/{{ r.Arch }}
            <span class="text-muted-foreground/70">· {{ formatSize(r.SizeBytes) }} · {{ (r.SHA256 || '').slice(0, 8) }}…</span>
          </span>
          <span v-if="!releases.length" class="text-muted-foreground">暂无发布版本</span>
        </div>
      </div>
    </div>

    <div class="rounded-lg border">
      <div class="flex items-center justify-between border-b px-4 py-3">
        <span class="text-sm font-medium">节点代理（{{ agents.length }}）</span>
        <div class="flex items-center gap-2">
          <button
            class="rounded-md border px-3 py-1 text-xs hover:bg-muted disabled:opacity-40"
            :disabled="busy || !selectedAgents.length || !selectedReleaseId"
            :title="selectedAgents.length && selectedReleaseId ? '滚动更新选中的 node_agent（跳过有运行实例的节点）' : '请先勾选节点并选择目标版本'"
            @click="onBatchUpdate"
          >
            批量更新选中（{{ selectedAgents.length }}）
          </button>
          <select v-model="selectedReleaseId" class="rounded border bg-transparent px-2 py-1 text-xs">
            <option value="">目标版本…</option>
            <option v-for="r in releases" :key="r.ID" :value="r.ID">{{ r.Version }} ({{ r.OS }}/{{ r.Arch }})</option>
          </select>
          <button class="rounded-md border px-3 py-1 text-xs hover:bg-muted" @click="load">刷新</button>
        </div>
      </div>
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-muted-foreground">
            <th class="w-8 px-2 py-3"></th>
            <th class="px-2 py-3">ID</th>
            <th class="px-2 py-3">节点 ID</th>
            <th class="px-2 py-3">端口</th>
            <th class="px-2 py-3">状态</th>
            <th class="px-2 py-3">存活</th>
            <th class="px-2 py-3" title="心跳上报的当前版本 / 目标发布版本">版本</th>
            <th class="px-2 py-3" title="更新状态机：idle/downloading/rebooting/updated/failed">更新状态</th>
            <th class="px-2 py-3">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in agents" :key="a.ID" class="border-b last:border-0">
            <td class="px-2 py-3">
              <input type="checkbox" v-model="selectedIds" :value="a.ID" class="h-3.5 w-3.5 rounded border" :disabled="busy || !canSelect(a)" />
            </td>
            <td class="px-2 py-3 font-mono text-xs">{{ a.ID }}</td>
            <td class="px-2 py-3">{{ a.NodeId || '-' }}</td>
            <td class="px-2 py-3">{{ a.Port }}</td>
            <td class="px-2 py-3">
              <span class="rounded px-2 py-0.5 text-xs" :class="a.Status === 1 ? 'bg-primary text-primary-foreground' : 'bg-muted'">
                {{ a.Status === 1 ? 'Enabled' : 'Disabled' }}
              </span>
            </td>
            <td class="px-2 py-3">
              <span
                v-if="a.Status === 1"
                class="rounded px-2 py-0.5 text-xs"
                :class="a.Alive ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'"
              >
                {{ a.Alive ? '存活' : '失联' }}
              </span>
              <span v-else class="text-xs text-muted-foreground">-</span>
              <div v-if="a.Alive && a.LastHeartbeatAt" class="mt-0.5 text-xs text-muted-foreground">
                心跳 {{ fmtTime(a.LastHeartbeatAt) }}
              </div>
            </td>
            <td class="px-2 py-3 text-xs">
              <span class="font-mono">{{ a.AgentVersion || '-' }}</span>
              <span v-if="a.UpdateState === 'downloading' || a.UpdateState === 'rebooting' || (a.UpdateState === 'updated' && a.TargetVersion && a.TargetVersion !== a.AgentVersion)" class="ml-1 text-muted-foreground">
                → {{ a.TargetVersion }}
              </span>
            </td>
            <td class="px-2 py-3 text-xs">
              <span
                class="rounded px-1.5 py-0.5"
                :class="updateStateClass(a.UpdateState)"
                :title="a.UpdateState === 'failed' ? a.LastUpdateErr || '更新失败' : undefined"
              >
                {{ AGENT_UPDATE_STATE_LABELS[a.UpdateState ?? 'idle'] ?? a.UpdateState }}
              </span>
              <div v-if="a.UpdateState === 'failed' && a.LastUpdateErr" class="mt-0.5 max-w-[220px] truncate text-[11px] text-red-500" :title="a.LastUpdateErr">
                {{ a.LastUpdateErr }}
              </div>
            </td>
            <td class="px-2 py-3">
              <div class="flex flex-wrap items-center gap-1">
                <button
                  v-if="a.Status === 1"
                  class="rounded-md border px-2 py-1 text-xs hover:bg-muted"
                  @click="onToggle(a, false)"
                >停用</button>
                <button
                  v-else
                  class="rounded-md bg-primary px-2 py-1 text-xs text-primary-foreground hover:opacity-90"
                  @click="onToggle(a, true)"
                >启用</button>
                <button
                  class="rounded-md border px-2 py-1 text-xs hover:bg-muted"
                  :disabled="!a.NodeId"
                  :title="a.NodeId ? '查看该 node_agent 运行日志' : '该 agent 未绑定节点'"
                  @click="openLogs(a)"
                >日志</button>
                <button
                  class="rounded-md border px-2 py-1 text-xs hover:bg-muted disabled:opacity-40"
                  :disabled="busy || !latestRelease || !canUpdate(a)"
                  :title="updateBtnTitle(a)"
                  @click="onUpdateOne(a)"
                >更新</button>
                <button
                  v-if="a.UpdateState === 'failed' || (a.AgentVersion && latestRelease && a.AgentVersion !== latestRelease.Version)"
                  class="rounded-md border px-2 py-1 text-xs text-amber-700 hover:bg-muted disabled:opacity-40"
                  :disabled="busy || !latestRelease"
                  :title="latestRelease ? '回滚到最近的已发布版本' : '无可用发布版本'"
                  @click="onRollback(a)"
                >回滚</button>
              </div>
            </td>
          </tr>
          <tr v-if="!agents.length">
            <td colspan="9" class="px-4 py-8 text-center text-muted-foreground">暂无 node_agent</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
    <p v-if="notice" class="text-sm text-green-600">{{ notice }}</p>

    <!-- 更新确认弹窗 -->
    <div v-if="confirmTargets.length" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="confirmTargets = []">
      <div class="w-[520px] rounded-lg border bg-background p-5">
        <h2 class="text-base font-semibold">确认更新 {{ confirmTargets.length }} 个 node_agent？</h2>
        <ul class="mt-2 max-h-40 space-y-1 overflow-auto text-sm text-muted-foreground">
          <li v-for="t in confirmTargets" :key="t.agent_id" class="font-mono text-xs">
            {{ t.agent_id }} → {{ targetLabel }}
          </li>
        </ul>
        <p class="mt-3 text-xs text-amber-700">
          更新期间每个 agent 将下载新二进制、重启并短暂失联（约 10~60s）；有运行实例的节点会被自动跳过。
          请确保节点已由 systemd 托管（Restart=always），否则重启后不会被自动拉起。
        </p>
        <div class="mt-4 flex justify-end gap-2">
          <button class="rounded-md border px-4 py-1.5 text-sm hover:bg-muted" @click="confirmTargets = []">取消</button>
          <button class="rounded-md bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:opacity-90" :disabled="busy" @click="doUpdate">
            确认更新
          </button>
        </div>
      </div>
    </div>

    <!-- 日志查看弹层 -->
    <AgentLogsDialog v-if="logAgent" :agent="logAgent" @close="logAgent = null" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'

import {
  AGENT_UPDATE_STATE_LABELS,
  batchUpdateNodeAgents,
  createNodeAgent,
  listAgentReleases,
  listNodeAgents,
  rollbackNodeAgent,
  setNodeAgentEnabled,
  type AgentRelease,
  type NodeAgent,
} from '@/api/admin'
import AgentLogsDialog from './AgentLogsDialog.vue'

const agents = ref<NodeAgent[]>([])
const releases = ref<AgentRelease[]>([])
const error = ref('')
const notice = ref('')
const busy = ref(false)
const logAgent = ref<NodeAgent | null>(null)
const selectedIds = ref<string[]>([])
const selectedReleaseId = ref('')
const confirmTargets = ref<Array<{ agent_id: string }>>([])
let timer: number | undefined

const fmtTime = (t?: string) => (t ? new Date(t).toLocaleString() : '-')
const form = reactive({ name: '', nodeId: '', port: 9090 })
const relForm = reactive<{ version: string; os: string; arch: string; note: string; file?: File | null }>({
  version: '',
  os: 'linux',
  arch: 'amd64',
  note: '',
  file: null,
})
const relFileInput = ref<HTMLInputElement | null>(null)

function formatSize(n?: number) {
  if (!n) return '0B'
  if (n > 1e9) return (n / 1e9).toFixed(1) + 'GB'
  if (n > 1e6) return (n / 1e6).toFixed(1) + 'MB'
  return (n / 1e3).toFixed(0) + 'KB'
}

const selectedAgents = computed(() => agents.value.filter((a) => selectedIds.value.includes(a.ID)))
const latestRelease = computed<AgentRelease | undefined>(() => {
  // 与列表版本比较需要排序（按上传时间即可，视为最新发布）
  return releases.value.length ? releases.value[0] : undefined
})
const targetLabel = computed(() => {
  const r = releases.value.find((x) => x.ID === selectedReleaseId.value)
  return r ? r.Version : '-'
})

function canSelect(a: NodeAgent) {
  return !!a.NodeId && a.Alive
}
function canUpdate(a: NodeAgent) {
  if (!a.NodeId || !a.Alive) return false
  if (a.UpdateState && ['downloading', 'rebooting'].includes(a.UpdateState)) return false
  return !!latestRelease.value && latestRelease.value.Version !== a.AgentVersion
}
function updateBtnTitle(a: NodeAgent) {
  if (!latestRelease.value) return '请先发布一个版本'
  if (a.UpdateState && ['downloading', 'rebooting'].includes(a.UpdateState)) return '更新进行中'
  if (!a.Alive || !a.NodeId) return 'agent 失联或未绑定节点'
  if (latestRelease.value.Version === a.AgentVersion) return '已是最新版本'
  return '更新到 ' + latestRelease.value.Version
}

function updateStateClass(s?: string) {
  const map: Record<string, string> = {
    idle: 'bg-muted text-muted-foreground',
    downloading: 'bg-blue-100 text-blue-700',
    rebooting: 'bg-amber-100 text-amber-700',
    updated: 'bg-green-100 text-green-700',
    failed: 'bg-red-100 text-red-700',
  }
  return map[s ?? 'idle'] ?? 'bg-muted'
}

async function load() {
  error.value = ''
  try {
    const [a, r] = await Promise.all([listNodeAgents(), listAgentReleases()])
    agents.value = a
    releases.value = r
    // 若已选 release 被删，清空
    if (selectedReleaseId.value && !r.some((x) => x.ID === selectedReleaseId.value)) selectedReleaseId.value = ''
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '加载失败（controller 是否已启动？）'
  }
}

function onPickFile(e: Event) {
  const input = e.target as HTMLInputElement
  relForm.file = input.files?.[0] || null
}

// 显式打开文件选择器：先清空 value，保证再次选择同一个文件也能触发 @change
function onClickPickFile() {
  const el = relFileInput.value
  if (!el) return
  el.value = ''
  el.click()
}

function resetFileInput() {
  const el = relFileInput.value
  if (el) el.value = ''
  relForm.file = null
}

async function onUploadRelease() {
  if (!relForm.file) {
    error.value = '请选择二进制文件'
    return
  }
  if (!/^v\d+\.\d+\.\d+/.test(relForm.version)) {
    error.value = '版本号格式应为 vX.Y.Z（如 v0.1.1）'
    return
  }
  busy.value = true
  error.value = ''
  try {
    // 上传走长超时（大文件）
    const { uploadAgentRelease } = await import('@/api/admin')
    await uploadAgentRelease({
      version: relForm.version,
      os: relForm.os,
      arch: relForm.arch,
      note: relForm.note,
      file: relForm.file,
    })
    relForm.version = ''
    relForm.note = ''
    resetFileInput() // 清空已选文件 + 原生 input value（下次可再选同一文件）
    notice.value = '版本已发布'
    setTimeout(() => (notice.value = ''), 4000)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '上传失败'
  } finally {
    busy.value = false
  }
}

async function onCreate() {
  error.value = ''
  if (!form.name) {
    error.value = '请填写名称'
    return
  }
  try {
    await createNodeAgent({ name: form.name, node_id: form.nodeId || undefined, port: form.port || undefined })
    form.name = ''
    form.nodeId = ''
    form.port = 9090
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '新增失败'
  }
}

async function onToggle(a: NodeAgent, enabled: boolean) {
  error.value = ''
  try {
    await setNodeAgentEnabled(a.ID, enabled)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '操作失败'
  }
}

function openLogs(a: NodeAgent) {
  error.value = ''
  logAgent.value = a
}

// ---- 更新/回滚 ----

function onUpdateOne(a: NodeAgent) {
  if (!latestRelease.value) return
  confirmTargets.value = [{ agent_id: a.ID }]
}

function onBatchUpdate() {
  if (!selectedAgents.value.length || !selectedReleaseId.value) return
  confirmTargets.value = selectedAgents.value.map((a) => ({ agent_id: a.ID }))
}

async function doUpdate() {
  if (!selectedReleaseId.value) {
    // 单行更新未选版本时：用最新发布
    if (!latestRelease.value) {
      error.value = '无可用发布版本'
      confirmTargets.value = []
      return
    }
    selectedReleaseId.value = latestRelease.value.ID
  }
  busy.value = true
  error.value = ''
  try {
    const results = await batchUpdateNodeAgents(
      confirmTargets.value.map((t) => ({ agent_id: t.agent_id, release_id: selectedReleaseId.value })),
    )
    confirmTargets.value = []
    summarize(results)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '更新请求失败'
  } finally {
    busy.value = false
  }
}

async function onRollback(a: NodeAgent) {
  if (!latestRelease.value) return
  busy.value = true
  error.value = ''
  try {
    const result = await rollbackNodeAgent(a.ID, latestRelease.value.ID)
    summarize([result])
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? '回滚失败'
  } finally {
    busy.value = false
  }
}

function summarize(results: Array<{ agent_id?: string; ok?: boolean; skipped?: boolean; reason?: string }>) {
  const ok = results.filter((r) => r.ok || r.skipped)
  const bad = results.filter((r) => !r.ok && !r.skipped)
  if (bad.length) {
    error.value = bad.map((r) => `${r.agent_id}: ${r.reason ?? '失败'}`).join('；')
  } else {
    notice.value = '全部处理完成（含跳过项）'
    setTimeout(() => (notice.value = ''), 5000)
  }
  if (ok.some((r) => r.ok)) {
    notice.value = '更新已受理，节点重启后自动完成'
    setTimeout(() => (notice.value = ''), 6000)
  }
}

onMounted(() => {
  load()
  timer = window.setInterval(load, 5000)
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

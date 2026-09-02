<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" @click.self="$emit('close')">
    <div class="flex h-[70vh] w-[900px] max-w-[95vw] flex-col rounded-lg border bg-background">
      <!-- 头部 -->
      <div class="flex items-center justify-between border-b px-4 py-3">
        <div>
          <div class="text-sm font-medium">node_agent 日志 · <span class="font-mono">{{ agent.ID }}</span></div>
          <div class="text-xs text-muted-foreground">
            直连 agent 日志服务；仅 admin 可见 · token 短效自动续签
          </div>
        </div>
        <button class="text-sm text-muted-foreground hover:underline" @click="$emit('close')">关闭</button>
      </div>

      <!-- 工具条 -->
      <div class="flex flex-wrap items-center gap-3 border-b px-4 py-2.5 text-xs">
        <label class="flex items-center gap-1.5">
          <span class="text-muted-foreground">级别</span>
          <select v-model="level" class="rounded border bg-transparent px-2 py-1" @change="onFilterChange">
            <option value="">全部</option>
            <option value="info">INFO+</option>
            <option value="warn">WARN+</option>
            <option value="error">ERROR</option>
          </select>
        </label>
        <label class="flex items-center gap-1.5">
          <span class="text-muted-foreground">关键词</span>
          <input
            v-model="keyword"
            type="text"
            placeholder="如 cache / error / build"
            class="w-44 rounded border bg-transparent px-2 py-1 outline-none focus:ring-2"
            @keyup.enter="onFilterChange"
          />
        </label>
        <label class="flex items-center gap-1.5">
          <span class="text-muted-foreground">行数</span>
          <select v-model="lineCount" class="rounded border bg-transparent px-2 py-1" @change="onFilterChange">
            <option :value="100">100</option>
            <option :value="300">300</option>
            <option :value="1000">1000</option>
            <option :value="2000">2000</option>
          </select>
        </label>
        <label class="flex items-center gap-1.5">
          <input v-model="autoRefresh" type="checkbox" class="h-3.5 w-3.5 rounded border" />
          <span class="text-muted-foreground">自动刷新（3s）</span>
        </label>
        <button class="rounded border px-2.5 py-1 hover:bg-muted" @click="reload">立即刷新</button>

        <span v-if="status" class="ml-auto text-muted-foreground">{{ status }}</span>
      </div>

      <!-- 日志正文 -->
      <div class="flex-1 overflow-auto bg-black/90 p-3 font-mono text-xs leading-relaxed">
        <div v-if="!lines.length && !error" class="py-10 text-center text-zinc-500">暂无日志行（等待 agent 写入或调整过滤）</div>
        <div v-for="(l, i) in lines" :key="i" class="whitespace-pre-wrap break-all" :class="lineClass(l)">
          {{ l }}
        </div>
        <div v-if="truncated" class="mt-2 text-amber-400">… 日志较长，仅展示最近 {{ lineCount }} 行（含更早日志被截断）</div>
      </div>

      <!-- 错误 -->
      <div v-if="error" class="border-t px-4 py-2 text-xs text-red-500">{{ error }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'

import type { NodeAgent } from '@/api/admin'
import { AgentLogClient } from '@/api/agentLogs'

const props = defineProps<{ agent: NodeAgent }>()
defineEmits<{ close: [] }>()

const client = new AgentLogClient(props.agent.ID)
const lines = ref<string[]>([])
const level = ref('')
const keyword = ref('')
const lineCount = ref(300)
const autoRefresh = ref(true)
const error = ref('')
const status = ref('')
const truncated = ref(false)
let offset: number | undefined
let timer: number | undefined
let busy = false

function lineClass(l: string) {
  if (l.includes('[ERROR]')) return 'text-red-400'
  if (l.includes('[WARN]')) return 'text-yellow-400'
  if (l.includes('[DEBUG]')) return 'text-zinc-400'
  return 'text-zinc-200'
}

async function tail(reset: boolean) {
  if (busy) return
  busy = true
  try {
    const data = await client.tail(lineCount.value, level.value, keyword.value, reset ? undefined : offset)
    if (reset) {
      lines.value = data.text ? data.text.split('\n').filter((x) => x.length) : []
      truncated.value = !!data.truncated
    } else if (data.text) {
      lines.value.push(...data.text.split('\n').filter((x) => x.length))
      if (lines.value.length > 4000) lines.value = lines.value.slice(-4000)
      truncated.value = truncated.value || !!data.truncated
    }
    offset = data.offset
    error.value = ''
    status.value = '更新于 ' + new Date().toLocaleTimeString('zh-CN', { hour12: false })
  } catch (e: any) {
    if (reset) {
      error.value = e?.message ?? e?.response?.data?.error ?? '读取日志失败（agent 是否可达？）'
      if (e?.status === 401 || e?.status === 403) error.value = '日志会话签发失败（权限不足或 controller 不可达）'
    }
  } finally {
    busy = false
  }
}

function onFilterChange() {
  offset = undefined
  tail(true)
}

function reload() {
  offset = undefined
  tail(true)
}

onMounted(() => {
  tail(true)
  timer = window.setInterval(() => {
    if (autoRefresh.value) tail(false)
  }, 3000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

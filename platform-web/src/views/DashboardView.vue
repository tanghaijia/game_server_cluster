<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">仪表盘</h1>
      <p class="text-muted-foreground text-sm">平台概览占位页——实例、节点、订单、账单统计将在此展示。</p>
    </div>
    <div class="grid grid-cols-1 gap-4 md:grid-cols-4">
      <div v-for="card in cards" :key="card.label" class="rounded-lg border p-4">
        <p class="text-sm text-muted-foreground">{{ card.label }}</p>
        <p class="mt-1 text-2xl font-semibold">{{ card.value }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
const cards = [
  { label: '运行中实例', value: '--' },
  { label: '节点数', value: '--' },
  { label: '本月订单', value: '--' },
  { label: '本月营收（元）', value: '--' },
]
</script>

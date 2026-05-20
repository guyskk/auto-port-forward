<script setup lang="ts">
import { computed, ref } from 'vue'
import { NSpace, NSelect, NButton, NEmpty, NText, NIcon } from 'naive-ui'
import PortTable from '../components/PortTable.vue'
import { useAppStore } from '../store/state'
import { zh } from '../i18n/zh'

const store = useAppStore()

const serverOptions = computed(() =>
  store.servers.map((s) => ({ label: `${s.name || s.host} (${s.host})`, value: s.id })),
)
const selectedServer = ref<string | null>(null)

const filtered = computed(() => {
  if (!selectedServer.value) return store.forwards
  return store.forwards.filter((f) => f.server_id === selectedServer.value)
})

const lastScanLabel = computed(() => {
  if (!store.lastScanAt) return zh.monitor.never
  return new Date(store.lastScanAt).toLocaleTimeString('zh-CN')
})

async function onScan() {
  await store.scanNow()
}
async function onStart() {
  // 在 wails 环境下直接调；mock 模式 noop。
  await window.go?.main?.App?.StartAll?.()
}
async function onStop() {
  await window.go?.main?.App?.StopAll?.()
}
async function onToggle(serverId: string, port: number, on: boolean) {
  await window.go?.main?.App?.ToggleForward?.(serverId, port, on)
  await store.scanNow()
}
</script>

<template>
  <n-space vertical :size="16">
    <n-space justify="space-between" align="center">
      <n-space>
        <n-select
          v-model:value="selectedServer"
          :options="serverOptions"
          :placeholder="zh.monitor.server"
          clearable
          style="width: 240px"
        />
        <n-button type="primary" @click="onScan" :loading="store.loading">
          {{ zh.monitor.scanNow }}
        </n-button>
        <n-button @click="onStart">{{ zh.monitor.startAll }}</n-button>
        <n-button @click="onStop">{{ zh.monitor.stopAll }}</n-button>
      </n-space>
      <n-text depth="3">
        {{ zh.monitor.lastScan }}: {{ lastScanLabel }}
      </n-text>
    </n-space>

    <port-table
      v-if="filtered.length > 0"
      :data="filtered"
      @toggle="onToggle"
    />
    <n-empty v-else :description="zh.monitor.empty" />
  </n-space>
</template>

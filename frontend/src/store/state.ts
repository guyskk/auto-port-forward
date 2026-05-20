// store/state.ts —— Pinia 全局状态。
//
// useAppStore() 暴露 servers / forwards / config / loading；
// initAppStore() 在 App 挂载时跑一次：拉初始数据、订阅 wails events。

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, onEvent } from '../api/wails'
import {
  EVENT_FORWARD_UPDATE,
  EVENT_SCAN_ERROR,
  EVENT_SERVER_STATUS,
  EVENT_STATE_UPDATE,
} from '../types'
import type { Config, Forward, Server, ServerStatus } from '../types'

export const useAppStore = defineStore('app', () => {
  const servers = ref<Server[]>([])
  const forwards = ref<Forward[]>([])
  const config = ref<Config | null>(null)
  const loading = ref(false)
  const lastScanAt = ref<number | null>(null)
  const lastError = ref<string>('')
  const serverStatus = ref<Record<string, ServerStatus>>({})

  async function refresh(): Promise<void> {
    loading.value = true
    try {
      const [cfg, snap, list] = await Promise.all([
        api.GetConfig(),
        api.GetSnapshot(),
        api.ListServers(),
      ])
      config.value = cfg
      forwards.value = snap
      servers.value = list
    } finally {
      loading.value = false
    }
  }

  async function scanNow(): Promise<void> {
    await api.ScanNow()
    lastScanAt.value = Date.now()
    forwards.value = await api.GetSnapshot()
  }

  async function addServer(s: Server): Promise<Server> {
    const created = await api.AddServer(s)
    servers.value = await api.ListServers()
    return created
  }

  async function updateServer(s: Server): Promise<void> {
    await api.UpdateServer(s)
    servers.value = await api.ListServers()
  }

  async function deleteServer(id: string): Promise<void> {
    await api.DeleteServer(id)
    servers.value = await api.ListServers()
  }

  async function updateRules(r: Config['rules']): Promise<void> {
    await api.UpdateRules(r)
    config.value = await api.GetConfig()
  }

  async function updateScanInterval(sec: number): Promise<void> {
    await api.UpdateScanInterval(sec)
    config.value = await api.GetConfig()
  }

  function subscribe(): void {
    onEvent(EVENT_STATE_UPDATE, (data) => {
      forwards.value = data as Forward[]
      lastScanAt.value = Date.now()
    })
    onEvent(EVENT_FORWARD_UPDATE, () => {
      // 单条变化：拉一次完整快照，保持简单。
      api.GetSnapshot().then((s) => (forwards.value = s))
    })
    onEvent(EVENT_SCAN_ERROR, (data) => {
      const d = data as { error?: string }
      lastError.value = d?.error || 'scan error'
    })
    onEvent(EVENT_SERVER_STATUS, (data) => {
      const s = data as ServerStatus
      if (s?.server_id) {
        serverStatus.value = { ...serverStatus.value, [s.server_id]: s }
      }
    })
  }

  return {
    servers,
    forwards,
    config,
    loading,
    lastScanAt,
    lastError,
    serverStatus,
    refresh,
    scanNow,
    addServer,
    updateServer,
    deleteServer,
    updateRules,
    updateScanInterval,
    subscribe,
  }
})

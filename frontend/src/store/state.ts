// store/state.ts —— Pinia 全局状态。
//
// useAppStore() 暴露 hosts / enabledHosts / forwards / config / serverStatus；
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
import type { Config, Forward, Host, ServerStatus } from '../types'

export const useAppStore = defineStore('app', () => {
  const hosts = ref<Host[]>([])
  const enabledHosts = ref<string[]>([])
  const forwards = ref<Forward[]>([])
  const config = ref<Config | null>(null)
  const loading = ref(false)
  const lastScanAt = ref<number | null>(null)
  const lastError = ref<string>('')
  const serverStatus = ref<Record<string, ServerStatus>>({})

  async function refresh(): Promise<void> {
    loading.value = true
    try {
      const [cfg, snap, hostList, enabled] = await Promise.all([
        api.GetConfig(),
        api.GetSnapshot(),
        api.ListHosts(),
        api.EnabledHosts(),
      ])
      config.value = cfg
      forwards.value = snap
      hosts.value = hostList
      enabledHosts.value = enabled
    } finally {
      loading.value = false
    }
  }

  async function scanNow(): Promise<void> {
    await api.ScanNow()
    lastScanAt.value = Date.now()
    forwards.value = await api.GetSnapshot()
  }

  async function setHostEnabled(alias: string, on: boolean): Promise<void> {
    await api.SetHostEnabled(alias, on)
    enabledHosts.value = await api.EnabledHosts()
    // 关闭某 host 后，立刻清掉它的 forward 行；开启则等下一次扫描自然填充。
    if (!on) {
      forwards.value = forwards.value.filter((f) => f.server_id !== alias)
    }
  }

  async function reloadSSHConfig(): Promise<void> {
    await api.ReloadSSHConfig()
    hosts.value = await api.ListHosts()
  }

  async function testHost(alias: string): Promise<void> {
    await api.TestHost(alias)
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
    hosts,
    enabledHosts,
    forwards,
    config,
    loading,
    lastScanAt,
    lastError,
    serverStatus,
    refresh,
    scanNow,
    setHostEnabled,
    reloadSSHConfig,
    testHost,
    updateRules,
    updateScanInterval,
    subscribe,
  }
})

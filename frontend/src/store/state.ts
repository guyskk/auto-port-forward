// store/state.ts —— 薄 Pinia adapter：把 core/ 的 state + reducer + selectors + intents
// 编织成响应式 store。
//
// 设计要点：
//   - 单一 shallowRef 持有完整 AppState；reducer 返回 prev 时 ref 不变，
//     Vue 的 hasChanged(=== Object.is) 检查会跳过依赖触发 → 整个组件树不重渲染。
//   - 业务模板看到的不是裸 ref，而是 computed —— Vue computed 在依赖 ref 未变时
//     直接缓存输出值返回，selector 内部 memoize 配合做到端到端"零浪费重算"。
//   - 所有副作用（api 调用、事件订阅）由 intents/subscribe 闭包持有；
//     模板只能 dispatch event 触发 reducer，从这个意义上 store 是 reducer 的 Vue 壳。

import { defineStore } from 'pinia'
import { computed, shallowRef } from 'vue'
import { api, onEvent } from '../api/wails'
import {
  EVENT_FORWARD_UPDATE,
  EVENT_SCAN_ERROR,
  EVENT_SERVER_STATUS,
  EVENT_STATE_UPDATE,
} from '../types'
import { initialState } from '../core/state'
import { applyEvent } from '../core/reducer'
import { createIntents } from '../core/intent'
import {
  byServerForwards,
  forwardsByHost,
  hostsView,
} from '../core/selectors'
import type {
  Event,
  Forward,
  PortStatus,
  ServerStatus,
} from '../core/types'

function toArray<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
}

export const useAppStore = defineStore('app', () => {
  const state = shallowRef(initialState())

  function dispatch(ev: Event): void {
    state.value = applyEvent(state.value, ev)
  }

  const intents = createIntents(api, dispatch)

  function subscribe(): void {
    onEvent(EVENT_STATE_UPDATE, (data) => {
      dispatch({
        kind: 'state-update',
        forwards: toArray(data as Forward[] | null | undefined),
      })
    })
    // 后端的 EventForwardUpdate 已经携带 {server_id, remote_port, status, error}，
    // 不再额外拉 GetSnapshot —— 这是治理"一次状态变化 → 一次全量拉取"的关键路径。
    onEvent(EVENT_FORWARD_UPDATE, (data) => {
      const d = data as {
        server_id?: string
        remote_port?: number
        status?: string
        error?: string
      }
      if (!d?.server_id || typeof d.remote_port !== 'number') return
      dispatch({
        kind: 'forward-update',
        serverId: d.server_id,
        port: d.remote_port,
        status: (d.status as PortStatus) ?? 'pending',
        error: d.error || '',
      })
    })
    onEvent(EVENT_SCAN_ERROR, (data) => {
      const d = data as { error?: string }
      dispatch({ kind: 'scan-error', error: d?.error || 'scan error' })
    })
    onEvent(EVENT_SERVER_STATUS, (data) => {
      const s = data as ServerStatus
      if (s?.server_id) dispatch({ kind: 'server-status', status: s })
    })
  }

  // 平铺的字段 computed：仅供旧模板代码读，引用相等保证不重渲染。
  const hosts = computed(() => state.value.hosts)
  const enabledHosts = computed(() => state.value.enabledHosts)
  const forwards = computed(() => state.value.forwards)
  const config = computed(() => state.value.config)
  const loading = computed(() => state.value.loading)
  const lastScanAt = computed(() => state.value.lastScanAt)
  const lastError = computed(() => state.value.lastError)
  const serverStatus = computed(() => state.value.serverStatus)

  // 派生 computed：在模板里直接用，selector 的 memoize 让等价输入返回相同输出。
  const byServer = computed(() => byServerForwards(state.value))
  const hostsViewComp = computed(() => hostsView(state.value))

  function forwardsByAlias(alias: string): ReadonlyArray<Forward> {
    return forwardsByHost(state.value, alias)
  }

  return {
    // 基础切片（供逐步迁移；新代码建议用 hostsView / byServer）
    hosts,
    enabledHosts,
    forwards,
    config,
    loading,
    lastScanAt,
    lastError,
    serverStatus,
    // 派生视图
    byServer,
    hostsView: hostsViewComp,
    forwardsByAlias,
    // 用户意图（来自 core/intent.ts，已包含 refresh/scanNow/toggleForward 等）
    refresh: intents.refresh,
    scanNow: intents.scanNow,
    setHostEnabled: intents.setHostEnabled,
    reloadSSHConfig: intents.reloadSSHConfig,
    testHost: intents.testHost,
    updateRules: intents.updateRules,
    updateScanInterval: intents.updateScanInterval,
    toggleForward: intents.toggleForward,
    // 订阅 wails 事件
    subscribe,
  }
})

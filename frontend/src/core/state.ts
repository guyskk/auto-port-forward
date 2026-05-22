// core/state.ts —— 纯数据状态形状。
//
// Readonly 仅作类型提示；reducer 通过创建新对象保证不可变性，
// 不用 Object.freeze 以避免运行时开销。

import type { Forward, Host, Config, ServerStatus } from './types'

export interface AppState {
  hosts: ReadonlyArray<Host>
  enabledHosts: ReadonlyArray<string>
  forwards: ReadonlyArray<Forward>
  serverStatus: Readonly<Record<string, ServerStatus>>
  config: Config | null
  lastScanAt: number | null
  lastError: string
  loading: boolean
}

export function initialState(): AppState {
  return {
    hosts: [],
    enabledHosts: [],
    forwards: [],
    serverStatus: {},
    config: null,
    lastScanAt: null,
    lastError: '',
    loading: false,
  }
}

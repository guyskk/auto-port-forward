// api/wails.ts —— Wails 后端绑定封装 + 浏览器 mock fallback。
//
// 在 `wails dev` 内运行时 window.go.main.App.* 由 wails 注入；
// 在 vite dev (浏览器直开 http://localhost:5173) 时降级到 mock 数据，
// 便于在 Linux miniubt 上独立验收 UI。

import type { Config, Forward, Server } from '../types'
import * as mock from './mock'

interface WailsBindings {
  ListServers(): Promise<Server[]>
  AddServer(s: Server): Promise<Server>
  UpdateServer(s: Server): Promise<void>
  DeleteServer(id: string): Promise<void>
  TestServer(id: string): Promise<void>
  GetConfig(): Promise<Config>
  UpdateRules(r: Config['rules']): Promise<void>
  UpdateScanInterval(sec: number): Promise<void>
  StartAll(): Promise<void>
  StopAll(): Promise<void>
  ScanNow(): Promise<void>
  ToggleForward(serverID: string, port: number, on: boolean): Promise<void>
  GetSnapshot(): Promise<Forward[]>
}

interface WailsRuntime {
  EventsOn(name: string, cb: (data: unknown) => void): () => void
  EventsOff(name: string): void
}

declare global {
  interface Window {
    go?: { main?: { App?: WailsBindings } }
    runtime?: WailsRuntime
  }
}

function isWailsAvailable(): boolean {
  return typeof window !== 'undefined' && !!window.go?.main?.App
}

function backend(): WailsBindings {
  if (isWailsAvailable()) {
    return window.go!.main!.App!
  }
  return mock.api
}

export const api: WailsBindings = {
  ListServers: () => backend().ListServers(),
  AddServer: (s) => backend().AddServer(s),
  UpdateServer: (s) => backend().UpdateServer(s),
  DeleteServer: (id) => backend().DeleteServer(id),
  TestServer: (id) => backend().TestServer(id),
  GetConfig: () => backend().GetConfig(),
  UpdateRules: (r) => backend().UpdateRules(r),
  UpdateScanInterval: (sec) => backend().UpdateScanInterval(sec),
  StartAll: () => backend().StartAll(),
  StopAll: () => backend().StopAll(),
  ScanNow: () => backend().ScanNow(),
  ToggleForward: (id, port, on) => backend().ToggleForward(id, port, on),
  GetSnapshot: () => backend().GetSnapshot(),
}

// onEvent 注册一个事件监听，返回取消函数。
// 浏览器 mock 模式下使用 mock 事件总线。
export function onEvent(name: string, cb: (data: unknown) => void): () => void {
  if (typeof window !== 'undefined' && window.runtime) {
    return window.runtime.EventsOn(name, cb)
  }
  return mock.onEvent(name, cb)
}

export const isWails = isWailsAvailable

// core/types.ts —— 纯逻辑层使用的类型定义，零 vue/wails 依赖。
//
// 大部分类型直接从顶层 ../types re-export；本文件额外定义 Event 联合类型，
// 即 reducer 接受的所有事件形状。

import type {
  Forward,
  Host,
  Config,
  Rules,
  ServerStatus,
  PortStatus,
} from '../types'

export type {
  Forward,
  Host,
  Config,
  Rules,
  ServerStatus,
  PortStatus,
}

// Event 是 reducer 接受的所有事件形状的联合。
// 来源：wails runtime 推送的事件 + 用户意图触发的本地 dispatch。
export type Event =
  | { kind: 'state-update'; forwards: Forward[] }
  | {
      kind: 'forward-update'
      serverId: string
      port: number
      status: PortStatus
      error: string
    }
  | { kind: 'server-status'; status: ServerStatus }
  | { kind: 'scan-error'; error: string }
  | { kind: 'config-loaded'; config: Config }
  | { kind: 'hosts-loaded'; hosts: Host[]; enabled: string[] }
  | { kind: 'enabled-hosts-updated'; enabled: string[] }
  | { kind: 'loading'; on: boolean }
  | { kind: 'scan-finished'; at: number }

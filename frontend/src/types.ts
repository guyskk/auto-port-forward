// types.ts —— 镜像 Go internal/model + internal/config + internal/sshcfg 的核心结构。
//
// 字段名与 JSON tag 完全一致，便于 wails 反序列化直接喂入。

export type PortStatus =
  | 'forwarding'
  | 'pending'
  | 'excluded'
  | 'conflict'
  | 'conflict_priv'
  | 'error'

export interface RemotePort {
  port: number
  bind_addr: string
  ip_version: string
  pid: number
  process: string
  command: string
  docker_image: string
}

export interface LocalPort {
  port: number
  process: string
  type: string
  pid: number
}

export interface Forward {
  server_id: string
  remote_port: number
  local_port: number
  status: PortStatus
  error?: string
  last_active: number
  remote: RemotePort
}

export interface Span {
  lo: number
  hi: number
}

export interface Rules {
  exclude_ports: number[]
  exclude_ranges: Span[]
}

// Host 镜像 Go sshcfg.Host —— 来自 ssh config 的具体别名 + ssh -G effective 配置。
export interface Host {
  alias: string
  host_name: string
  user: string
  port: number
}

export interface Config {
  scan_interval_sec: number
  rules: Rules
  enabled_hosts: string[]
}

export type ServerConnState = 'dialing' | 'connected' | 'broken' | 'degraded'

export interface ServerStatus {
  server_id: string // 实际为 ssh alias
  state: ServerConnState
  attempt: number
  disconnected_ms: number
  error?: string
}

export const EVENT_STATE_UPDATE = 'state:update'
export const EVENT_SERVER_STATUS = 'server:status'
export const EVENT_SCAN_ERROR = 'scan:error'
export const EVENT_FORWARD_UPDATE = 'forward:update'

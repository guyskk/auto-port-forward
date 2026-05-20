// types.ts —— 镜像 Go internal/model 与 internal/config 的核心结构。
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
  only_public_bind: boolean
  local_port_offset: number
}

export interface Server {
  id: string
  name: string
  host: string
  port: number
  user: string
  auth_method: 'password' | 'ssh_key' | 'ssh_agent'
  password?: string
  key_path?: string
  passphrase?: string
  host_key: 'known_hosts' | 'insecure'
  enabled: boolean
}

export interface Config {
  scan_interval_sec: number
  servers: Server[]
  rules: Rules
}

export type ServerConnState = 'dialing' | 'connected' | 'broken' | 'degraded'

export interface ServerStatus {
  server_id: string
  state: ServerConnState
  attempt: number
  disconnected_ms: number
  error?: string
}

export const EVENT_STATE_UPDATE = 'state:update'
export const EVENT_SERVER_STATUS = 'server:status'
export const EVENT_SCAN_ERROR = 'scan:error'
export const EVENT_FORWARD_UPDATE = 'forward:update'

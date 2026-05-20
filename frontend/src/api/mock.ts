// api/mock.ts —— 浏览器开发模式的 mock 数据 + 事件总线。
//
// 仅用于在 vite dev 时让 UI 可以独立渲染、点击；wails dev 不走这里。

import type { Config, Forward, Server, ServerStatus } from '../types'
import { EVENT_SERVER_STATUS, EVENT_STATE_UPDATE } from '../types'

type Listener = (data: unknown) => void
const listeners = new Map<string, Set<Listener>>()

function emit(name: string, data: unknown): void {
  listeners.get(name)?.forEach((l) => l(data))
}

export function onEvent(name: string, cb: Listener): () => void {
  if (!listeners.has(name)) listeners.set(name, new Set())
  listeners.get(name)!.add(cb)
  return () => listeners.get(name)?.delete(cb)
}

const seedServers: Server[] = [
  {
    id: 'mock-ubt',
    name: 'ubt (mock)',
    host: '10.0.0.42',
    port: 22,
    user: 'ubuntu',
    auth_method: 'ssh_agent',
    host_key: 'known_hosts',
    enabled: true,
  },
]

let state: Config = {
  scan_interval_sec: 15,
  servers: [...seedServers],
  rules: {
    exclude_ports: [22, 53, 80, 443, 111, 631],
    exclude_ranges: [],
    only_public_bind: false,
    local_port_offset: 0,
  },
}

const seedForwards: Forward[] = [
  {
    server_id: 'mock-ubt',
    remote_port: 9527,
    local_port: 9527,
    status: 'forwarding',
    last_active: Math.floor(Date.now() / 1000),
    remote: { port: 9527, bind_addr: '0.0.0.0', ip_version: 'IPv4', pid: 1024, process: 'tabby', command: 'tabby agent', docker_image: '' },
  },
  {
    server_id: 'mock-ubt',
    remote_port: 3000,
    local_port: 3000,
    status: 'pending',
    last_active: 0,
    remote: { port: 3000, bind_addr: '0.0.0.0', ip_version: 'IPv4', pid: 2048, process: 'node', command: 'node server.js', docker_image: '' },
  },
  {
    server_id: 'mock-ubt',
    remote_port: 8080,
    local_port: 8080,
    status: 'conflict',
    error: 'address already in use',
    last_active: 0,
    remote: { port: 8080, bind_addr: '0.0.0.0', ip_version: 'IPv4', pid: 4096, process: 'caddy', command: 'caddy', docker_image: 'caddy:2.7' },
  },
  {
    server_id: 'mock-ubt',
    remote_port: 80,
    local_port: 80,
    status: 'conflict_priv',
    last_active: 0,
    remote: { port: 80, bind_addr: '0.0.0.0', ip_version: 'IPv4', pid: 8192, process: 'nginx', command: 'nginx', docker_image: '' },
  },
]

function deepClone<T>(x: T): T {
  return JSON.parse(JSON.stringify(x))
}

let snapshot: Forward[] = deepClone(seedForwards)

export const api = {
  async ListServers() {
    return deepClone(state.servers)
  },
  async AddServer(s: Server) {
    const created = { ...s, id: s.id || `mock-${Date.now()}` }
    state.servers.push(created)
    emitMockServerStatus(created.id, 'connected')
    return deepClone(created)
  },
  async UpdateServer(s: Server) {
    const i = state.servers.findIndex((x) => x.id === s.id)
    if (i >= 0) state.servers[i] = { ...s }
  },
  async DeleteServer(id: string) {
    state.servers = state.servers.filter((x) => x.id !== id)
  },
  async TestServer(id: string) {
    const s = state.servers.find((x) => x.id === id)
    if (!s) throw new Error('server not found')
    await new Promise((r) => setTimeout(r, 200))
  },
  async GetConfig() {
    return deepClone(state)
  },
  async UpdateRules(r: Config['rules']) {
    state.rules = { ...r }
  },
  async UpdateScanInterval(sec: number) {
    state.scan_interval_sec = sec
  },
  async StartAll() {
    state.servers.forEach((s) => emitMockServerStatus(s.id, 'connected'))
  },
  async StopAll() {},
  async ScanNow() {
    snapshot = deepClone(seedForwards)
    snapshot.forEach((f) => (f.last_active = Math.floor(Date.now() / 1000)))
    emit(EVENT_STATE_UPDATE, deepClone(snapshot))
  },
  async ToggleForward(_serverID: string, _port: number, _on: boolean) {},
  async GetSnapshot() {
    return deepClone(snapshot)
  },
}

function emitMockServerStatus(id: string, statusState: ServerStatus['state']): void {
  const payload: ServerStatus = {
    server_id: id,
    state: statusState,
    attempt: 0,
    disconnected_ms: 0,
  }
  emit(EVENT_SERVER_STATUS, payload)
}

// 初始化时把 seed servers 标为已连接，便于直接看到状态列。
setTimeout(() => {
  seedServers.forEach((s) => emitMockServerStatus(s.id, 'connected'))
}, 50)

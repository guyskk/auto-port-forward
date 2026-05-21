// api/mock.ts —— 浏览器开发模式的 mock 数据 + 事件总线。
//
// 仅用于在 vite dev 时让 UI 可以独立渲染、点击；wails dev 不走这里。

import type { Config, Forward, Host, ServerStatus } from '../types'
import { EVENT_FORWARD_UPDATE, EVENT_SERVER_STATUS, EVENT_STATE_UPDATE } from '../types'

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

// 模拟 ssh config 里有 3 个具体别名。
const seedHosts: Host[] = [
  { alias: 'ubt', host_name: '10.0.0.42', user: 'ubuntu', port: 22 },
  { alias: 'prod-db', host_name: 'db.example.com', user: 'root', port: 2222 },
  { alias: 'stage-edge', host_name: 'edge.example.com', user: 'admin', port: 22 },
]

let cfg: Config = {
  scan_interval_sec: 15,
  rules: {
    exclude_ports: [22, 53, 80, 443, 111, 631],
    exclude_ranges: [],
  },
  enabled_hosts: ['ubt'], // 默认仅启用 ubt 一个
}

const seedForwards: Forward[] = [
  {
    server_id: 'ubt',
    remote_port: 9527,
    local_port: 9527,
    status: 'forwarding',
    last_active: Math.floor(Date.now() / 1000),
    remote: { port: 9527, bind_addr: '0.0.0.0', ip_version: 'IPv4', pid: 1024, process: 'tabby', command: 'tabby agent', docker_image: '' },
  },
  {
    server_id: 'ubt',
    remote_port: 3000,
    local_port: 3000,
    status: 'pending',
    last_active: 0,
    remote: { port: 3000, bind_addr: '0.0.0.0', ip_version: 'IPv4', pid: 2048, process: 'node', command: 'node server.js', docker_image: '' },
  },
  {
    server_id: 'ubt',
    remote_port: 8080,
    local_port: 8080,
    status: 'conflict',
    error: 'address already in use',
    last_active: 0,
    remote: { port: 8080, bind_addr: '0.0.0.0', ip_version: 'IPv4', pid: 4096, process: 'caddy', command: 'caddy', docker_image: 'caddy:2.7' },
  },
  {
    server_id: 'ubt',
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
  async ListHosts() {
    return deepClone(seedHosts)
  },
  async EnabledHosts() {
    return [...cfg.enabled_hosts]
  },
  async SetHostEnabled(alias: string, on: boolean) {
    const has = cfg.enabled_hosts.includes(alias)
    if (on && !has) cfg.enabled_hosts = [...cfg.enabled_hosts, alias]
    if (!on && has) cfg.enabled_hosts = cfg.enabled_hosts.filter((a) => a !== alias)
    if (on) {
      emitMockServerStatus(alias, 'connected')
      // 让 mock 监控页能立刻看到端口
      snapshot = deepClone(seedForwards).filter((f) => cfg.enabled_hosts.includes(f.server_id))
      emit(EVENT_STATE_UPDATE, deepClone(snapshot))
    } else {
      snapshot = snapshot.filter((f) => f.server_id !== alias)
      emit(EVENT_FORWARD_UPDATE, { server_id: alias })
    }
  },
  async ReloadSSHConfig() {
    // mock：no-op，host 列表本就来自 seedHosts。
  },
  async TestHost(alias: string) {
    if (!seedHosts.some((h) => h.alias === alias)) throw new Error('host not found')
    await new Promise((r) => setTimeout(r, 200))
  },
  async GetConfig() {
    return deepClone(cfg)
  },
  async UpdateRules(r: Config['rules']) {
    cfg.rules = deepClone(r)
  },
  async UpdateScanInterval(sec: number) {
    cfg.scan_interval_sec = sec
  },
  async StartAll() {
    cfg.enabled_hosts.forEach((a) => emitMockServerStatus(a, 'connected'))
  },
  async StopAll() {},
  async ScanNow() {
    snapshot = deepClone(seedForwards).filter((f) => cfg.enabled_hosts.includes(f.server_id))
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

// 初始化时把启用的 host 标为已连接。
setTimeout(() => {
  cfg.enabled_hosts.forEach((a) => emitMockServerStatus(a, 'connected'))
}, 50)

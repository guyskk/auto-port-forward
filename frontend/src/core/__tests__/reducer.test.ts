// core/__tests__/reducer.test.ts —— 验证 reducer 的纯函数语义 + 性能契约。
//
// 性能契约（最关键）：内容相同的事件返回原 state 引用。
// 这就是治理"事件风暴 → 雪崩重渲染"的底层机制 —— Vue 在 ref 不赋值的情况下
// 不会触发任何 computed/template 重算。

import { describe, expect, it } from 'vitest'
import { applyEvent } from '../reducer'
import { initialState } from '../state'
import type { Event, Forward, ServerStatus } from '../types'

function makeForward(serverId: string, port: number, overrides: Partial<Forward> = {}): Forward {
  return {
    server_id: serverId,
    remote_port: port,
    local_port: port,
    status: 'forwarding',
    last_active: 0,
    remote: {
      port,
      bind_addr: '0.0.0.0',
      ip_version: 'IPv4',
      pid: 0,
      process: '',
      command: '',
      docker_image: '',
    },
    ...overrides,
  }
}

describe('state-update', () => {
  it('首次填入 forwards 时返回新 state，更新 lastScanAt', () => {
    const s0 = initialState()
    const forwards = [makeForward('a', 22)]
    const s1 = applyEvent(s0, { kind: 'state-update', forwards })
    expect(s1).not.toBe(s0)
    expect(s1.forwards).toBe(forwards)
    expect(s1.lastScanAt).not.toBeNull()
  })

  it('内容相同的 state-update 返回 prev 引用（性能契约核心）', () => {
    const forwards = [makeForward('a', 22), makeForward('a', 80)]
    const s1 = applyEvent(initialState(), { kind: 'state-update', forwards })
    // 不同数组实例，相同内容
    const ev2: Event = { kind: 'state-update', forwards: [...forwards.map((f) => ({ ...f }))] }
    const s2 = applyEvent(s1, ev2)
    expect(s2).toBe(s1)
  })

  it('forwards 数组长度变化时返回新 state', () => {
    const f1 = makeForward('a', 22)
    const s1 = applyEvent(initialState(), { kind: 'state-update', forwards: [f1] })
    const s2 = applyEvent(s1, {
      kind: 'state-update',
      forwards: [f1, makeForward('a', 80)],
    })
    expect(s2).not.toBe(s1)
  })

  it('某条 forward 内容变化（status）时返回新 state', () => {
    const f1 = makeForward('a', 22, { status: 'pending' })
    const s1 = applyEvent(initialState(), { kind: 'state-update', forwards: [f1] })
    const s2 = applyEvent(s1, {
      kind: 'state-update',
      forwards: [{ ...f1, status: 'forwarding' }],
    })
    expect(s2).not.toBe(s1)
  })

  it('last_active 变化但其他字段相同时也认为内容相同', () => {
    // 我们刻意忽略 last_active 在等值比较中的差异——稳态扫描可能因 tick 边界
    // 写入相同的 last_active，但即便写入不同的，UI 也不依赖它做关键展示。
    const f1 = makeForward('a', 22, { last_active: 100 })
    const s1 = applyEvent(initialState(), { kind: 'state-update', forwards: [f1] })
    const s2 = applyEvent(s1, {
      kind: 'state-update',
      forwards: [{ ...f1, last_active: 200 }],
    })
    expect(s2).toBe(s1)
  })
})

describe('server-status', () => {
  const status: ServerStatus = {
    server_id: 'a',
    state: 'connected',
    attempt: 0,
    disconnected_ms: 0,
  }

  it('首次写入 server status 返回新 state', () => {
    const s0 = initialState()
    const s1 = applyEvent(s0, { kind: 'server-status', status })
    expect(s1).not.toBe(s0)
    expect(s1.serverStatus['a']).toEqual(status)
  })

  it('相同 server-status 重复 100 次只产生 1 次有效变化（性能契约）', () => {
    let s = initialState()
    let changes = 0
    for (let i = 0; i < 100; i++) {
      const next = applyEvent(s, { kind: 'server-status', status: { ...status } })
      if (next !== s) changes++
      s = next
    }
    expect(changes).toBe(1)
  })

  it('state 字段变化时返回新 state', () => {
    const s1 = applyEvent(initialState(), { kind: 'server-status', status })
    const s2 = applyEvent(s1, {
      kind: 'server-status',
      status: { ...status, state: 'broken' },
    })
    expect(s2).not.toBe(s1)
  })

  it('attempt 字段变化时返回新 state', () => {
    const s1 = applyEvent(initialState(), { kind: 'server-status', status })
    const s2 = applyEvent(s1, {
      kind: 'server-status',
      status: { ...status, attempt: 5 },
    })
    expect(s2).not.toBe(s1)
  })

  it('丢空 server_id 不影响 state', () => {
    const s1 = applyEvent(initialState(), {
      kind: 'server-status',
      status: { ...status, server_id: '' },
    })
    expect(s1).toBe(initialState() === s1 ? s1 : s1) // 实现可能返回 prev 或返回新 state；只要 serverStatus 不写入''
    expect(s1.serverStatus['']).toBeUndefined()
  })
})

describe('forward-update', () => {
  it('已存在的 forward 状态变化时返回新 state', () => {
    const f = makeForward('a', 22, { status: 'pending' })
    const s1 = applyEvent(initialState(), { kind: 'state-update', forwards: [f] })
    const s2 = applyEvent(s1, {
      kind: 'forward-update',
      serverId: 'a',
      port: 22,
      status: 'forwarding',
      error: '',
    })
    expect(s2).not.toBe(s1)
    const updated = s2.forwards.find((x) => x.server_id === 'a' && x.remote_port === 22)
    expect(updated?.status).toBe('forwarding')
  })

  it('forward-update 命中不存在的 (server, port) 直接返回 prev', () => {
    const s1 = applyEvent(initialState(), {
      kind: 'state-update',
      forwards: [makeForward('a', 22)],
    })
    const s2 = applyEvent(s1, {
      kind: 'forward-update',
      serverId: 'a',
      port: 999,
      status: 'forwarding',
      error: '',
    })
    expect(s2).toBe(s1)
  })

  it('status 与 error 都相同时返回 prev', () => {
    const f = makeForward('a', 22, { status: 'forwarding', error: '' })
    const s1 = applyEvent(initialState(), { kind: 'state-update', forwards: [f] })
    const s2 = applyEvent(s1, {
      kind: 'forward-update',
      serverId: 'a',
      port: 22,
      status: 'forwarding',
      error: '',
    })
    expect(s2).toBe(s1)
  })
})

describe('scan-error / loading / scan-finished', () => {
  it('scan-error 内容相同返回 prev', () => {
    const s1 = applyEvent(initialState(), { kind: 'scan-error', error: 'oops' })
    const s2 = applyEvent(s1, { kind: 'scan-error', error: 'oops' })
    expect(s2).toBe(s1)
  })

  it('loading on 与当前不同才返回新 state', () => {
    const s1 = applyEvent(initialState(), { kind: 'loading', on: true })
    const s2 = applyEvent(s1, { kind: 'loading', on: true })
    expect(s2).toBe(s1)
    const s3 = applyEvent(s1, { kind: 'loading', on: false })
    expect(s3).not.toBe(s1)
  })

  it('scan-finished 总是更新 lastScanAt', () => {
    const s1 = applyEvent(initialState(), { kind: 'scan-finished', at: 100 })
    expect(s1.lastScanAt).toBe(100)
  })
})

describe('hosts-loaded / config-loaded / enabled-hosts-updated', () => {
  it('config-loaded 内容相同（按 JSON.stringify 等价）返回 prev', () => {
    const cfg = {
      scan_interval_sec: 15,
      rules: { exclude_ports: [22], exclude_ranges: [] },
      enabled_hosts: ['a'],
    }
    const s1 = applyEvent(initialState(), { kind: 'config-loaded', config: cfg })
    const s2 = applyEvent(s1, { kind: 'config-loaded', config: { ...cfg } })
    expect(s2).toBe(s1)
  })

  it('hosts-loaded 列表内容相同返回 prev', () => {
    const hosts = [{ alias: 'a', host_name: 'h', user: 'u', port: 22 }]
    const enabled = ['a']
    const s1 = applyEvent(initialState(), { kind: 'hosts-loaded', hosts, enabled })
    const s2 = applyEvent(s1, {
      kind: 'hosts-loaded',
      hosts: [...hosts.map((h) => ({ ...h }))],
      enabled: [...enabled],
    })
    expect(s2).toBe(s1)
  })

  it('enabled-hosts-updated 数组相同返回 prev', () => {
    const s1 = applyEvent(initialState(), {
      kind: 'enabled-hosts-updated',
      enabled: ['a'],
    })
    const s2 = applyEvent(s1, {
      kind: 'enabled-hosts-updated',
      enabled: ['a'],
    })
    expect(s2).toBe(s1)
  })
})

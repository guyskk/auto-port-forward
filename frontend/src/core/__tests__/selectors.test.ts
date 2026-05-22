// core/__tests__/selectors.test.ts —— 验证 selectors：
//
// 1) 输出正确（按 server 分组、统计、衍生派生字段等）
// 2) 输入引用未变时输出引用相等（memoize 契约）—— 这就是让 Vue computed 不重算的关键

import { describe, expect, it } from 'vitest'
import {
  createSelectors,
  byServerForwards,
  hostsView,
  forwardsByHost,
} from '../selectors'
import { initialState } from '../state'
import type { AppState } from '../state'
import { applyEvent } from '../reducer'
import type { Forward, Host } from '../types'

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

function makeHost(alias: string): Host {
  return { alias, host_name: `${alias}.local`, user: 'root', port: 22 }
}

describe('byServerForwards', () => {
  it('按 server_id 分组 forwards', () => {
    const s = applyEvent(initialState(), {
      kind: 'state-update',
      forwards: [
        makeForward('a', 22),
        makeForward('a', 80),
        makeForward('b', 22),
      ],
    })
    const grouped = byServerForwards(s)
    expect(grouped['a']).toHaveLength(2)
    expect(grouped['b']).toHaveLength(1)
  })

  it('forwards 引用未变时输出引用相等（memoize 契约）', () => {
    const s = applyEvent(initialState(), {
      kind: 'state-update',
      forwards: [makeForward('a', 22)],
    })
    const r1 = byServerForwards(s)
    const r2 = byServerForwards(s)
    expect(r2).toBe(r1)
  })

  it('上一次返回的引用在等价 state-update 后仍然有效（reducer 不换 ref）', () => {
    const f = [makeForward('a', 22)]
    const s1 = applyEvent(initialState(), { kind: 'state-update', forwards: f })
    const r1 = byServerForwards(s1)
    // 等价更新 → reducer 返回相同 state ref → selector 返回相同 ref
    const s2 = applyEvent(s1, {
      kind: 'state-update',
      forwards: [{ ...f[0] }],
    })
    expect(s2).toBe(s1)
    const r2 = byServerForwards(s2)
    expect(r2).toBe(r1)
  })
})

describe('hostsView', () => {
  it('合并 hosts + enabledHosts + serverStatus 为 view-model', () => {
    let s: AppState = initialState()
    s = applyEvent(s, {
      kind: 'hosts-loaded',
      hosts: [makeHost('a'), makeHost('b')],
      enabled: ['a'],
    })
    s = applyEvent(s, {
      kind: 'server-status',
      status: {
        server_id: 'a',
        state: 'connected',
        attempt: 0,
        disconnected_ms: 0,
      },
    })
    const view = hostsView(s)
    expect(view).toHaveLength(2)
    const a = view.find((h) => h.alias === 'a')!
    const b = view.find((h) => h.alias === 'b')!
    expect(a.enabled).toBe(true)
    expect(a.status?.state).toBe('connected')
    expect(b.enabled).toBe(false)
    expect(b.status).toBeUndefined()
  })

  it('输入引用未变时输出引用相等', () => {
    const s = applyEvent(initialState(), {
      kind: 'hosts-loaded',
      hosts: [makeHost('a')],
      enabled: ['a'],
    })
    expect(hostsView(s)).toBe(hostsView(s))
  })
})

describe('forwardsByHost', () => {
  it('返回指定 server 的 forwards 列表', () => {
    const s = applyEvent(initialState(), {
      kind: 'state-update',
      forwards: [makeForward('a', 22), makeForward('b', 22)],
    })
    const list = forwardsByHost(s, 'a')
    expect(list).toHaveLength(1)
    expect(list[0].server_id).toBe('a')
  })

  it('未知 host 返回空数组', () => {
    const s = applyEvent(initialState(), {
      kind: 'state-update',
      forwards: [makeForward('a', 22)],
    })
    expect(forwardsByHost(s, 'nope')).toEqual([])
  })
})

describe('createSelectors (实例化)', () => {
  it('每个实例有独立的 memoize 缓存', () => {
    const A = createSelectors()
    const B = createSelectors()
    const s = applyEvent(initialState(), {
      kind: 'state-update',
      forwards: [makeForward('a', 22)],
    })
    const a1 = A.byServerForwards(s)
    const b1 = B.byServerForwards(s)
    // 不同实例 — 内容相等但引用不同（因为是各自第一次计算）
    expect(a1).toEqual(b1)
    // 同一实例第二次必定引用相等
    expect(A.byServerForwards(s)).toBe(a1)
  })
})

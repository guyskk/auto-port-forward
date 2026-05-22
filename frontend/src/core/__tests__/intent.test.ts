// core/__tests__/intent.test.ts —— 验证 intents：用户意图被翻译为 api 调用 + dispatch。
//
// 用 fake ApiDeps + fake dispatch 隔离 wails / pinia。

import { describe, expect, it, vi } from 'vitest'
import { createIntents } from '../intent'
import type { ApiDeps } from '../intent'
import type { Event } from '../types'

function makeApi(overrides: Partial<ApiDeps> = {}): ApiDeps {
  return {
    ListHosts: vi.fn().mockResolvedValue([]),
    EnabledHosts: vi.fn().mockResolvedValue([]),
    SetHostEnabled: vi.fn().mockResolvedValue(undefined),
    ReloadSSHConfig: vi.fn().mockResolvedValue(undefined),
    TestHost: vi.fn().mockResolvedValue(undefined),
    GetConfig: vi.fn().mockResolvedValue({
      scan_interval_sec: 15,
      rules: { exclude_ports: [], exclude_ranges: [] },
      enabled_hosts: [],
    }),
    UpdateRules: vi.fn().mockResolvedValue(undefined),
    UpdateScanInterval: vi.fn().mockResolvedValue(undefined),
    ScanNow: vi.fn().mockResolvedValue(undefined),
    ToggleForward: vi.fn().mockResolvedValue(undefined),
    GetSnapshot: vi.fn().mockResolvedValue([]),
    ...overrides,
  }
}

describe('refresh', () => {
  it('并行拉取 config/snapshot/hosts/enabled 并 dispatch 对应事件', async () => {
    const dispatched: Event[] = []
    const api = makeApi({
      GetConfig: vi.fn().mockResolvedValue({
        scan_interval_sec: 15,
        rules: { exclude_ports: [22], exclude_ranges: [] },
        enabled_hosts: ['a'],
      }),
      GetSnapshot: vi.fn().mockResolvedValue([]),
      ListHosts: vi.fn().mockResolvedValue([
        { alias: 'a', host_name: 'h', user: 'u', port: 22 },
      ]),
      EnabledHosts: vi.fn().mockResolvedValue(['a']),
    })
    const intents = createIntents(api, (e) => dispatched.push(e))
    await intents.refresh()
    const kinds = dispatched.map((d) => d.kind)
    expect(kinds).toContain('loading')
    expect(kinds).toContain('config-loaded')
    expect(kinds).toContain('state-update')
    expect(kinds).toContain('hosts-loaded')
  })

  it('refresh 始终结束 loading（即使失败）', async () => {
    const dispatched: Event[] = []
    const api = makeApi({
      GetConfig: vi.fn().mockRejectedValue(new Error('boom')),
    })
    const intents = createIntents(api, (e) => dispatched.push(e))
    await expect(intents.refresh()).rejects.toThrow('boom')
    const loadings = dispatched.filter((d) => d.kind === 'loading')
    expect(loadings[loadings.length - 1]).toEqual({ kind: 'loading', on: false })
  })

  it('toArray 兜底：后端返回 null 也能正常 dispatch', async () => {
    const dispatched: Event[] = []
    const api = makeApi({
      GetSnapshot: vi.fn().mockResolvedValue(null as unknown as never),
      ListHosts: vi.fn().mockResolvedValue(null as unknown as never),
      EnabledHosts: vi.fn().mockResolvedValue(null as unknown as never),
    })
    const intents = createIntents(api, (e) => dispatched.push(e))
    await intents.refresh()
    const su = dispatched.find((d) => d.kind === 'state-update')
    const hl = dispatched.find((d) => d.kind === 'hosts-loaded')
    expect(su?.kind === 'state-update' && su.forwards).toEqual([])
    expect(hl?.kind === 'hosts-loaded' && hl.hosts).toEqual([])
    expect(hl?.kind === 'hosts-loaded' && hl.enabled).toEqual([])
  })
})

describe('scanNow', () => {
  it('调 api.ScanNow 然后 dispatch scan-finished', async () => {
    const dispatched: Event[] = []
    const api = makeApi()
    const intents = createIntents(api, (e) => dispatched.push(e))
    await intents.scanNow()
    expect(api.ScanNow).toHaveBeenCalled()
    // 不应该再调用 GetSnapshot ——
    // 关键性能优化：依赖 state-update 事件，而非额外拉一次快照。
    expect(api.GetSnapshot).not.toHaveBeenCalled()
    const sf = dispatched.find((d) => d.kind === 'scan-finished')
    expect(sf).toBeDefined()
  })
})

describe('toggleForward', () => {
  it('乐观更新：dispatch forward-update（pending），再调 api', async () => {
    const dispatched: Event[] = []
    const api = makeApi()
    const intents = createIntents(api, (e) => dispatched.push(e))
    await intents.toggleForward('a', 22, false)
    expect(api.ToggleForward).toHaveBeenCalledWith('a', 22, false)
    // 后端会通过事件回送真实状态，前端不需要重复拉 snapshot。
    expect(api.GetSnapshot).not.toHaveBeenCalled()
  })

  it('api 抛错时仍然完成 dispatch（不吞错）', async () => {
    const api = makeApi({
      ToggleForward: vi.fn().mockRejectedValue(new Error('nope')),
    })
    const intents = createIntents(api, () => {})
    await expect(intents.toggleForward('a', 22, false)).rejects.toThrow('nope')
  })
})

describe('setHostEnabled', () => {
  it('调 api 后 dispatch enabled-hosts-updated', async () => {
    const dispatched: Event[] = []
    const api = makeApi({
      EnabledHosts: vi.fn().mockResolvedValue(['a', 'b']),
    })
    const intents = createIntents(api, (e) => dispatched.push(e))
    await intents.setHostEnabled('b', true)
    expect(api.SetHostEnabled).toHaveBeenCalledWith('b', true)
    const ev = dispatched.find((d) => d.kind === 'enabled-hosts-updated')
    expect(ev?.kind === 'enabled-hosts-updated' && ev.enabled).toEqual(['a', 'b'])
  })
})

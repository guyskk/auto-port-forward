// core/intent.ts —— 用户意图（refresh / scanNow / toggle / setHostEnabled / ...）
// 翻译为「调 api → dispatch event」的协调层。
//
// 设计要点：
//   - 完全不依赖 vue / pinia / wails；只通过 ApiDeps 接口拿后端能力。
//   - 测试用 fake api + fake dispatch 即可覆盖整条调用链。
//   - 关键性能优化：不要在 scanNow / toggleForward 后立刻 GetSnapshot，
//     依赖后端的 EventStateUpdate / EventForwardUpdate 异步回送即可，
//     避免「一次操作 → 一次 RPC → 一次额外 RPC」的雪崩放大。

import type { Config, Event, Forward, Host } from './types'

// ApiDeps 是 core 层对外部能力的最小接口；wails / mock 均可实现。
export interface ApiDeps {
  ListHosts(): Promise<Host[] | null | undefined>
  EnabledHosts(): Promise<string[] | null | undefined>
  SetHostEnabled(alias: string, on: boolean): Promise<void>
  ReloadSSHConfig(): Promise<void>
  TestHost(alias: string): Promise<void>
  GetConfig(): Promise<Config>
  UpdateRules(r: Config['rules']): Promise<void>
  UpdateScanInterval(sec: number): Promise<void>
  ScanNow(): Promise<void>
  ToggleForward(serverID: string, port: number, on: boolean): Promise<void>
  GetSnapshot(): Promise<Forward[] | null | undefined>
}

// toArray 兜底：Go 端 nil slice 序列化为 null，这里统一收口为 []。
function toArray<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
}

export type Dispatch = (ev: Event) => void

export function createIntents(api: ApiDeps, dispatch: Dispatch) {
  return {
    async refresh(): Promise<void> {
      dispatch({ kind: 'loading', on: true })
      try {
        const [cfg, snap, hostList, enabled] = await Promise.all([
          api.GetConfig(),
          api.GetSnapshot(),
          api.ListHosts(),
          api.EnabledHosts(),
        ])
        dispatch({ kind: 'config-loaded', config: cfg })
        dispatch({ kind: 'state-update', forwards: toArray(snap) })
        dispatch({
          kind: 'hosts-loaded',
          hosts: toArray(hostList),
          enabled: toArray(enabled),
        })
      } finally {
        dispatch({ kind: 'loading', on: false })
      }
    },

    async scanNow(): Promise<void> {
      await api.ScanNow()
      // 关键：不主动 GetSnapshot — 等后端 EventStateUpdate 事件回送数据。
      // 这避免了「点一次按钮 → 一次 RPC + 一次额外快照拉取」的放大。
      dispatch({ kind: 'scan-finished', at: Date.now() })
    },

    async setHostEnabled(alias: string, on: boolean): Promise<void> {
      await api.SetHostEnabled(alias, on)
      const enabled = toArray(await api.EnabledHosts())
      dispatch({ kind: 'enabled-hosts-updated', enabled })
    },

    async reloadSSHConfig(): Promise<void> {
      await api.ReloadSSHConfig()
      const hostList = toArray(await api.ListHosts())
      const enabled = toArray(await api.EnabledHosts())
      dispatch({ kind: 'hosts-loaded', hosts: hostList, enabled })
    },

    async testHost(alias: string): Promise<void> {
      await api.TestHost(alias)
    },

    async updateRules(r: Config['rules']): Promise<void> {
      await api.UpdateRules(r)
      const cfg = await api.GetConfig()
      dispatch({ kind: 'config-loaded', config: cfg })
    },

    async updateScanInterval(sec: number): Promise<void> {
      await api.UpdateScanInterval(sec)
      const cfg = await api.GetConfig()
      dispatch({ kind: 'config-loaded', config: cfg })
    },

    async toggleForward(serverID: string, port: number, on: boolean): Promise<void> {
      // 不做乐观更新：后端会通过 EventStateUpdate / EventForwardUpdate 回送真实状态。
      // 也刻意不调用 GetSnapshot——这就是治理"一操作就全量重渲染"的关键。
      await api.ToggleForward(serverID, port, on)
    },
  }
}

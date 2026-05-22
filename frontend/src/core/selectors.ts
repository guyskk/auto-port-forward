// core/selectors.ts —— 派生数据计算 + memoize。
//
// 设计要点：
//   - selector 是纯函数 (AppState) -> view-model；不应该有任何副作用。
//   - 用 memo() 包一层：输入引用未变（reducer 已保证）→ 跳过重算 → 返回上次结果。
//   - 在 Vue computed 里调用 selector：state ref 没变 → computed 不重算 → 模板不重渲染。
//
// 默认导出一组 module-level selectors（足够支撑单 store 场景），
// 同时暴露 createSelectors() 工厂方便测试隔离与多实例场景。

import { memo } from './memo'
import type { AppState } from './state'
import type { Forward, Host, ServerStatus } from './types'

export interface HostView {
  alias: string
  host_name: string
  user: string
  port: number
  enabled: boolean
  status: ServerStatus | undefined
}

function buildByServerForwards(forwards: ReadonlyArray<Forward>): Record<string, Forward[]> {
  const out: Record<string, Forward[]> = {}
  for (const f of forwards) {
    const arr = out[f.server_id] || (out[f.server_id] = [])
    arr.push(f)
  }
  return out
}

function buildHostsView(input: {
  hosts: ReadonlyArray<Host>
  enabledHosts: ReadonlyArray<string>
  serverStatus: Readonly<Record<string, ServerStatus>>
}): HostView[] {
  const enabledSet = new Set(input.enabledHosts)
  return input.hosts.map((h) => ({
    alias: h.alias,
    host_name: h.host_name,
    user: h.user,
    port: h.port,
    enabled: enabledSet.has(h.alias),
    status: input.serverStatus[h.alias],
  }))
}

// HostView 派生于 hosts / enabledHosts / serverStatus 三个独立 ref。
// 用 memo 需要一个稳定的「输入对象」，但每次现拼对象会让 input ref 总是变化。
// 所以这里用 multiInput memo：维持上一次的三元组，只要三个 ref 都没变就返回上次结果。
function memoTri<A, B, C, O>(
  pickA: (s: AppState) => A,
  pickB: (s: AppState) => B,
  pickC: (s: AppState) => C,
  compute: (a: A, b: B, c: C) => O,
): (s: AppState) => O {
  let lastA: A
  let lastB: B
  let lastC: C
  let lastOut: O
  let init = false
  return (s: AppState) => {
    const a = pickA(s)
    const b = pickB(s)
    const c = pickC(s)
    if (init && a === lastA && b === lastB && c === lastC) return lastOut
    lastA = a
    lastB = b
    lastC = c
    lastOut = compute(a, b, c)
    init = true
    return lastOut
  }
}

// createSelectors 工厂：每个调用返回一组独立的、有自己 memoize 缓存的 selectors。
// 测试里用它做实例隔离；生产用 module-level 默认实例。
export function createSelectors() {
  const byServer = memo(
    (s: AppState) => s.forwards,
    (forwards) => buildByServerForwards(forwards),
  )

  const hostsViewMemo = memoTri(
    (s: AppState) => s.hosts,
    (s: AppState) => s.enabledHosts,
    (s: AppState) => s.serverStatus,
    (hosts, enabled, status) =>
      buildHostsView({ hosts, enabledHosts: enabled, serverStatus: status }),
  )

  return {
    byServerForwards: byServer,
    hostsView: hostsViewMemo,
    forwardsByHost: (s: AppState, alias: string) => byServer(s)[alias] || [],
  }
}

const _default = createSelectors()
export const byServerForwards = _default.byServerForwards
export const hostsView = _default.hostsView
export const forwardsByHost = _default.forwardsByHost

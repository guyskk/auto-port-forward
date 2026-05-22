// core/reducer.ts —— 纯函数 reducer：(state, event) -> state。
//
// 性能契约（最关键）：
//   内容相同的事件返回原 state 引用。
//   Vue 的 computed/template 依赖 ref 引用变化触发重算；
//   如果 reducer 不变更 ref，整个组件树就不会重渲染——
//   这就是治理"事件风暴 → 全量重绘雪崩"的核心机制。
//
// 不可变性：每次"有变化"的更新都返回新对象/新数组；
// 旧引用永不被原地修改，方便 selector 用引用相等做 memoize。

import type { AppState } from './state'
import type { Event, Forward, ServerStatus } from './types'

function forwardCoreEqual(a: Forward, b: Forward): boolean {
  // 等值比较刻意忽略 last_active：
  // 稳态扫描每秒都可能写入新的 last_active，但 UI 不用它做关键展示，
  // 不应该因此重渲染整页。其余关键字段（status/error/local_port/remote.*）
  // 才是用户能感知到的变化。
  if (a === b) return true
  if (a.server_id !== b.server_id) return false
  if (a.remote_port !== b.remote_port) return false
  if (a.local_port !== b.local_port) return false
  if (a.status !== b.status) return false
  if ((a.error || '') !== (b.error || '')) return false
  const ra = a.remote
  const rb = b.remote
  if (ra === rb) return true
  if (!ra || !rb) return ra === rb
  return (
    ra.port === rb.port &&
    ra.bind_addr === rb.bind_addr &&
    ra.ip_version === rb.ip_version &&
    ra.pid === rb.pid &&
    ra.process === rb.process &&
    ra.command === rb.command &&
    ra.docker_image === rb.docker_image
  )
}

function forwardsContentEqual(
  prev: ReadonlyArray<Forward>,
  next: ReadonlyArray<Forward>,
): boolean {
  if (prev === next) return true
  if (prev.length !== next.length) return false
  for (let i = 0; i < prev.length; i++) {
    if (!forwardCoreEqual(prev[i], next[i])) return false
  }
  return true
}

function serverStatusEqual(
  a: ServerStatus | undefined,
  b: ServerStatus,
): boolean {
  if (!a) return false
  return (
    a.server_id === b.server_id &&
    a.state === b.state &&
    a.attempt === b.attempt &&
    a.disconnected_ms === b.disconnected_ms &&
    (a.error || '') === (b.error || '')
  )
}

function stringArrayEqual(
  a: ReadonlyArray<string>,
  b: ReadonlyArray<string>,
): boolean {
  if (a === b) return true
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false
  }
  return true
}

function hostsEqual(
  a: ReadonlyArray<{ alias: string; host_name: string; user: string; port: number }>,
  b: ReadonlyArray<{ alias: string; host_name: string; user: string; port: number }>,
): boolean {
  if (a === b) return true
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    const ai = a[i]
    const bi = b[i]
    if (
      ai.alias !== bi.alias ||
      ai.host_name !== bi.host_name ||
      ai.user !== bi.user ||
      ai.port !== bi.port
    ) {
      return false
    }
  }
  return true
}

// applyEvent 是状态机的唯一入口。所有 mutation 都经此通过返回新 state 完成；
// 对外只能读 prev、写 next。
export function applyEvent(prev: AppState, ev: Event): AppState {
  switch (ev.kind) {
    case 'state-update': {
      const next = ev.forwards
      if (forwardsContentEqual(prev.forwards, next)) return prev
      return { ...prev, forwards: next, lastScanAt: Date.now() }
    }

    case 'forward-update': {
      const idx = prev.forwards.findIndex(
        (f) => f.server_id === ev.serverId && f.remote_port === ev.port,
      )
      if (idx < 0) return prev
      const cur = prev.forwards[idx]
      if (cur.status === ev.status && (cur.error || '') === (ev.error || '')) {
        return prev
      }
      const nextForwards = prev.forwards.slice()
      nextForwards[idx] = { ...cur, status: ev.status, error: ev.error }
      return { ...prev, forwards: nextForwards }
    }

    case 'server-status': {
      if (!ev.status.server_id) return prev
      const old = prev.serverStatus[ev.status.server_id]
      if (serverStatusEqual(old, ev.status)) return prev
      return {
        ...prev,
        serverStatus: { ...prev.serverStatus, [ev.status.server_id]: ev.status },
      }
    }

    case 'scan-error': {
      if (prev.lastError === ev.error) return prev
      return { ...prev, lastError: ev.error }
    }

    case 'config-loaded': {
      // config 字段较多但写入频次低，直接 JSON 等值即可；
      // 真正需要细粒度比较时再换成 deep equal。
      if (prev.config && JSON.stringify(prev.config) === JSON.stringify(ev.config)) {
        return prev
      }
      return { ...prev, config: ev.config }
    }

    case 'hosts-loaded': {
      const hostsSame = hostsEqual(prev.hosts, ev.hosts)
      const enabledSame = stringArrayEqual(prev.enabledHosts, ev.enabled)
      if (hostsSame && enabledSame) return prev
      return {
        ...prev,
        hosts: hostsSame ? prev.hosts : ev.hosts,
        enabledHosts: enabledSame ? prev.enabledHosts : ev.enabled,
      }
    }

    case 'enabled-hosts-updated': {
      if (stringArrayEqual(prev.enabledHosts, ev.enabled)) return prev
      return { ...prev, enabledHosts: ev.enabled }
    }

    case 'loading': {
      if (prev.loading === ev.on) return prev
      return { ...prev, loading: ev.on }
    }

    case 'scan-finished': {
      // 即使 lastScanAt 完全相同也认为是一次"扫描完成"——
      // 但既然 ref 不变化等价于无事发生，仍然走引用相等优化。
      if (prev.lastScanAt === ev.at) return prev
      return { ...prev, lastScanAt: ev.at }
    }
  }
}

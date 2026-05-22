# 端口转发页面卡顿治理 + 前端数据逻辑层剥离

> 讨论日志：[docs/discuss-20260522-port-forward-page-perf.md](../../docs/discuss-20260522-port-forward-page-perf.md)
> 实施日期：2026-05-22 起

## Context

用户反馈端口转发页面（MonitorView）**持续不动也一直卡，点端口开关时尤其卡**，连带电脑都卡。只配了 1 个 SSH server（远端约几十个 listening 端口）。

经过两轮代码勘察（详见讨论日志），定性结论：

- **不是死循环，不是 SSH 后端 busy-loop**（重连有完整指数退避；scanTick 也只在 15s ticker 上触发）
- **真正的卡顿原因是事件风暴 + 前端全量重渲染雪崩**：
  - 后端每 15s 无条件 emit 完整 `EventStateUpdate`（即使内容一字未变）
  - 每条 forward 启停额外 emit per-forward 的 `EventForwardUpdate`
  - 前端收到任一事件就 `forwards.value = toArray(data)` 整数组替换（Vue 无法识别"内容相同"）
  - 模板里 `forwardsOf` / `isEnabled` / `connLabel` 都是普通函数而不是 computed，每次重渲染都 O(host×forward) 重 filter
  - 收到 `EventForwardUpdate` 又额外 `api.GetSnapshot()` 反向 IPC 拉一次全量 → N 倍放大
- 另外发现 **`engine/mutate.go:12-17` 的 `ToggleForward` 是空壳**（标 `TODO(M7+)`）：用户点端口开关其实啥都没做，全部卡顿来自被它触发的完整 scan 链。

用户最终决策：

1. **路径 B**：建 `frontend/src/core/` 纯逻辑层（不 import vue / 不 import wails runtime）+ vitest 单测 + 重构 store + 顺手修四个前端 bug。后端 emit 去抖暂不做。
2. **`ToggleForward` 语义：持久化用户禁用意图**（写入 `config.toml`，重启仍记得，下次 reconcile 跳过这些端口）。
3. **引入 vitest**。

预期收益：

- 稳态下事件即使到达，纯逻辑层做内容 diff 后返回原引用，Vue 不会被无意义重渲染
- 模板里走 memoized selector，几十个端口的卡片刷新成本可忽略
- `ToggleForward` 真正落到 config 持久化，关掉的端口不会被下一次扫描"复活"
- 「事件风暴 → state 变化次数」可以用 vitest 直接断言，性能问题变成可验证的契约

## 总览：要做的事

| 模块 | 改动 | 大致代码量 |
|------|------|----------|
| 后端 `internal/config/` | `Rules` 增加 `DisabledPorts map[string][]int` 字段（per-host），新增 `Store.SetForwardEnabled(alias, port, on)` | ~80 行 + ~60 行单测 |
| 后端 `internal/engine/reconcile.go` | `Inputs` 增加 `DisabledPorts []int`，Reconcile 跳过被禁用的端口 | ~30 行 + ~80 行单测 |
| 后端 `internal/engine/mutate.go` | `ToggleForward` 真实实现：调 store 写盘 + 触发 ScanNow | ~40 行 |
| 后端 `internal/engine/runtime.go` | `scanServer` 把 disabled 列表喂给 Reconcile | ~10 行 |
| 后端 `app.go` | `ToggleForward` 路径接 store；提供 `disabled_ports` 到 GetConfig 返回 | ~10 行 |
| 前端 `frontend/src/core/` | 新建：types / state / reducer / selectors / intent + 测试 | ~400 行 + ~400 行单测 |
| 前端 `frontend/src/api/` | 抽 `ApiDeps` 接口；wails 实现 + mock 实现走同一契约 | ~40 行 |
| 前端 `frontend/src/store/state.ts` | 重构为薄 Pinia adapter（持有 `ref<AppState>`，事件 → reducer → 引用变才赋值） | ~150 行（净减少） |
| 前端 `frontend/src/views/MonitorView.vue` | 用 store 暴露的 computed selector 替代行内函数；删除 `onTogglePort` 里的 `await scanNow()`（后端会自己触发） | ~30 行变动 |
| 前端 `frontend/package.json` + `vite.config.ts` | 加 vitest / @vue/test-utils / jsdom；script 加 `test` | ~15 行 |

## 详细方案

### 一、后端：ToggleForward 持久化

#### 数据模型变更（`internal/config/config.go`）

在 `Config` 顶层加 per-host 的禁用端口集合。**不放进 `Rules`**——`Rules` 是全局过滤规则，per-host 禁用是用户意图，语义不同。

```go
type Config struct {
    ScanIntervalSec int                  `toml:"scan_interval_sec" json:"scan_interval_sec"`
    Rules           Rules                `toml:"rules" json:"rules"`
    EnabledHosts    []string             `toml:"enabled_hosts" json:"enabled_hosts"`
    DisabledPorts   map[string][]int     `toml:"disabled_ports" json:"disabled_ports"` // alias → 禁用的远端端口列表（已去重已排序）
}
```

`applyDefaults` 把 nil map 兜底为空 map（参考 1f53c05 PR 的 nil-slice 处理思路）。

#### Store 增加方法（`internal/config/store.go`）

```go
// DisabledPorts 返回某 alias 的禁用端口快照（排序后的拷贝；alias 不存在返回 nil）。
func (s *Store) DisabledPorts(alias string) []int

// SetForwardEnabled 设置 alias:port 是否启用并持久化。
// on=true: 从禁用列表中移除（幂等）
// on=false: 加入禁用列表（去重排序，幂等）
func (s *Store) SetForwardEnabled(alias string, port int, on bool) error
```

实现风格完全对齐 `SetHostEnabled`（持锁修改 → 克隆 → 锁外 Save）。

`cloneConfig` 增加 `DisabledPorts` 的深拷贝（参考已有 `cloneStrings` / `cloneInts` 模式：nil → [] 兜底以保证 JSON 序列化正常）。

#### Reconcile 跳过禁用端口（`internal/engine/reconcile.go`）

```go
type Inputs struct {
    ServerID       string
    Remote         []model.RemotePort
    LocalOccupied  map[int]LocalOwnership
    CurrentForward map[int]bool
    DisabledPorts  map[int]bool          // ← 新增（remote port → true）
    Rules          config.Rules
    IsRoot         bool
}
```

`Reconcile` 内对每个 remote port，先检查是否在 `DisabledPorts` 里——如果是：
- Status 设为 `model.StatusExcluded`（复用已有状态；UI 上会显示成灰色"已排除"）
- **不**放入 `desiredSet`（即使在跑也会被 diff 出 `del`）
- 仍输出到 `Snapshot`（让前端表格能看到这一行 + 开关状态为关）

这保证 UI 上禁用的端口仍然显示，只是不转发。

> 备选过：新增一个 `StatusDisabled` 状态。否决，因为禁用与"命中黑名单"在用户视角上等价，且新增状态会带来更多前端文案/颜色映射改动，性价比低。

#### runtime.go 接入

`scanServer` 调用 Reconcile 前从 `e.cfg.DisabledPorts[st.cfg.Alias]` 取列表并转为 map。

注意：`e.cfg` 在 `Engine` 中是被 `UpdateRules` / `ApplyServers` 显式更新的快照，不会自动跟随 `Store`——所以也要：

- 新增 `Engine.UpdateDisabledPorts(alias string, ports []int)`：更新 `e.cfg.DisabledPorts[alias]` 并触发 `ScanNow`
- 在 `mutate.go::ToggleForward` 中调它

#### `ToggleForward` 真实实现（`internal/engine/mutate.go`）

```go
func (e *Engine) ToggleForward(serverID string, port int, on bool) error {
    e.mu.Lock()
    if e.cfg.DisabledPorts == nil {
        e.cfg.DisabledPorts = map[string][]int{}
    }
    cur := append([]int(nil), e.cfg.DisabledPorts[serverID]...)
    next := toggleInSet(cur, port, !on)  // on=true → 从禁用列表移除；on=false → 加入
    e.cfg.DisabledPorts[serverID] = next
    running := e.running
    e.mu.Unlock()
    if running {
        _ = e.ScanNow(context.Background())
    }
    return nil
}
```

> 注：**engine 层不直接写盘**——持久化由调用方（`app.go`）负责，与 `UpdateRules` / `SetHostEnabled` 的现有分层一致（engine 只管运行时状态，store 管持久化）。

#### `app.go::ToggleForward` 串联

```go
func (a *App) ToggleForward(serverID string, port int, on bool) error {
    if a.store == nil {
        return errStoreNotReady
    }
    if err := a.store.SetForwardEnabled(serverID, port, on); err != nil {
        return err
    }
    if a.engine != nil {
        return a.engine.ToggleForward(serverID, port, on)
    }
    return nil
}
```

启动时把 store 的 `DisabledPorts` 灌进 engine：`setup` 中 `engine.New` 传入的 `store.Snapshot()` 已经包含此字段，无需额外改动。

#### 单测

- `config/store_test.go`：增加 `SetForwardEnabled` 的幂等性 / 跨重启持久化 / 多 host 隔离测试
- `engine/reconcile_test.go`：增加"禁用端口被标 Excluded 且 Diff 输出 del"的测试
- `engine/engine_test.go` 或新文件 `mutate_test.go`：`ToggleForward` 路径触发 ScanNow 的测试（已有 `testhelpers_test.go` 可复用）

---

### 二、前端 core/ 纯逻辑层

#### 目录结构

```
frontend/src/
├── core/                              # ★ 新增（零 vue/wails runtime 依赖）
│   ├── types.ts                       # 从 ../types.ts re-export + Event 类型
│   ├── state.ts                       # AppState + initialState
│   ├── reducer.ts                     # applyEvent(state, ev) 纯函数（内容等价时返回原引用）
│   ├── selectors.ts                   # forwardsByServer / enabledHostSet / connLabel / sortedHosts
│   ├── memo.ts                        # 极简的"基于输入引用相等"的 memoize 工具
│   ├── intent.ts                      # 用户意图 → API 调用计划（接收 ApiDeps 注入）
│   └── __tests__/
│       ├── reducer.test.ts
│       ├── selectors.test.ts
│       ├── intent.test.ts
│       └── perf-contract.test.ts      # 关键：断言事件风暴不引起额外重渲染
│
├── api/
│   ├── deps.ts                        # ★ 新增 ApiDeps 接口（getConfig/getSnapshot/...）
│   ├── wails.ts                       # 实现 ApiDeps；保持 mock fallback
│   └── mock.ts                        # 保持，同样实现 ApiDeps
│
├── store/
│   └── state.ts                       # ★ 重构：薄薄一层 Pinia adapter
│
└── views/
    ├── MonitorView.vue                # 改用 store 暴露的 computed selector
    └── ...（其他不动）
```

#### `core/state.ts`

```ts
export interface AppState {
  hosts: ReadonlyArray<Host>
  enabledHosts: ReadonlyArray<string>
  forwards: ReadonlyArray<Forward>
  serverStatus: Readonly<Record<string, ServerStatus>>
  config: Config | null
  lastScanAt: number | null
  lastError: string
  loading: boolean
}

export function initialState(): AppState
```

`Readonly` 仅做类型层面提示——reducer 内部仍走"产生新对象再赋值"模式，不强行 Object.freeze（运行时开销）。

#### `core/reducer.ts`

```ts
export type Event =
  | { kind: 'state-update'; forwards: Forward[] }
  | { kind: 'forward-update'; serverId: string; port: number; status: PortStatus; error: string }
  | { kind: 'server-status'; status: ServerStatus }
  | { kind: 'scan-error'; error: string }
  | { kind: 'config-loaded'; config: Config }
  | { kind: 'hosts-loaded'; hosts: Host[]; enabled: string[] }
  | { kind: 'host-toggle-local'; alias: string; on: boolean; nextEnabled: string[] }
  | { kind: 'loading'; on: boolean }
  | { kind: 'scan-now-finished'; at: number }

export function applyEvent(prev: AppState, ev: Event): AppState
```

**最关键的实现细节**——这是性能修复的核心：

```ts
case 'state-update': {
  if (forwardsContentEqual(prev.forwards, ev.forwards)) {
    return prev   // ← 引用相等，Vue computed 不会重算，模板不会重渲染
  }
  return { ...prev, forwards: ev.forwards, lastScanAt: Date.now() }
}

case 'server-status': {
  const old = prev.serverStatus[ev.status.server_id]
  if (serverStatusEqual(old, ev.status)) return prev
  return { ...prev, serverStatus: { ...prev.serverStatus, [ev.status.server_id]: ev.status } }
}
```

`forwardsContentEqual` 实现要点：先长度，再按 `(server_id, remote_port)` 逐字段比较（包含 status / error / last_active）。**`last_active` 稳态确实不变**（已亲自核对 `engine.go:175` 只在 startForward 时 Store），所以这个相等比较在稳态下会持续命中"相等"分支。

#### `core/selectors.ts`

```ts
export const forwardsByServer = memo(
  (s: AppState) => s.forwards,
  (forwards): ReadonlyMap<string, ReadonlyArray<Forward>> => {
    const m = new Map<string, Forward[]>()
    for (const f of forwards) {
      const list = m.get(f.server_id)
      if (list) list.push(f)
      else m.set(f.server_id, [f])
    }
    return m
  }
)

export const enabledHostSet = memo(
  (s: AppState) => s.enabledHosts,
  (e) => new Set(e)
)

export const sortedHosts = memo(
  (s: AppState) => [s.hosts, s.enabledHosts] as const,
  ([hosts, enabled]) => {
    const set = new Set(enabled)
    return [...hosts].sort((a, b) => {
      const ea = set.has(a.alias) ? 0 : 1
      const eb = set.has(b.alias) ? 0 : 1
      if (ea !== eb) return ea - eb
      return a.alias.localeCompare(b.alias)
    })
  }
)

export function connLabel(s: AppState, alias: string): { label: string; type: ... }
```

#### `core/memo.ts`

极简版（不引第三方库）：

```ts
export function memo<I, O>(
  pick: (s: AppState) => I,
  compute: (input: I) => O
): (s: AppState) => O {
  let lastInput: I | undefined
  let lastOutput: O | undefined
  let initialized = false
  return (s: AppState) => {
    const input = pick(s)
    if (initialized && input === lastInput) return lastOutput!
    lastOutput = compute(input)
    lastInput = input
    initialized = true
    return lastOutput
  }
}
```

注意：**这个 memo 是「单实例缓存」**，全局只有 1 个 store 调一次，足够。多实例场景另议。

#### `core/intent.ts`（用户意图 → API 调用序列）

```ts
export interface ApiDeps {
  getConfig(): Promise<Config>
  getSnapshot(): Promise<Forward[]>
  listHosts(): Promise<Host[]>
  enabledHosts(): Promise<string[]>
  setHostEnabled(alias: string, on: boolean): Promise<void>
  reloadSSHConfig(): Promise<void>
  testHost(alias: string): Promise<void>
  scanNow(): Promise<void>
  toggleForward(serverId: string, port: number, on: boolean): Promise<void>
  updateRules(r: Rules): Promise<void>
  updateScanInterval(sec: number): Promise<void>
}

export async function refresh(deps: ApiDeps, dispatch: (ev: Event) => void): Promise<void>
export async function togglePort(deps: ApiDeps, dispatch, serverId, port, on): Promise<void>
// ...
```

intent 函数不持有状态，只 dispatch event。可注入 mock ApiDeps 做单测，断言"调一次 togglePort 期间 dispatch 了哪些 event，调了哪些 API 方法"。

#### `core/__tests__/perf-contract.test.ts`（性能契约用例）

```ts
it('snapshot 内容相同时返回 prev 引用', () => {
  const forwards = [makeForward('a', 22), makeForward('a', 80)]
  const s1 = applyEvent(initialState(), { kind: 'state-update', forwards })
  const s2 = applyEvent(s1, { kind: 'state-update', forwards: [...forwards] }) // 不同数组实例，相同内容
  expect(s2).toBe(s1)
})

it('connectLoop 抖动 100 次只产生 1 次有效变化', () => {
  let s = initialState()
  const status = { server_id: 'a', state: 'connected' as const, attempt: 0, disconnected_ms: 0 }
  let changes = 0
  for (let i = 0; i < 100; i++) {
    const next = applyEvent(s, { kind: 'server-status', status: { ...status } })
    if (next !== s) changes++
    s = next
  }
  expect(changes).toBe(1)
})

it('forwardsByServer 同输入返回同引用', () => {
  const s = /* 构造 */
  expect(selectors.forwardsByServer(s)).toBe(selectors.forwardsByServer(s))
})
```

这就是用户要的"能单独验证数据逻辑"。

---

### 三、store/state.ts 重构为薄 Pinia adapter

```ts
import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { applyEvent, initialState, type AppState, type Event } from '../core/reducer'
import * as selectors from '../core/selectors'
import { api, onEvent } from '../api/wails'
import * as intent from '../core/intent'
import { EVENT_STATE_UPDATE, EVENT_SERVER_STATUS, EVENT_SCAN_ERROR, EVENT_FORWARD_UPDATE } from '../types'

export const useAppStore = defineStore('app', () => {
  const state = ref<AppState>(initialState())

  function dispatch(ev: Event) {
    const next = applyEvent(state.value, ev)
    if (next !== state.value) {       // ★ 引用相等检查 = 关键
      state.value = next
    }
  }

  // 暴露的字段都走 selector，模板里零计算
  const hosts = computed(() => state.value.hosts)
  const enabledHosts = computed(() => state.value.enabledHosts)
  const forwards = computed(() => state.value.forwards)
  const config = computed(() => state.value.config)
  const loading = computed(() => state.value.loading)
  const lastScanAt = computed(() => state.value.lastScanAt)
  const lastError = computed(() => state.value.lastError)
  const serverStatus = computed(() => state.value.serverStatus)

  // 派生
  const sortedHosts = computed(() => selectors.sortedHosts(state.value))
  const forwardsByServer = computed(() => selectors.forwardsByServer(state.value))
  const enabledHostSet = computed(() => selectors.enabledHostSet(state.value))

  // 意图 → 走 intent.ts
  const refresh = () => intent.refresh(api, dispatch)
  const scanNow = () => intent.scanNow(api, dispatch)
  const setHostEnabled = (alias, on) => intent.setHostEnabled(api, dispatch, alias, on)
  const toggleForward = (serverId, port, on) => intent.toggleForward(api, dispatch, serverId, port, on)
  // ...

  function subscribe() {
    onEvent(EVENT_STATE_UPDATE, (data) => {
      dispatch({ kind: 'state-update', forwards: toArray(data as Forward[]) })
    })
    onEvent(EVENT_FORWARD_UPDATE, (data) => {
      // ★ 不再反向 GetSnapshot；EventStateUpdate 会带全量；这里只更新单条字段
      const d = data as { server_id; remote_port; status; error }
      dispatch({ kind: 'forward-update', serverId: d.server_id, port: d.remote_port, status: d.status, error: d.error })
    })
    onEvent(EVENT_SERVER_STATUS, (data) => {
      dispatch({ kind: 'server-status', status: data as ServerStatus })
    })
    onEvent(EVENT_SCAN_ERROR, (data) => {
      const d = data as { error?: string }
      dispatch({ kind: 'scan-error', error: d?.error || 'scan error' })
    })
  }

  return {
    hosts, enabledHosts, forwards, config, loading, lastScanAt, lastError, serverStatus,
    sortedHosts, forwardsByServer, enabledHostSet,
    refresh, scanNow, setHostEnabled, toggleForward, /* ... */
    subscribe,
  }
})
```

关键改动总结：
- ✅ 删除 R3：EVENT_FORWARD_UPDATE 不再反向 GetSnapshot
- ✅ 删除 R7：scanNow 不再末尾自己拉 snapshot（intent.scanNow 也不需要——后端 emit 会带全量）
- ✅ 修复 R2：reducer 引用相等时 ref 不赋值
- ✅ 修复 R8：reducer 内对 serverStatus 也做等值检查后才换引用

---

### 四、MonitorView.vue 改造

```vue
<script setup lang="ts">
const store = useAppStore()
// 删除原来的 isEnabled / connLabel / forwardsOf / sortedHosts —— 都从 store 取
const forwardsByServer = computed(() => store.forwardsByServer)
const enabledHostSet = computed(() => store.enabledHostSet)

async function onTogglePort(serverId: string, port: number, on: boolean) {
  await store.toggleForward(serverId, port, on)
  // ★ 不再手动 scanNow —— 后端 ToggleForward 内部会触发 ScanNow，前端会通过事件收到更新
}
</script>

<template>
  <!-- ... -->
  <port-table
    v-if="(forwardsByServer.get(h.alias) ?? []).length > 0"
    :data="forwardsByServer.get(h.alias) ?? []"
    @toggle="onTogglePort"
  />
  <!-- 用 enabledHostSet.has(h.alias) 替代 isEnabled(h.alias) -->
  <!-- 用 store.serverStatus[h.alias] 派生 connLabel（可以保留 connLabel 工具函数，但只接受 status 参数，不读 store） -->
</template>
```

---

### 五、引入 vitest

#### `frontend/package.json` 新增

```json
"scripts": {
  "test": "vitest run",
  "test:watch": "vitest"
},
"devDependencies": {
  "vitest": "^2.1.0",
  "@vitest/ui": "^2.1.0",
  "jsdom": "^25.0.0"
}
```

#### `frontend/vite.config.ts` 增加 test 块

```ts
// @ts-ignore - vitest extends vite config
test: {
  environment: 'jsdom',
  include: ['src/**/*.test.ts'],
  globals: false,
}
```

> 注：core 层单测**不需要** jsdom（纯 ts），但保留 jsdom 是为未来如果需要测 Vue 组件留口子；当前不写 Vue 组件单测（UI 部分按 CLAUDE.md 约定走 playwright 手工验收）。

## 实施步骤（按 TDD + BPR 协作）

CLAUDE.md 强调 TDD 与 BPR。本次任务按业务维度分 3 个 BPR 子任务串行实施（每个子任务都先写测试再写实现）：

1. **BPR-1 后端：DisabledPorts + ToggleForward 持久化**
   - 子任务分支：`feat/disabled-ports-persist`
   - 顺序：
     1. 改 `config/config.go` 加字段 + 测试
     2. 改 `config/store.go` 加 `SetForwardEnabled` / `DisabledPorts` + 测试（参考已有 `SetHostEnabled` 测试模式）
     3. 改 `engine/reconcile.go` 接收 disabled + Reconcile 跳过 + 测试
     4. 改 `engine/mutate.go::ToggleForward` 真实实现 + 测试
     5. 改 `engine/runtime.go::scanServer` 喂 disabled
     6. 改 `app.go::ToggleForward` 串联 store
   - 完成后跑 `go test ./... -count=1` 全通过
   - 合并到 main

2. **BPR-2 前端：core/ 纯逻辑层 + vitest**
   - 子任务分支：`feat/frontend-core-domain`
   - 顺序（**TDD**）：
     1. `package.json` + `vite.config.ts` 配 vitest，先跑通空骨架 `npm run test`
     2. 写 `core/types.ts` `core/state.ts` `core/memo.ts`
     3. 写 `core/reducer.test.ts`（包含 perf-contract 用例），再写 `core/reducer.ts` 让测试通过
     4. 写 `core/selectors.test.ts` → `core/selectors.ts`
     5. 写 `core/intent.test.ts`（mock ApiDeps）→ `core/intent.ts`
     6. 写 `api/deps.ts`（ApiDeps 接口）；调整 `api/wails.ts` `api/mock.ts` 让它们实现 `ApiDeps`（实际上现在的 `WailsBindings` 已经几乎一致，只是命名 camelCase 化）
   - 合并到 main

3. **BPR-3 前端：store + MonitorView 接入新 core**
   - 子任务分支：`feat/frontend-store-refactor`
   - 顺序：
     1. 重写 `store/state.ts` 为 reducer adapter
     2. 改 `MonitorView.vue` 用 store 暴露的 selector / 删除手动 scanNow
     3. 用 playwright-cli 真实启动 wails dev 手工验收：
        - 启用 host → 看到端口列表 → CPU 不再持续高
        - 点关闭某端口开关 → 该端口立即灰显（excluded）→ 重启应用后仍然 disabled
        - 点开启某端口 → 几秒内恢复 forwarding
        - 持续观察 1 分钟无 host 状态抖动情况下 CPU 占用
   - 合并到 main

每个 BPR 都通过我自己审查 PR 后合并，按用户指示**合并到 main 需要用户确认**。

---

## Verification

### 单元测试

```bash
# 后端
go test ./... -count=1

# 前端（新加）
cd frontend && npm run test
```

关键断言（实现完成后必须 pass）：

- `engine/reconcile_test.go`：禁用某端口后，Diff 输出 `del`，下次 Reconcile Snapshot 仍包含该端口但 Status=excluded
- `config/store_test.go`：`SetForwardEnabled` 写入后再 `Load` 同一文件，禁用集合保留
- `core/reducer.test.ts`：内容相同的 state-update 事件返回原 state 引用
- `core/reducer.test.ts`：100 次相同 server-status 事件只产生 1 次引用变化
- `core/selectors.test.ts`：`forwardsByServer` 同输入返回同输出引用

### 端到端手工验收（playwright-cli）

按 BPR-3 步骤 3 操作。重点观察：

1. 启动后页面初始渲染不卡（之前是几十个 forward 同时启动 → N 次重渲染雪崩）
2. 稳态下页面操作（鼠标 hover / 切 tab）响应正常
3. 关闭某端口 → 立刻看到状态变 excluded（< 200ms）→ kill 应用 → 重启后仍 disabled
4. macOS 活动监视器观察 CPU：单 host + 30 端口稳态下应 < 5% CPU（之前用户反馈"整机卡"说明明显异常）

### 回归检查

- ListHosts / EnabledHosts / TestHost 流程不动
- Settings 页面 Rules 修改正常
- Mock 浏览器模式开发不破坏（`api/mock.ts` 与新 ApiDeps 接口对齐）

---

## 风险与权衡

| 项 | 风险 | 缓解 |
|----|------|------|
| 引入 jsdom | 增加 frontend 依赖体积 | 仅 devDep，不进生产包 |
| reducer 内容相等判断的成本 | 每次 state-update 都 O(N) 比较 forwards | N 几十量级，比 Vue 重渲染整张表便宜数量级 |
| memo 是单实例 | 多 store 实例会失效 | 本项目只有 1 个 Pinia store 实例，OK；将来如改成多实例需重做 |
| DisabledPorts 配置膨胀 | 用户禁用了某 host 后又把 host 删了，孤儿数据 | 当前 EnabledHosts 已有同样问题（README 注明"孤儿别名状态保留"），按相同语义处理；不主动清理 |
| `engine/mutate.go::ToggleForward` 改实现破坏既有调用方 | app.go 是唯一调用方，且现在是空操作 | 全代码搜索确认仅 app.go 一处调用，无破坏面 |
| 后端 emit 没改，事件量不变 | 仍每 15s 一次 + per-forward N 次 | 前端 reducer 的内容相等检查会吸收掉所有"无变化"事件，效果等价；后端去抖留作未来优化 |

---

## 不在本次范围内（明确不做）

- 后端 emit 去抖 / 内容指纹（路径 C）
- 抽 EventBus 接口支持非 Wails 前端
- Vue 组件单测
- forward 字节计数 / 活动连接数等新指标（model.Forward 字段不动）
- `LastActive` 字段语义调整（保持现状："开始转发的时间戳"）

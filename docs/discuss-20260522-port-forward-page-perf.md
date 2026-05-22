# 端口转发页面卡顿性能排查讨论

> 开始日期: 2026-05-22
> 主题: 端口转发页面 UI 极度卡顿，连带导致整机卡，疑似 SSH 查询相关代码存在死循环或资源过度占用

## 用户反馈

- 现象：端口转发页面性能特别差，特别卡顿，导致整个电脑都有点卡
- 触发条件：只开了**一个**自动转发的服务器（即配置只有 1 个 server，资源消耗本不应这么高）
- 用户怀疑：
  - SSH 相关的查询进程有问题
  - 是否存在死循环
  - 资源占用太高

## 排查思路

不能猜，先扫清三块：
1. 后端 — 扫描循环 / SSH 连接守护 / 事件 emit 节奏；尤其要看 ticker、retry、goroutine 泄漏
2. 前端 — Pinia 状态订阅 / Vue 响应式 / 表格渲染；尤其要看是否每个事件都触发了全量重渲染
3. 数据流 — events 名字与频率，前后端事件契约，是否在某种状态下事件被打成无限循环（后端 emit → 前端调用 wails 方法 → 后端再 emit）

下面按维度记录探索发现。


---

## 第一轮探索：三个 Agent 并行排查（后端 / 前端 / 数据流）

### 后端发现摘要

- 扫描周期默认 **15 秒**（`internal/config/config.go:38`，`<=0` 强制改为 15）
- 只有 **1 个 ticker**（`internal/engine/engine.go:239`），无独立本地扫描 ticker，本地 / 远端共享 scanTick
- 重连退避：Initial 500ms / Max 60s / 累计 15 分钟才标 degraded（`internal/sshctl/reconnect.go:13-39`）— **不会 busy-loop**
- 每次 scanTick 完成会无条件 emit 一次 `EventStateUpdate`（完整 Snapshot，深拷贝，`internal/engine/runtime.go:54-55`）
- **每条 forward 启 / 停时各 emit 一次 `EventForwardUpdate`**（`internal/engine/runtime.go:181`，用了 `context.Background()`）
- connectLoop 中状态变化时 emit `EventServerStatus`（`internal/engine/engine.go:173/178/184/197`）
- `Snapshot()` 方法持 snapshotMu + 嵌套 serverState 锁，深拷贝整个状态（`engine.go:310-349`）
- 配置 Save 只在用户操作（SetHostEnabled / UpdateRules / UpdateScanInterval）时触发，scanTick **不写盘**
- 没有 busy-loop，没有 goroutine 泄漏的直接证据

### 前端发现摘要（核心嫌疑！）

发现位置 `frontend/src/store/state.ts:86-103`：

```ts
onEvent(EVENT_STATE_UPDATE, (data) => {
  forwards.value = toArray(data)           // 整个数组替换
})
onEvent(EVENT_FORWARD_UPDATE, () => {
  api.GetSnapshot().then((s) => (forwards.value = toArray(s)))  // ★ 每次单端口事件都拉全量 + 又整个数组替换
})
onEvent(EVENT_SERVER_STATUS, (data) => {
  const s = data as ServerStatus
  if (s?.server_id) {
    serverStatus.value = { ...serverStatus.value, [s.server_id]: s }   // 每次都 spread 新建整个 Record
  }
})
```

`frontend/src/views/MonitorView.vue` 进一步放大：

- 第 48 行 `forwardsOf` 是**普通箭头函数**而不是 computed → 每次模板更新都重新 filter 整个数组
- 第 30-45 行 `isEnabled`、`connLabel` 同样无缓存
- 第 131、144-145、149、173、175-176 行模板中**多次调用**这些函数
- 第 90 行 `onTogglePort` 主动 `await store.scanNow()` → 又触发一波完整 scan + emit 风暴

### 数据流放大链（关键！）

**核心放大链：**

```
后端 scanTick (15s 一次)
  → Reconcile 启停 forward
    → 每个端口 emit 一次 EventForwardUpdate    (N 个端口 ⇒ N 次)
  → 最后再 emit 一次 EventStateUpdate         (全量 Snapshot)

前端：
  → 每次 EventForwardUpdate 都触发 api.GetSnapshot() 走 Wails IPC   ★ 放大器
  → 后端 Snapshot() 持锁深拷贝 → 返回 JSON
  → 前端 forwards.value = toArray(s)  整个数组替换                  ★ 触发全量重渲染
  → 模板中 forwardsOf / isEnabled / connLabel 等非 computed 函数被反复执行
  → NDataTable / Collapse / NTag 等 Naive UI 组件全量重 diff
```

**为什么"只有 1 个 server"也卡？** 因为放大倍数取决于"远端 listening 端口数量"，不是 server 数量。一个跑着 docker / 各种服务的 Linux 主机轻松出 30~50+ listening 端口；启动时一次性 startForward 30+ 个 → 30+ 次 EventForwardUpdate → 30+ 次 GetSnapshot → 30+ 次全量数组替换 + 全量 Vue 重渲染。叠加 Naive UI 的 NDataTable 内部 diff 开销，主线程被同步打满 → 整机卡顿。

### 三个 Agent 公认的 Top-1 真凶

**`frontend/src/store/state.ts:91-93` 的 EVENT_FORWARD_UPDATE 回调：**

```ts
onEvent(EVENT_FORWARD_UPDATE, () => {
  api.GetSnapshot().then((s) => (forwards.value = toArray(s)))
})
```

这段是冗余的——`EventStateUpdate` 已经在 scanTick 末尾发了完整 snapshot，前端不需要再为每条 forward 反向 IPC 拉一次。

### 候选优化路径（按性价比排序）

| 优先级 | 位置 | 改法 | 收益 |
|--------|------|------|------|
| **P0** | `state.ts:91-93` | 删除 EVENT_FORWARD_UPDATE 回调中的 GetSnapshot 调用，**只依靠 EVENT_STATE_UPDATE 推送的全量** | 直接消除 N 倍 IPC + 重渲染 |
| **P0** | `MonitorView.vue:48` | `forwardsOf` 改为按 server_id 分组的 computed (一次性 group)；`isEnabled` 改为 Set 化或 computed | 模板侧消除 O(host×forward) 重复 filter |
| **P1** | `state.ts:98-103` | `serverStatus.value[id] = s` 直接赋值（ref 是 reactive 时 OK），或改 Map，避免 spread 重建 | 减少响应式风暴的级联范围 |
| **P1** | `runtime.go:54-55` | 加变化检测：snapshot 与上次 hash/版本号比较，无变化就不 emit | 减少稳态下的无意义全量推送 |
| **P2** | `runtime.go:175-187` | 移除每条 forward 的 EventForwardUpdate emit，或合并为单事件；`scanTick` 完成时统一 emit StateUpdate 即可 | 后端层面消除"per-port 事件" |
| **P2** | `engine.go:310-349` | Snapshot 可在内部缓存（dirty bit），无变化时不重新深拷贝 | 减少持锁时间 |

### 暂时存疑 / 需要进一步验证的

- 用户日常使用时，远端 listening 端口数量大概多少？这个决定了爆炸倍数。
- 是不是仅在初次启动 / 重启 server / 手动 ScanNow 时卡？还是稳态下也持续卡？
  - 如果只是初始爆发，主因就是 startForward 风暴 + 前端 N 次 GetSnapshot；
  - 如果稳态下也持续卡，可能 connectLoop 状态在反复抖动，需要看用户的 SSH 服务器是否健康。
- 是否有 console 错误（Naive UI 报错可能也很耗 CPU）？

---

## 第二轮探索：用户补充信息 + 亲自核对代码

### 用户补充

1. 远端 listening 端口"几十个"
2. **持续不动也一直卡，点端口开关时尤其卡**
3. 架构诉求：UI 无关的数据逻辑要剥离出来能单独验证

### 亲自核对的关键事实

#### 事实 1：`engine/mutate.go:12-17` 的 ToggleForward 是空壳！

```go
func (e *Engine) ToggleForward(serverID string, port int, on bool) error {
    _ = serverID
    _ = port
    _ = on
    return nil
}
// TODO(M7+): 接入 forced 集合后下次 diff 忽略该端口。
```

但前端 `frontend/src/views/MonitorView.vue:88-91` 还在调它：

```ts
async function onTogglePort(serverId: string, port: number, on: boolean) {
  await window.go?.main?.App?.ToggleForward?.(serverId, port, on)  // ← 空操作
  await store.scanNow()                                              // ← 等完整 scan
}
```

**用户感受到"点开关尤其卡"的真相**：开关其实没做任何事，只是触发了一次完整 scan + 一连串 emit 风暴 + 前端 N 次 GetSnapshot + N 次重渲染。用户以为是开关本身慢，其实是 scan 链路慢。

#### 事实 2：LastActive 稳态确实不变

`engine.go:175` `h.updated.Store(e.deps.Now().Unix())` 只在 startForward 一次性写入；`engine.go:345` 读取。稳态下不变。

#### 事实 3：稳态卡顿的真正原因

每 15s 一次的 EventStateUpdate（`runtime.go:54-55`）虽然内容不变，但前端 `state.ts:87` 用 `forwards.value = toArray(data)` 做**数组整体替换** → Vue 标记下游所有 computed / watcher dirty → NDataTable / Collapse / NTag 全量 diff。叠加模板里的 forwardsOf / isEnabled / connLabel 等非 computed 函数被反复执行 → 一次 15s tick 卡几百毫秒。如果还有 connectLoop 抖动加 ServerStatus 事件，那就持续卡。

#### 事实 4：scanNow 路径的双重放大

前端 `state.ts:51-55`：

```ts
async function scanNow(): Promise<void> {
  await api.ScanNow()                                            // 后端做完整 scanTick：N 次 EventForwardUpdate + 1 次 EventStateUpdate
  lastScanAt.value = Date.now()
  forwards.value = toArray(await api.GetSnapshot())              // 这里又自己拉一次
}
```

加上 `state.ts:90-93` 的 EventForwardUpdate 回调还在反向拉一次，**一次 toggle = N + 2 次全量替换 + N+1 次 IPC 反向拉**。

---

## 综合根因清单（精确）

| 编号 | 位置 | 现象 | 严重度 |
|---|---|---|---|
| R1 | `engine/mutate.go:12-17` | ToggleForward 是空壳，开关功能名实不副 | 🔴 功能 bug |
| R2 | `frontend/src/store/state.ts:86-89` | EventStateUpdate 不论内容是否变化都做数组整替 | 🔴 主因（稳态泵） |
| R3 | `frontend/src/store/state.ts:90-93` | EventForwardUpdate 又反向拉 GetSnapshot 全量 | 🔴 N 倍放大器 |
| R4 | `internal/engine/runtime.go:54-55` | 后端无条件 emit EventStateUpdate | 🟡 稳态白噪声 |
| R5 | `internal/engine/runtime.go:176-187` | per-forward 事件，每条 forward 启停各 emit 一次 | 🟡 配合 R3 放大 |
| R6 | `frontend/src/views/MonitorView.vue:30-56` | 模板里 isEnabled / connLabel / forwardsOf 非 computed，每次重渲染 O(host×forward) 重算 | 🟡 单次渲染慢 |
| R7 | `frontend/src/store/state.ts:51-55` | scanNow 函数末尾自己又拉一次 GetSnapshot，与事件 push 重复 | 🟡 toggle 路径放大 |
| R8 | `frontend/src/store/state.ts:98-103` | serverStatus 每次都 spread 重建整个 Record，连接抖动时连锁触发 | 🟢 次要 |

---

## 关于"架构剥离 + 可单独验证"的设计方案

### 设计目标

把"接收事件 + 维护状态 + 派生视图模型 + 决定调哪个 API"这些**纯逻辑**从 Vue / Pinia / Wails 里剥离出来，放到 `frontend/src/core/` 一个**不 import vue / 不 import wails runtime** 的模块里。

### 为什么这能解决问题（不只是好看）

1. **可以直接 vitest 验证事件风暴**：模拟一组事件流灌进去，断言"在 N 个 EventForwardUpdate 后 state 只变了 K 次"——把性能问题变成可断言的单测。
2. **applyEvent 是纯函数**，能做内容 diff 决定"要不要真的换 state 引用"——下游 Vue 才不会做无意义重渲染。
3. **selectors 是纯函数**，可以缓存（memoize），模板就不会反复 filter 全量数组。
4. **后端 engine 已经是这种风格**了（`Reconcile`、`Diff`、`conflict.Classify` 都是纯函数，有充分单测），前端补齐这块。

### 目录结构

```
frontend/src/
├── core/                              # ★ 新增，零 vue/wails 依赖
│   ├── types.ts                       # 从 ../types.ts 搬迁数据契约（或 re-export）
│   ├── state.ts                       # AppState 类型 + initialState()
│   ├── reducer.ts                     # applyEvent(state, event) 纯函数
│   │                                  #   - 关键：内容相等时返回原 state 引用（结构共享）
│   ├── selectors.ts                   # 派生：forwardsByServer / enabledHostSet / connLabel
│   │                                  #   - 用 weakly memo（基于上一次输入引用）
│   ├── intent.ts                      # 用户意图 → API 调用计划（接收 ApiDeps 注入）
│   └── __tests__/
│       ├── reducer.test.ts
│       ├── selectors.test.ts
│       └── intent.test.ts
│
├── api/
│   ├── wails.ts                       # 保留，作为 ApiDeps 一种实现
│   ├── mock.ts                        # 保留
│   └── deps.ts                        # ★ 抽 ApiDeps 接口（getConfig/getSnapshot/...）
│
├── store/
│   └── state.ts                       # ★ 重构：薄薄一层 Pinia adapter
│                                      #   - 持有 ref<AppState>
│                                      #   - 事件 → reducer.applyEvent → 仅当 ref 引用变才赋值
│                                      #   - 暴露 computed(selectors.xxx(state))
│
└── views/
    └── MonitorView.vue                # 不再写 isEnabled/connLabel/forwardsOf，直接用 store.forwardsByServer 等
```

### 关键接口签名（草图）

```ts
// core/state.ts
export interface AppState {
  hosts: Host[]
  enabledHosts: string[]
  forwards: Forward[]
  serverStatus: Record<string, ServerStatus>
  config: Config | null
  lastScanAt: number | null
  lastError: string
}

export function initialState(): AppState

// core/reducer.ts
export type Event =
  | { kind: 'state-update'; forwards: Forward[] }
  | { kind: 'forward-update'; serverId: string; port: number; status: string; error: string }
  | { kind: 'server-status'; status: ServerStatus }
  | { kind: 'scan-error'; error: string }
  | { kind: 'config-loaded'; config: Config }
  | { kind: 'hosts-loaded'; hosts: Host[]; enabled: string[] }
  | { kind: 'host-toggled'; alias: string; on: boolean; enabled: string[] }

// 纯函数；内容等价时必须返回 prev 自身（让 Vue 跳过重渲染）
export function applyEvent(prev: AppState, ev: Event): AppState

// core/selectors.ts —— 这些函数是 memoized，输入引用不变则结果引用不变
export function forwardsByServer(s: AppState): ReadonlyMap<string, Forward[]>
export function enabledHostSet(s: AppState): ReadonlySet<string>
export function connLabel(s: AppState, alias: string): string

// api/deps.ts
export interface ApiDeps {
  getConfig(): Promise<Config>
  getSnapshot(): Promise<Forward[]>
  listHosts(): Promise<Host[]>
  enabledHosts(): Promise<string[]>
  setHostEnabled(alias: string, on: boolean): Promise<void>
  scanNow(): Promise<void>
  toggleForward(serverId: string, port: number, on: boolean): Promise<void>
  // ...
}
```

### 单测可验证的事

```ts
// reducer.test.ts
it('forwards 内容相同时返回 prev', () => {
  const s1 = applyEvent(initialState(), { kind: 'state-update', forwards: [/*...*/] })
  const s2 = applyEvent(s1, { kind: 'state-update', forwards: [/*相同内容*/] })
  expect(s2).toBe(s1)   // ← 引用相等，证明 Vue 不会重渲染
})

it('connectLoop 抖动 100 次只产生 1 次有效变化', () => {
  let s = initialState()
  for (let i = 0; i < 100; i++) {
    s = applyEvent(s, { kind: 'server-status', status: {server_id:'a', state:'connected', ...} })
  }
  // 第一次后 status 已经稳定为 connected，后续 99 次应该返回相同引用
  // 这就是"事件风暴"被吸收的证明
})

// selectors.test.ts
it('forwardsByServer 同输入返回同引用', () => {
  const s = /* 构造 state */
  expect(forwardsByServer(s)).toBe(forwardsByServer(s))   // memo 生效
})
```

### 关于"要不要引入 zustand / immer"

不建议。当前规模手写 reducer + Object.freeze + 一个轻量 memo 工具足够，引入新依赖反而增加心智负担。后端 engine 也是这个判断（自己写纯函数 + 单测，没有引入 saga / redux 这种）。

---

## 候选实施路径（决策待我确认）

### 路径 A：先修 bug，不重构
- 改 R1（实现 ToggleForward）、R3（删 EventForwardUpdate 回调）、R7（去掉 scanNow 末尾的重复拉）
- 改动量小，一个 PR，立竿见影
- 但 R2/R6 还在，仍会有稳态轻微卡

### 路径 B：先重构 core/ 再修 bug（推荐）
- 先把 `core/reducer + selectors + tests` 建起来（约 300-400 行 + 单测），后端无改动
- 重构 `store/state.ts` 走 reducer
- 在 reducer 里加内容 diff（引用相等返回 prev），消灭 R2 的"无效重渲染"
- 把模板里 forwardsOf / connLabel 改用 selectors，消灭 R6
- 顺手修 R1 R3 R7
- 这个路径下，**事件风暴 / 内容不变不重渲染** 这些都能用 vitest 验证

### 路径 C：B + 后端 emit 去抖
- B 基础上后端也加 snapshot 内容指纹，无变化不 emit；per-forward 事件合并
- 范围更大，但稳态下 IPC 也清净

---

## 等待用户确认的点

1. 优先路径选 A / B / C？我倾向 **B**（性价比最高，且正好符合架构诉求）
2. ToggleForward 的实现：是要"强制启停某个端口直到下次 scan"还是"持久化用户意图（用户标了 off 的端口永远不转发）"？这关系到要不要持久化到 config.toml
3. 是否同意 vitest 引入到 frontend
4. 是否需要把后端 emit 去抖也纳入这次（建议先不做，B 实施后看效果）

---

## 决策（2026-05-22）

用户拍板：

1. ✅ **路径 B**：建 `core/` 纯逻辑层 + 单测 + 重构 store + 顺手修 R1 R3 R6 R7。后端 emit 去抖（路径 C）暂不做。
2. ✅ **ToggleForward 语义：持久化**。用户禁用过的端口永远不转发，写入 `config.toml`；重启应用后仍记得；下次 reconcile 时被禁用的端口直接跳过。
3. ✅ **引入 vitest** 到 frontend。

下一步：进入 Plan 模式产出详细实施方案。

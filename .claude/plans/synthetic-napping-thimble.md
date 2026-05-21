# 简化 APP：去掉服务器 CRUD，改用系统 ssh 命令 + ControlMaster

> 讨论日志：`docs/discuss-20260521-simplify-app.md`（4 轮对齐，含所有决策与 WHY）

## Context

当前 APP 把 SSH 服务器当成可增删改查的实体：独立"服务器" tab、11 字段的 `config.Server`、自己用 `golang.org/x/crypto/ssh` 处理认证（agent/私钥/密码）、自己在 Go 里做 listen+bridge 转发。这套实现重复了用户 `~/.ssh/config` 已有的信息，且不支持 ProxyJump/ProxyCommand/known_hosts。

用户要求彻底简化：**复用系统 ssh 命令作为唯一数据源和连接手段**，APP 不再管理服务器、不碰认证。落地方式是 OpenSSH 的 ControlMaster——一个 master 长连接 socket，扫描、动态增删转发全部复用它，认证只在建主连接时由 ssh 自己走一次。结果：删掉服务器 CRUD、删掉 `golang.org/x/crypto/ssh` 依赖、删掉 `local_port_offset` 和 `only_public_bind` 两个规则字段，监控页改成按服务器分组、每个服务器一个监控开关。

## 关键决策（已与用户对齐）

1. **连接层**：fork 系统 `ssh` + ControlMaster，**重写**连接/转发层（非保留 Go ssh 库）。
2. **列举 host**：用 shell 工具（grep/awk）读 `~/.ssh/config`，**不**在 Go 里 parse ssh config 语法。每个别名用 `ssh -G <alias>` 取 effective hostname/user/port。
3. **host key**：**不**附加任何 `-o StrictHostKeyChecking` 选项，沿用用户 ssh 默认行为。首连未知 host 失败时，界面提示"请先在终端手动 `ssh <alias>` 接受 host key"。
4. **启用状态**：存 APP config.toml 的别名集合，**默认禁用**（避免一启动扫爆所有 host）。
5. **扫描周期默认 15s**、**排除端口默认 `[22,53,80,443,111,631]`**，界面都要显式显示默认值。
6. **孤儿状态保留**：config 里某别名从 ssh config 消失后，其启用状态保留，同名再现时恢复。

## ControlMaster 命令契约

```bash
ssh -M -S <sock> -N <alias>                                    # 建主连接（前台，Go 持 *exec.Cmd）
ssh -S <sock> -O check <alias>                                 # 探活
ssh -S <sock> <alias> 'ss -H -tlnp 2>/dev/null'                # 扫描（复用现有 ParseSS）
ssh -S <sock> -O forward -L 127.0.0.1:N:127.0.0.1:N <alias>    # 加转发
ssh -S <sock> -O cancel  -L 127.0.0.1:N:127.0.0.1:N <alias>    # 删转发
ssh -S <sock> -O exit <alias>                                  # 关闭
```
- socket 路径：`<UserConfigDir>/auto-port-forward/ctl/<sanitized-alias>.sock`，注意 macOS unix socket 路径 ≤104 字符——别名 sanitize（非字母数字转 `_`）+ 必要时对长别名取 hash 后缀。
- master 进程**不用 `-f`**：前台启动，`cmd.Wait()` 返回即视为断开。
- 加保活选项：`-o ServerAliveInterval=15 -o ServerAliveCountMax=3 -o ConnectTimeout=10`（这些是连接健壮性参数，非认证策略，可加）。

---

## 后端改动

### 新模块 `internal/sshcfg/`（列举 + 解析 host）
- `Host{ Alias, HostName, User string; Port int }`
- `ListAliases(runner) ([]string, error)`：shell `grep -iE '^[[:space:]]*Host[[:space:]]' <config> | awk ... | grep -vE '[*?!]'`，过滤通配符。MVP 仅主 `~/.ssh/config`（含其直接 `Include` 文件，能力允许的话）。
- `Resolve(runner, alias) (Host, error)`：`ssh -G <alias>`，解析 `hostname`/`user`/`port` 三行。
- `ListHosts(runner) ([]Host, error)`：组合上面两步。
- 抽象 `Runner` 接口（`Run(ctx, name, args...) ([]byte, error)`），单测注入 fake，不真 fork。

### 新模块 `internal/sshctl/`（替换 `sshpool` 的连接实现）
- `Client`：持 `alias`、`sockPath`、`*exec.Cmd`、`doneCh`，实现 engine 的 `ClientHandle`。
  - `Connect(ctx)`：启动 master 进程 + 轮询 `-O check` 直到成功或超时。
  - `Run(ctx, cmd)`：`ssh -S sock alias cmd`，实现 `scan.Executor`（**`scan/` 包零改动**）。
  - `AddForward(ctx, port)` / `CancelForward(ctx, port)`：`-O forward`/`-O cancel`，按退出码+stderr 判定成败。
  - `Close()` / `Done()`：`-O exit` + 进程清理 / 断开通知。
- **复用** `internal/sshpool/reconnect.go` 的 `DefaultBackoff/NextDelay/IsDegraded`（纯逻辑，连同其测试保留）。建议把 `reconnect.go` 移入 `sshctl`，删除 `sshpool` 其余文件。
- 删除：`sshpool/client.go`、`sshpool/auth.go`（及 `auth_test.go`）、`internal/forward/`（整个包，listen+bridge 不再需要）。
- 移除 `go.mod` 中 `golang.org/x/crypto` 依赖（若无其他引用）。

### `internal/engine/` 改造
- `ClientHandle` 接口：去掉 `Dial`，新增 `AddForward(ctx, port) error` / `CancelForward(ctx, port) error`。
- `Deps.ClientFactory` 签名改为 `func(host sshcfg.Host) ClientHandle`；`serverState.cfg` 由 `config.Server` 换成 `sshcfg.Host`，`ServerID` 取 `Host.Alias`。
- `startForward/stopForward`（`runtime.go`）：不再起 forward goroutine + `forward.Forward`，改为调 `client.AddForward/CancelForward`，结果写回 `forwardHandle.status`（成功→forwarding，本地占用/失败→conflict/error）。
- 删 `LocalPortOffset`：`runtime.go:140,158`、`reconcile.go:41` 的 `localPort = remotePort + offset` 改为 `localPort = remotePort`。
- `ApplyServers` → 接收 `[]sshcfg.Host` + 启用集合（保留按 alias diff 启停的热插拔逻辑）。
- `connectLoop`/backoff/degraded/`scheduleLoop`/`Snapshot` 框架**保留**。

### `internal/conflict/` 清理
- 删 `isExcluded` 里的 `OnlyPublicBind && isLoopback(...)` 分支（`conflict.go:54-56`）及 `isLoopback`（若无其他引用）。删 `Input.Remote`/`OnlyPublicBind` 相关测试。
- `conflict_priv`（<1024 非 root）逻辑**保留**（ssh -O forward 仍在本地 listen，低端口需权限）。

### `internal/config/` 精简
- `Config`：`ScanIntervalSec int` + `Rules` + `EnabledHosts []string`（启用监控的别名）。删 `Servers`。
- `Rules`：仅 `ExcludePorts []int` + `ExcludeRanges []Span`。删 `OnlyPublicBind`、`LocalPortOffset`。
- 删 `Server` 结构体。`DefaultConfig`/`applyDefaults` 同步（默认 15s、默认 exclude_ports）。
- `store.go`：删 `AddServer/UpdateServer/DeleteServer/GetServer/Servers/GenerateID`；加 `EnabledHosts() []string` / `SetHostEnabled(alias string, on bool) error`。

### `app.go` Wails API 重构
- **删**：`ListServers/AddServer/UpdateServer/DeleteServer`、`syncEngineServers`。
- **加**：`ListHosts() []sshcfg.Host`、`SetHostEnabled(alias string, on bool) error`、`ReloadSSHConfig() error`（重读 ssh config + `engine.ApplyServers`）。
- `TestServer(id)` → `TestHost(alias)`：起一个临时 master 连接 + `-O check` 后 `-O exit`。
- `UpdateRules` 参数用精简后的 `config.Rules`。`setup()` 里 `ClientFactory` 改用 `sshctl.NewClient`，启动时 `ListHosts` + 按 `EnabledHosts` 启停。

### `cmd/verify_e2e/` 同步
- 移除 `LocalPortOffset`/`only_public_bind`/Server 构造，改用 ssh config 别名路径。

---

## 前端改动

### `types.ts`
- 删 `Server`；加 `Host{ alias, host_name, user, port }`。
- `Rules` 删 `only_public_bind`/`local_port_offset`。`Config`：`scan_interval_sec` + `rules` + `enabled_hosts: string[]`。

### 路由 / 删文件
- `router/index.ts` 删 `/servers` 路由。
- 删 `views/ServersView.vue`、`components/ServerForm.vue`。
- `i18n/zh.ts` 删 `servers.*`，加 `hosts.*`（连接状态、监控开关、试连接、host key 提示文案）。

### `MonitorView.vue` 重写（按服务器分组卡片）
```
┌────────────────────────────────────────────────────────────┐
│ 顶部：[立即扫描] [刷新 SSH 配置]        上次扫描：xx:xx:xx  │
├────────────────────────────────────────────────────────────┤
│ ▼ prod-db   git@10.1.2.3:22   [● 已连接]   监控[ ON ]  [试连]│
│   ┌──────────────────────────────────────────────────────┐ │
│   │ 远端端口 │ 进程 │ docker │ 状态 │ 本地端口 │ 备注     │ │  ← 复用 PortTable.vue
│   └──────────────────────────────────────────────────────┘ │
│ ▶ stage-db  u@x:22            [○ 未连接]   监控[ OFF ]      │
│   （监控未开启）                                            │
└────────────────────────────────────────────────────────────┘
```
- 每个 host 一张可折叠卡片：别名 + `user@host:port` + 连接状态 tag（来自 `serverStatus[alias]`）+ 监控开关（`SetHostEnabled`）+ 试连接按钮。
- 开关 ON 时展开端口表（`store.forwards` 按 `server_id===alias` 过滤），OFF 时显示"监控未开启"。
- 连接失败（含 host key 未信任）时，卡片内提示"若首次连接，请先在终端 `ssh <alias>` 接受 host key"。
- `PortTable.vue` 基本复用（去掉与 offset 无关，本地端口列保留）。

### `SettingsView.vue` 简化 + 显示默认值
- 删 `onlyPublic`/`offset` 两项。
- 扫描周期 `NInputNumber`，加 placeholder/灰字"默认 15 秒"。
- 排除端口 `NDynamicTags`：确保初始加载就渲染出 config 里的默认值（当前 `watch(store.config)` 已 map，需确认 store 初始化时机；必要时 `refresh()` 后即填充）。

### `store/state.ts` + `api/{wails,mock}.ts`
- 删 `servers`/`addServer`/`updateServer`/`deleteServer`，加 `hosts`/`listHosts`/`setHostEnabled`/`reloadSSHConfig`。
- `api` 接口删旧 CRUD，加新方法；`mock.ts` 提供假 host 列表（浏览器开发模式）。

---

## 任务拆分与协作（feat 分支 + worktree + BPR）

建主 feat 分支 `feat/ssh-config-source`，子任务并行后 BPR 合入：

1. **T1 数据契约层**（先行，阻塞其他）：`config` 精简 + `model`/`Rules` 字段删除 + `conflict` 清理。定义 `sshcfg.Host` 结构。
2. **T2 sshcfg 模块**（依赖 T1 的 Host 结构）：列举 + `ssh -G` 解析 + Runner 接口 + 单测。
3. **T3 sshctl 模块**（依赖 T1）：ControlMaster 连接/转发 + 复用 reconnect + 单测（fake runner）。
4. **T4 engine 接入**（依赖 T1/T2/T3）：ClientHandle 改接口、startForward 改 AddForward、删 forward 包、ApplyServers 改签名。
5. **T5 app.go + cmd**（依赖 T4）：Wails API 重构。
6. **T6 前端**（依赖 T5 的 API 契约）：路由/视图/store/types/i18n/mock。

T2、T3 可在 T1 合入后并行（各自 subagent/worktree）。每个子任务走 TDD：先写测试，再实现，`go test ./... -count=1` + 前端 `npm run build` 通过后 BPR，交叉 review。

## TDD 关键测试场景

- **sshcfg**：fake runner 喂入含 `Host *`/具体别名/`Host a b` 多别名的样本，验证过滤通配符、`ssh -G` 输出解析 hostname/user/port。
- **sshctl**：fake runner 验证 Connect 拼出 `-M -S -N`、AddForward 拼出 `-O forward -L 127.0.0.1:N:127.0.0.1:N`、CancelForward 拼 `-O cancel`、forward 失败（stderr/exit≠0）返回 error；Done 在进程退出后关闭。
- **conflict**：保留排除端口/范围/conflict_priv/conflict 测试；删 only_public_bind 测试。
- **engine**：`apply_test`/`supervise_test` 适配新 ClientHandle（fake 实现 AddForward/CancelForward）；验证扫描→reconcile→AddForward 调用、端口消失→CancelForward、localPort==remotePort。
- **config**：roundtrip 保存/加载只含新字段；`SetHostEnabled` 持久化；孤儿别名状态保留。
- 纯解析器（`ParseSS`/`ParseProcNetTCP`/`ScanLocal`）测试**原样保留**。

## 验证（end-to-end，macOS）

1. `go test ./... -count=1` 全绿；`cd frontend && npm run build` 通过。
2. `wails dev`：监控页列出 `~/.ssh/config` 里的具体别名（无通配符项），全部默认 OFF。
3. 打开某个真实可达 host 的监控开关 → 状态变"已连接" → 端口表出现远端 LISTEN 端口 → 非冲突端口变绿 `forwarding`。
4. `lsof -nP -iTCP -sTCP:LISTEN | grep ssh` 能看到对应本地监听；`curl http://localhost:<port>/` 透过隧道访问远端。
5. 在远端新起一个服务（如 `python3 -m http.server 9999`）→ 下个扫描周期自动出现并转发（验证 `-O forward` 动态增）。
6. 关掉监控开关 → master 退出、转发消失。
7. 设置页：扫描周期显示默认 15s、排除端口显示默认 tags；改值保存后生效。
8. host key 未信任的 host：打开开关 → 状态报错 + 提示先在终端 ssh 一次。
9. 编辑 `~/.ssh/config` 增/删别名 → 点"刷新 SSH 配置" → 列表更新，已删别名的启用状态在 config 保留。

## 不做的边界

- 不支持运行时动态改扫描周期（沿用现状，下次重启生效）。
- 不做 fsnotify 监听 ssh config（用"刷新"按钮）。
- 不写回用户 ssh config。
- 不自动信任未知 host key。
- ProxyJump/ProxyCommand 由 ssh 命令自然处理（无需 APP 额外代码）。

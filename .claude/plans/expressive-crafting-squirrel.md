# autoportforward —— 最终实施方案

## Context（为什么做）

你需要一个**运行在 macOS 上的 Go GUI 应用**，把一台或几台 SSH 服务器（典型如 `ubt`）正在监听的 TCP 端口**定时扫描 → 自动同号 local port forward 到本地**，并在界面上显示可转发/被占用/特殊端口的状态。痛点：手动维护一堆 `ssh -L` 命令太累，且远端端口会变（新跑了 docker 服务等），手动同步成本高。期望产物：一个能稳定运行、断线自动重连、零干预的「自动端口同步」桌面应用。

参考：[loris-tunnel-app](https://github.com/RangerWolf/loris-tunnel-app)（Wails v2 + Go + Vue 3，SSH 隧道管理）、[ssh-tunnel-manager](https://github.com/mdminhazulhaque/ssh-tunnel-manager)（思路参考），以及本机 `sonar` CLI（端口扫描）。

## 已确认的关键决策

| 项 | 决策 |
|---|---|
| 开发环境 | Linux miniubt 开发 + 跑测试 + 跑前端 Vite；GUI 在 Mac 上 release/验收 |
| GUI 框架 | **Wails v2.11+ + Vue 3 + Naive UI**（与 loris 同框架，文档稳） |
| 本地端口扫描 | 复用 `sonar list --json`（用户已装） |
| 远端端口扫描 | **不依赖远端装 sonar** → Go SSH 直连，跑 `ss -H -tlnp` 或读 `/proc/net/tcp{,6}`，复用同一条 SSH 连接做转发 |
| 转发策略 | 默认远端 N → 本地 N（同号）；默认全转发；排除 `22/53/80/443/111/631` 等；本地占用/特权端口标冲突 |
| 后端语言 | Go 1.25，每文件 ≤300 行，模块按业务垂直切分 |
| TDD | 解析器/冲突规则/diff/退避/配置/bridge 必须先写测试 |

## 整体架构

```
autoportforward/
├── go.mod                    # module autoportforward (go 1.25)
├── main.go                   # wails 入口, App 装配, 嵌入 frontend/dist
├── wails.json                # wails 项目配置
├── app.go                    # *App: Wails 绑定门面 (CRUD/扫描/启停/快照)
├── internal/
│   ├── model/model.go        # 纯数据模型, 被所有层引用
│   ├── config/               # TOML 读写, 原子写, 默认值
│   │   ├── config.go
│   │   └── config_test.go
│   ├── scan/
│   │   ├── remote.go         # 远端扫描入口, 探测链 ss→proc→netstat
│   │   ├── remote_ss.go      # ParseSS([]byte) []RemotePort  纯函数
│   │   ├── remote_proc.go    # ParseProcNetTCP([]byte, v6 bool) 纯函数
│   │   ├── local.go          # sonar list --json → []LocalPort
│   │   └── *_test.go
│   ├── sshpool/              # SSH 连接生命周期 (借鉴 loris)
│   │   ├── client.go         # Dial / auth / hostkey / keepalive
│   │   ├── reconnect.go      # 500ms→60s 指数退避, 纯函数可单测
│   │   └── *_test.go
│   ├── forward/              # 端口转发
│   │   ├── forward.go        # listen → accept → cli.Dial → bridge
│   │   ├── bridge.go         # 双向 io.Copy + ctx cancel
│   │   └── *_test.go
│   ├── conflict/
│   │   ├── conflict.go       # Classify() 纯函数, 优先级排序
│   │   └── conflict_test.go
│   ├── engine/               # 编排
│   │   ├── engine.go         # 启停/扫描定时器/状态快照/事件回调
│   │   ├── diff.go           # 远端端口集合 diff → 增删 forward
│   │   └── *_test.go
│   └── events/events.go      # Emitter 接口, 事件名常量, wails runtime 适配
└── frontend/                 # Vue3 + Naive UI + Vite + TypeScript
    ├── package.json  vite.config.ts  index.html
    └── src/
        ├── main.ts  App.vue
        ├── views/        MonitorView.vue ServersView.vue SettingsView.vue
        ├── components/   PortTable.vue ServerForm.vue StatusTag.vue
        ├── store/        state.ts (pinia, 监听 wails events)
        └── i18n/         zh.ts
```

依赖方向（无环）：`model ← config/scan/conflict ← engine`，`sshpool ← forward ← engine`，`app.go → engine + config`，`main.go → app.go`。`events.Emitter` 用接口隔离 wails runtime，方便 engine 单测。

## 核心数据模型（`internal/model/model.go`）

```go
type PortStatus string
const (
    StatusForwarding   PortStatus = "forwarding"     // 绿
    StatusPending      PortStatus = "pending"        // 灰
    StatusExcluded     PortStatus = "excluded"       // 灰: 命中黑名单/特权
    StatusConflict     PortStatus = "conflict"       // 红: 本地被占用
    StatusConflictPriv PortStatus = "conflict_priv"  // 红: <1024 非 root
    StatusError        PortStatus = "error"          // 红: SSH/forward 错误
)
type RemotePort struct {
    Port int; BindAddr, IPVersion string
    PID int; Process, Command, DockerImage string
}
type LocalPort struct { Port int; Process, Type string; PID int }
type Forward struct {
    ServerID string; RemotePort, LocalPort int
    Status PortStatus; Error string; LastActive int64
    Remote RemotePort
}
```

配置（TOML，`github.com/BurntSushi/toml`，原子写=临时文件+`os.Rename`），路径 `os.UserConfigDir()/autoportforward/config.toml`：

```go
type Config struct {
    ScanIntervalSec int          // 默认 15
    Servers []Server
    Rules Rules
}
type Server struct {
    ID, Name, Host string; Port int          // 默认 22
    User, AuthMethod string                  // password|ssh_key|ssh_agent
    Password, KeyPath, Passphrase string
    HostKey string                           // known_hosts|insecure
    Enabled bool
}
type Rules struct {
    ExcludePorts []int                       // 默认 [22,53,80,443,111,631]
    ExcludeRanges []Span                     // [{Lo,Hi}]
    OnlyPublicBind bool                      // true: 只转发 0.0.0.0/:: 绑定的端口
    LocalPortOffset int                      // 默认 0 (同号)
}
```

## 远端扫描方案（不依赖远端 sonar）

探测链（首个成功即用，结果按 server 缓存方法）：
1. `ss -H -tlnp 2>/dev/null`
2. `ss -H -tln 2>/dev/null`（权限不足时无 -p）
3. `cat /proc/net/tcp /proc/net/tcp6 2>/dev/null`
4. `netstat -anv -p tcp 2>/dev/null`（macOS server fallback，M2 之后补）

`ParseSS` 算法：`strings.Fields` 切列 → 字段 0 = `LISTEN` → 字段 4 = Local；右起最后一个 `:` 分割 bindAddr/port，处理 `[::]:22`、`*:3000`、`[::ffff:127.0.0.1]:8080`；进程信息正则 `\(\("([^"]+)",pid=(\d+)`。

`ParseProcNetTCP` 算法：跳表头 → 字段 1 = local_address（`IP:port` 十六进制）→ 字段 3 = st，`!= "0A"` 跳过 → port 大端 16 进制；IPv4 IP 每 2 hex 一字节**逆序**（`0100007F`→`127.0.0.1`）；IPv6 按 4 字节组小端反转。进程名 `/proc/net/tcp` 拿不到（只有 inode），接受降级（端口转发不依赖进程名）。

## 端口转发

- 用 `golang.org/x/crypto/ssh.Client`，**不 fork ssh 子进程**。
- **一个 server 一个 ssh.Client，多条 forward 复用同一连接**（`client.Dial` 开新 channel）。
- 核心代码（移植自 loris `internal/forward/port_forward.go` 的 `bridge`）：

```go
func (f *Forward) Run(ctx context.Context, cli *ssh.Client) error {
    ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", f.LocalPort))
    if err != nil { return err }                           // 上层标 conflict
    go func(){ <-ctx.Done(); ln.Close() }()
    for {
        c, err := ln.Accept()
        if err != nil { return ctx.Err() }
        go f.handle(ctx, cli, c)
    }
}
func (f *Forward) handle(ctx context.Context, cli *ssh.Client, local net.Conn) {
    rc, err := cli.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", f.RemotePort))
    if err != nil { local.Close(); f.report(StatusError, err); return }
    bridge(ctx, local, rc)
}
```

`cli.Dial("127.0.0.1", port)` 是关键：能把仅本地回环监听的远端服务暴露到 Mac。

## 冲突识别（`internal/conflict/conflict.go` 纯函数，优先级从高到低）

1. 命中 `ExcludePorts/Ranges` 或 `OnlyPublicBind && bind ∈ 127.x/::1` → `excluded`
2. `localPort < 1024 && !isRoot` → `conflict_priv`
3. 本地同号端口被占用 && 占用者不是本程序自己的 forward → `conflict`
4. SSH client 不可用 → `error`
5. 否则 → `pending`，Listen 成功后转 `forwarding`

## Wails Go ↔ Vue 接口（挂在 `*App`）

```go
func (a *App) ListServers() []config.Server
func (a *App) AddServer(s config.Server) (config.Server, error)
func (a *App) UpdateServer(s config.Server) error
func (a *App) DeleteServer(id string) error
func (a *App) TestServer(id string) error
func (a *App) GetConfig() config.Config
func (a *App) UpdateRules(r config.Rules) error
func (a *App) StartAll() error
func (a *App) StopAll() error
func (a *App) ScanNow() error
func (a *App) ToggleForward(serverID string, port int, on bool) error
func (a *App) GetSnapshot() []model.Forward
```

**状态推送：用 Wails events，不轮询。** engine 每次状态变化 `runtime.EventsEmit(ctx, "state:update", snapshot)`，前端 `EventsOn` 写 pinia。事件名集中在 `internal/events/events.go`：`state:update / server:status / scan:error / forward:update`。

## 前端组件

- 布局：`n-layout` 左 `n-menu`（端口监控 / 服务器 / 设置），右路由视图。
- **MonitorView.vue**：`n-data-table` 列 `端口 | 远端进程 | Docker | 状态 | 本地端口 | 操作 | 备注`；状态用 `n-tag` 绿/灰/红 三色；顶部 `n-space`：服务器下拉、`StartAll/StopAll`、`ScanNow`、最后扫描时间。
- **ServersView.vue**：`n-data-table` + `n-form`；认证方式 `n-radio-group` 切换 password/key/agent 字段。
- **SettingsView.vue**：扫描周期 `n-input-number`、排除端口 `n-dynamic-tags`、`OnlyPublicBind` `n-switch`、`LocalPortOffset` `n-input-number`。
- 多语言：中文为主，预留 i18n key 结构。

## TDD 计划（遵循 test-driven-development，先红→绿→重构）

| 模块 | 测试文件 | 关键用例 |
|---|---|---|
| ss 解析 | `scan/remote_ss_test.go` | (1)`0.0.0.0:9527` 有进程 (2)回环 `127.0.0.1:41548` (3)`0.0.0.0:9308` 无 users (4)`[::]:22` IPv6+sshd (5)`*:3000` 双栈 (6)`[::ffff:127.0.0.1]:8080` 映射回环 (7)空输入 (8)表头误传 |
| proc 解析 | `scan/remote_proc_test.go` | (1)`0100007F:A24C st=0A`→127.0.0.1:41548 (2)`00000000:0050 st=0A`→0.0.0.0:80 (3)`st=01` 跳过 (4)tcp6 `::` 映射 (5)tcp6 真实 IPv6 (6)畸形行跳过 |
| 冲突规则 | `conflict/conflict_test.go` | 排除命中 / 范围命中 / 本地占用且非自己→conflict / 占用是自己→forwarding / `<1024` 非 root→conflict_priv / OnlyPublicBind 过滤回环 / 正常→pending |
| diff | `engine/diff_test.go` | 新增→add / 消失→del / 不变→noop / excluded 不进 desired |
| 退避 | `sshpool/reconnect_test.go` | n=0→500ms / 翻倍 / 封顶 60s / 15min 后 degraded |
| 配置 | `config/config_test.go` | 默认生成 / 往返序列化 / 原子写 / 缺字段补默认 / 坏 TOML 报错 |
| bridge | `forward/bridge_test.go` | `net.Pipe` 双向拷贝 / 一端关闭对端关闭 / ctx cancel 退出 |
| 集成 | `forward/integration_test.go` | `//go:build integration`，连 127.0.0.1:22 真转发往返字节 |

## 跨平台与构建

**Linux miniubt 开发**（GUI 不可见）：
- `go test ./...` 全量单测 — 不需要 GTK。
- 集成测试：`go test -tags=integration ./internal/forward/`（用本机 sshd + 用户私钥）。
- 前端独立开发：`cd frontend && npm i && npm run dev` → 浏览器 `http://localhost:5173`；前端对 `window.go` 存在性判断 + mock fallback。
- `wails dev` 窗口**不在 Linux 跑**（无 GTK/DISPLAY），等 Mac 验收。
- 可选：装 GTK 才能跑 `wails build`，命令（需你授权执行）：`sudo apt install -y build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev`。

**macOS release**（你在 Mac 上跑）：
```
brew install go node
go install github.com/wailsapp/wails/v2/cmd/wails@latest
cd autoportforward && wails build -platform darwin/universal -clean
# 产物: build/bin/autoportforward.app
```

## 分阶段里程碑（BPR 工作流）

- **M1 骨架**：`go mod init`、目录、`main.go`/`app.go`/各包 `*.go` 带 `// TODO` 与签名；最小 `wails.json`+`frontend` 脚手架；`go build ./...` 通过
- **M2 远端扫描**：`remote_ss.go`/`remote_proc.go` + 单测全绿
- **M3 本地扫描+冲突**：`scan/local.go`(sonar JSON) + `conflict/` + 单测
- **M4 SSH+转发**：`sshpool/` + `forward/` + bridge 单测 + integration
- **M5 diff 引擎**：`engine/diff.go` + `engine.go` + 单测
- **M6 Wails 绑定 + UI 骨架**：`app.go` 全方法 + Vue 三视图空壳 + events 接线
- **M7 UI 完整**：PortTable/ServerForm/Settings + pinia 事件实时更新
- **M8 健壮性**：重连退避、keep-alive、错误恢复、degraded 上报
- **M9 打包**：mac universal 构建 + README + 用户验收

每个里程碑结束 `go test ./...` 必须全绿，才能进入下一阶段。

## 关键文件路径速查

- `/home/ubuntu/dev/autoportforward/internal/scan/remote_ss.go` — ss 解析（最关键）
- `/home/ubuntu/dev/autoportforward/internal/scan/remote_proc.go` — proc 解析
- `/home/ubuntu/dev/autoportforward/internal/conflict/conflict.go` — 冲突规则
- `/home/ubuntu/dev/autoportforward/internal/engine/engine.go` — 编排
- `/home/ubuntu/dev/autoportforward/internal/forward/forward.go` — 转发核心
- 参考移植源：loris `internal/forward/port_forward.go` 的 `bridge`/`makeAuthMethod`/`reconnectWithBackoff`/`monitorClientLifecycle`

## 风险与未知项

| 项 | 决策/缓解 |
|---|---|
| Wails v2 vs v3 | **用 v2.11+**（loris 同版本、社区主流、v3 仍 alpha） |
| 远端非 Linux | 加 `remote_netstat.go` macOS fallback，M2 后补 |
| 远端无 root | Process 字段留空、端口扫描照样完成 |
| 本地特权端口转发 | 默认 listen 127.0.0.1:port，<1024 失败→`conflict_priv`；提供 `LocalPortOffset`（如 +20000）把 80→20080 |
| 本地端口已占用 | 标 `conflict`，不抢占；用户可调 offset 或加排除 |
| GTK 缺失 | Linux 只验逻辑+前端，GUI Mac 验收 |
| 密码明文 TOML | M1 同 loris；M9+ 可选接 macOS Keychain |
| ss 各发行版差异 | 按字段语义解析、不硬编码列数，多样本测试覆盖 |

## 验证（如何端到端验收）

**Linux miniubt 上**：
1. `go test ./... -count=1` — 全部单测绿
2. `go test -tags=integration ./internal/forward/ -count=1` — 真实 SSH 转发测试绿（127.0.0.1）
3. `cd frontend && npm run dev` 后浏览器访问 `http://localhost:5173`，UI 三页都能打开，表格能渲染 mock 数据

**Mac 上**（你最终验收）：
1. `wails build -platform darwin/universal` 成功，产出 `.app`
2. 打开 App，添加 `ubt` server（ssh-agent 认证），点扫描
3. 表格出现远端端口列表；非冲突端口在 30 秒内变成绿色 `forwarding`
4. `lsof -nP -iTCP -sTCP:LISTEN | grep autoportforward` 能看到对应本地监听
5. `curl http://localhost:<被转发的端口>/` 能透过隧道访问远端服务
6. 手工 `kill` 一个本地占用端口、再扫描，对应行从 `conflict` 变 `forwarding`
7. 断开网络几秒再恢复，UI 显示 `degraded`→恢复，无需人为干预

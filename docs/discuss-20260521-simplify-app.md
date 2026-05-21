# 讨论：简化 APP — 去掉服务器 CRUD、整合监控视图、清理规则字段

- 日期：2026-05-21
- 触发：用户反馈对 APP 形态的简化诉求
- 状态：进行中，等待对齐

---

## 用户提出的需求（原文摘要）

1. **去掉"服务器" tab**：复用用户的 `~/.ssh/config` 作为数据源（用命令行列出已配置的服务器）。APP 不再做服务器增删改查，不处理认证方式。
2. **端口监控页面直接显示所有服务器**：每个服务器一个监控开关（开启/关闭监控），把该服务器的端口列出来。
3. **全局设置**：
   - 扫描周期要有默认值（如 10s 或 30s）
   - 排除端口要有默认值
4. **删除字段**：
   - 去掉"本地端口偏移" (`local_port_offset`)
   - 去掉"仅转发 0.0.0.0 绑定端口" (`only_public_bind`)

---

## 探索结论汇总

### A. 现有"服务器管理"全貌（Explore Agent 1）

- 数据模型：`internal/config/config.go` 的 `Server` 结构体，11 个字段（id/name/host/port/user/auth_method/password/key_path/passphrase/host_key/enabled）
- 存储：`internal/config/store.go` 实现 CRUD + 原子写到 `~/.config/auto-port-forward/config.toml`
- Wails 绑定：`app.go` 暴露 `ListServers / AddServer / UpdateServer / DeleteServer / TestServer`
- 前端：`ServersView.vue` + `ServerForm.vue` + `i18n/zh.ts:servers.*`
- 路由：`/servers` 一个独立 tab；`/monitor` 监控；`/settings` 全局
- 热插拔：`engine.ApplyServers()` 按 ID diff 启停 connectLoop，已经能处理"列表变化"
- `Enabled=false` 的语义：不启 connectLoop，扫描/转发跳过

### B. 端口监控 / 扫描规则现状（Explore Agent 2）

- `scan_interval_sec` 默认 **15s**，下次重启生效（M8+ TODO 动态更新）
- `Rules` 字段：
  - `ExcludePorts` 默认 `[22,53,80,443,111,631]`
  - `ExcludeRanges` 默认 `nil`（前端无编辑 UI，原样回写）
  - `OnlyPublicBind` 默认 `false` —— 在 `internal/conflict/conflict.go:54` 用于把 loopback 绑定的端口标 `excluded`
  - `LocalPortOffset` 默认 `0` —— 在 `internal/engine/runtime.go:140,158` 和 `reconcile.go:41` 计算 `localPort = remotePort + offset`
- MonitorView：顶部有 server 选择器 + 扫描按钮 + 启动/停止 + 上次扫描时间；表格按 selectedServer 过滤；不分组，平铺
- PortTable 列：远端端口 / 进程 / docker / 状态 / 本地端口 / 备注 / 操作
- SettingsView：四项设置（扫描周期 / 排除端口 / 仅公开绑定 / 本地端口偏移）

### C. SSH config 复用可行性（Explore Agent 3）

- 现有 sshpool 是基于 `golang.org/x/crypto/ssh` 直连，**不支持** `ProxyCommand` / `ProxyJump` / `IdentityAgent` / `known_hosts` 严格校验（M8 TODO）
- 列出 Host 候选方案：
  - **方案 A（推荐）**：直接 `ssh -G <host>` 取 effective 配置 —— 完全兼容 OpenSSH 语义，无第三方库
  - 方案 B：`kevinburke/ssh_config`（社区首选，纯 Go，不支持 `Match`）
  - 方案 C：`k0sproject/rig/v2/sshconfig`（支持 Match/Include，生态弱）
- 列出"已配置的 Host"：
  - 解析 `~/.ssh/config`（或 `Include` 的所有文件）拿到所有 `Host` 块
  - 排除通配符（`*` / `?` / `!`），只保留具体别名
  - 对每个具体别名调 `ssh -G <alias>` 拿 effective 参数

---

## 我的方案设计（待对齐）

### 1. 数据源模型重构

**去掉**：`config.Server` 的大部分字段。改为：

```go
// internal/config/config.go
type Config struct {
    ScanIntervalSec int                 // 默认 30
    Rules           Rules
    // 不再持久化整个 Server 列表，只持久化"是否启用监控"的状态
    ServerStates    map[string]ServerState  // key = ssh config 中的 Host 别名
}

type ServerState struct {
    Enabled bool
}

type Rules struct {
    ExcludePorts  []int
    ExcludeRanges []Span
    // 删除 OnlyPublicBind 和 LocalPortOffset
}
```

**新增**：`internal/sshcfg/` 模块

```go
// internal/sshcfg/sshcfg.go
type Host struct {
    Alias    string   // SSH config 中的 Host 别名（如 "prod-db"）
    HostName string   // 解析后的 hostname
    Port     int      // 默认 22
    User     string   // 解析后的 user
    // 不再保存认证细节 —— 实际连接时由 ssh -G 现取或由 Go ssh 库现读
}

func ListHosts() ([]Host, error)         // 解析 ~/.ssh/config，过滤通配符
func Resolve(alias string) (Host, error) // ssh -G alias 取 effective
```

### 2. 服务器连接策略（关键决策点）

**两个选项，需要对齐：**

| 选项 | 实现 | 优势 | 劣势 |
|------|------|------|------|
| **A. 现有 Go ssh 库 + ssh -G 取参数** | 保留 `sshpool`，只把 host/port/user/identity_file 从 `ssh -G` 提取后塞进去 | 改动小，复用现有断线重连/保活/forward 桥接 | 不支持 ProxyJump / ProxyCommand / ControlMaster；用户复杂配置会连不上 |
| **B. 直接 fork `ssh -N -L`** | 完全用系统 ssh 命令做转发，APP 只管启停子进程 | 100% 兼容 OpenSSH（ProxyJump、ControlMaster、known_hosts 全都自动) | 几乎要重写 sshpool/forward；进程管理复杂；状态可见性差 |

**我的建议**：**先做 A**。原因：
- 改动可控，沿用现有 TDD 单测
- 大多数用户的 SSH config 没用到 ProxyJump
- 不支持的高级配置可以标 `error` 状态 + 显示"该 Host 配置过于复杂，不支持自动转发"
- 如果用户反馈强烈，再迁移到 B（属于另一期工作）

### 3. UI 重构

**路由**：`/servers` 整个删除。剩 `/monitor` + `/settings`。

**MonitorView 新布局**（ASCII 草图）：

```
┌──────────────────────────────────────────────────────────────┐
│  顶部工具栏：[立即扫描]  [刷新 SSH 配置]  上次扫描：xx:xx  │
├──────────────────────────────────────────────────────────────┤
│ ▶ prod-db   user@10.1.2.3:22    [● 已连接]  [开关 ON ]      │
│   ┌────────────────────────────────────────────────────────┐ │
│   │ 远端端口 │ 进程   │ docker │ 状态     │ 本地 │ 备注    │ │
│   │ 5432     │ postgres│        │ 转发中   │ 5432 │         │ │
│   │ 6379     │ redis   │        │ 转发中   │ 6379 │         │ │
│   └────────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────────┤
│ ▶ ubt       ubuntu@192.168.1.5  [● 已连接]  [开关 ON ]      │
│   ... 端口表 ...                                              │
├──────────────────────────────────────────────────────────────┤
│ ▶ stage-db  user@x.x.x.x        [○ 未连接]  [开关 OFF]      │
│   （监控未开启）                                              │
└──────────────────────────────────────────────────────────────┘
```

每个服务器一个折叠卡片（可展开/收起），开关控制是否纳入扫描。

**SettingsView 简化**：
- 扫描周期（默认 30s，min=5, max=3600）
- 排除端口（NDynamicTags，默认 `[22,53,80,443,111,631]`）
- **删除**：仅公开绑定、本地端口偏移

### 4. 后端 API 重构

| 旧 API | 新 API | 说明 |
|--------|--------|------|
| `ListServers()` | `ListHosts() []Host` | 从 SSH config 列 Host |
| `AddServer / UpdateServer / DeleteServer` | **删除** | 不再 CRUD |
| `TestServer(id)` | `TestHost(alias)` 或保留 | 试连接 |
| - | `SetHostEnabled(alias, on bool)` | 设置某 Host 的监控开关 |
| - | `ReloadSSHConfig()` | 重新读 SSH config 文件 |
| `UpdateRules / UpdateScanInterval / GetConfig` | 保留，但 Rules 字段精简 | - |

### 5. 删除字段的具体波及面

详见 Explore Agent 2 报告第 10 节。汇总：

- `only_public_bind`：6 个代码点 + 4 个测试点（含前端）
- `local_port_offset`：8 个代码点 + 5 个测试点（含前端 + E2E）

### 6. 隐含的"不做的边界"

- 不实现 ProxyJump / ProxyCommand 支持（先标 error）
- 不实现 fsnotify 监听 SSH config 变化（用户点"刷新"或重启 APP）
- 不实现 SSH config 写回（APP 不修改用户的 ssh config）
- 不实现历史端口持久化（开关 OFF 关闭即清空，再扫即恢复）

---

## 待对齐的问题（核心）

### Q1. SSH 连接策略
选项 A（Go ssh 库 + `ssh -G` 取参数）还是选项 B（fork `ssh -N -L`）？
我推荐 **A**。

### Q2. 服务器列表过滤
`~/.ssh/config` 里通常有 `Host *` 之类的通配符。是否同意：
- 排除含 `*` `?` `!` 的 Host
- 只保留具体别名
- 还要不要其他过滤（比如排除某些以 `local-` 开头的别名）？

### Q3. 服务器的"启用"状态保存在哪里
我建议保存到 APP config.toml 的 `[server_states]` 表里（按 Host 别名为 key）。**默认未启用**（避免 SSH config 里几十个 Host 全部扫爆）。

或者：**默认全部启用**？

### Q4. 扫描周期默认值
10s / 15s / 30s 选哪个？我建议 **30s**（对远端 SSH 压力小，且生产够用）。

### Q5. SSH config 变化的感知
- 选项 A：APP 启动时读 + 提供"刷新 SSH 配置"按钮（推荐）
- 选项 B：每次 ScanNow 之前重新读
- 选项 C：fsnotify 监听

我推荐 **A**。

### Q6. 排除端口默认值
当前 `[22, 53, 80, 443, 111, 631]`，是否保持？是否需要追加（如 `25, 587, 5353`）？

### Q7. 当 SSH config 中的 Host 被删除/重命名
原本启用监控的 alias 不见了，怎么办？
- 选项 A：在 `server_states` 表里保留状态，下次 alias 再出现时恢复
- 选项 B：每次刷新都清掉孤儿状态

我推荐 **A**（保留），用户重命名只是临时事件不应丢配置。

### Q8. 前端 `ServerForm.vue` / `ServersView.vue` 整个删除
确认这两个文件可以直接删除？路由 `/servers` 也整个去掉？

### Q9. 是否保留 TestServer / TestHost 功能
"试连接"按钮还要不要？我倾向 **保留**（放在每个服务器卡片的右上角菜单里）。

---

## 第二轮（2026-05-21）：用户反馈 + 关键架构矛盾浮现

### 用户反馈

1. **不能直接读 `~/.ssh/config` 文件**，要通过"命令行程序"获取可用的服务信息。
2. **尽量用 shell/bash 实现功能，比如复用 ssh 命令**。
3. 扫描周期默认值 **15s 可以**，但界面上要**显示默认值**。
4. 排除端口默认值界面上**也要显示**（目前没显示）。
5. 其余基本没问题。

### 实测验证（本机 OpenSSH_10.2p1）

- `ssh -G <host>` 工作正常，输出 effective 配置：
  ```
  user git
  hostname github.com
  port 22
  identityfile ~/.ssh/id_rsa ...
  proxycommand nc -v -x 127.0.0.1:7890 %h %p
  ```
  → 能拿到 hostname/port/user/identityfile，**也能看到 proxycommand/proxyjump**。
- 但 `ssh -G` **需要给定具体 host 名**，无法用它"枚举"所有 Host。

### 关键矛盾：方案 A 其实不符合"复用 ssh 命令、不处理认证"

第三个 Explore agent 评估了两条路线：

| 维度 | 方案 A（保留 Go ssh 库，参数来自 `ssh -G`） | 方案 B（fork 系统 ssh 命令） |
|------|------|------|
| 改动文件 | 1-2 个 | 15-20 个 |
| 代码量 | +50~100 行 | 重写 ~1250-1500 行 |
| 工时 | 1-2h | 35-60h |
| **认证** | **仍要在 Go 里处理**（ssh-agent/私钥/密码/passphrase） | **完全交给 ssh 命令，APP 不碰** |
| ProxyJump/ProxyCommand | **不支持** | **原生支持** |
| known_hosts 校验 | 未实现（M8 TODO） | ssh 自己做 |

**结论**：用户说的"复用 ssh 命令、不用处理认证方式" = **方案 B**，不是我上一轮推荐的 A。
我上一轮推荐 A 是错的——A 做不到"不处理认证"，也不支持 ProxyJump。

### 方案 B 的优雅实现：ControlMaster

OpenSSH 的 ControlMaster 机制完美契合本项目"动态增删转发 + 复用连接扫描 + ssh 处理一切认证"的需求：

```bash
# 1. 启动 master 长连接（认证、ProxyJump、known_hosts 全部 ssh 自己处理）
ssh -M -S <ctl_socket> -N -f -o ServerAliveInterval=15 <alias>

# 2. 远端扫描（复用 master 连接，毫秒级，无需重新认证）
ssh -S <ctl_socket> <alias> 'ss -H -tlnp 2>/dev/null'

# 3. 动态新增一条转发
ssh -S <ctl_socket> -O forward -L 127.0.0.1:5432:127.0.0.1:5432 <alias>

# 4. 动态取消一条转发
ssh -S <ctl_socket> -O cancel -L 127.0.0.1:5432:127.0.0.1:5432 <alias>

# 5. 检查连接存活
ssh -S <ctl_socket> -O check <alias>

# 6. 优雅关闭
ssh -S <ctl_socket> -O exit <alias>
```

这套方案的好处：
- **彻底不碰认证**：ssh-agent / 私钥 / 密码 / ProxyJump / known_hosts 全是 ssh 命令的事
- **代码概念更简单**：废弃 `golang.org/x/crypto/ssh`、`sshpool/auth.go`、`forward/bridge.go`，换成 shell 命令封装 + 输出解析
- **保留可复用的纯逻辑**：`scan/` 的 `ParseSS`/`ParseProcNetTCP` 解析器、`conflict/` 冲突规则、`engine/` 的 reconcile/diff/backoff 全部保留
- **connectLoop 框架可保留**：把"等 `client.Done()`" 换成"等 master 进程退出 / `-O check` 失败"

代价：~35-60h 的重构 + 测试改写。但新代码贴合用户"尽量用 shell"的偏好。

### 列举 Host 的技术现实

**OpenSSH 没有"列出所有已配置 Host"的官方命令。** `ssh -G` 必须给具体 host 名。
要列举只能解析 `~/.ssh/config`（含 `Include`）的内容。可行的"命令行"方式：

```bash
# 用 shell 命令（grep/awk）解析，过滤通配符，只留具体别名
grep -iE '^[[:space:]]*Host[[:space:]]' ~/.ssh/config \
  | awk '{for(i=2;i<=NF;i++) print $i}' \
  | grep -vE '[*?!]'
```

→ 这仍然"读了"config 文件，只是用的是 shell 工具而非 Go 文件 IO。
**需要向用户澄清"不能直接读 config"的边界**：是指"不要 Go open 文件 parse、改用 shell 命令读"，还是"连 grep 都不行"（若如此则技术上无法自动列举，只能让用户手输 host 名）。

### UI 显示默认值（已明确，无歧义）

- 扫描周期：默认 15s，界面上要明确显示这是默认值（如 placeholder 或灰字提示"默认 15 秒"）
- 排除端口：界面初始加载就要显示默认 `[22,53,80,443,111,631]` 这些 tag（当前可能 config 已有值但 UI 没渲染出来，需排查）


---

## 第三轮（2026-05-21）：核心决策锁定

### 用户拍板

1. **确认方案 B**（fork 系统 ssh 命令 + ControlMaster），同意重写，"明显会简单很多"。
2. **列举 host 用 shell 工具读 config**（解读 1）：可以用 grep/awk 等 shell 命令读 `~/.ssh/config`，不要在 Go 里用文件 IO 解析 ssh config 语法。

### 方案 B 落地的关键技术决策（待用户最终过目）

1. **master 进程管理**：用 Go `exec.Command` 启动 `ssh -M -S <sock> -N <alias>`（**不用 `-f`**，保持前台以便 Go 持有 `*exec.Cmd` 句柄、监听退出）。socket 放 APP 数据目录（如 `<AppData>/auto-port-forward/ctl/<sanitized-alias>.sock`，注意 macOS unix socket 路径 ≤104 字符限制，别名需 sanitize）。
2. **探活 / 重连**：保留现有 `connectLoop` + backoff/degraded 框架。
   - "已连接" = master 进程在 + `ssh -S <sock> -O check <alias>` 成功
   - "断开" = master 进程退出（`cmd.Wait()` 返回）或周期 `-O check` 失败
3. **扫描执行器**：`scan.Executor.Run` 改为 `exec.CommandContext(ctx, "ssh", "-S", sock, alias, cmd)`。`ParseSS` / `ParseProcNetTCP` 等纯解析逻辑**完全保留**。
4. **转发**：`ssh -S <sock> -O forward -L 127.0.0.1:N:127.0.0.1:N <alias>` 加；`-O cancel -L ...` 删。命令退出码 + stderr 判断成败。废弃 Go 的 `forward/bridge.go` listen+bridge。
5. **冲突判定**：保留 `reconcile` 里基于 `LocalScan` 的本地占用判断（→ conflict）。删除 `local_port_offset`（localPort 恒等 remotePort）、`only_public_bind`。
6. **host key 行为（安全相关，需确认）**：**不**附加任何 `-o StrictHostKeyChecking=...`，完全沿用用户的 ssh 默认行为。若首次连接未知 host 失败，提示用户"请先在终端手动 `ssh <alias>` 接受 host key"。—— 最贴合"不处理认证"的精神。
7. **唯一标识**：用 SSH config 的 Host 别名（alias）。`model.Forward.ServerID` 语义改为存 alias。
8. **数据模型**：APP config.toml 只存 `scan_interval_sec` + `rules`（仅 exclude_ports/exclude_ranges）+ 启用监控的别名集合（默认禁用）。
9. **测试（TDD）**：抽象一个 `Runner` 接口（封装"执行 ssh 命令"），单测注入 fake runner 验证命令参数组装 + 输出解析，不真 fork；集成测试用 integration tag 跑真实 host。
10. **范围限制**：列举别名 MVP 先支持主 `~/.ssh/config`（含其直接 `Include` 的文件），通配符别名（`* ? !`）过滤掉。

### 沿用上一轮推荐（用户"其余基本没问题"默认接受）

- Q2：过滤通配符别名，只留具体别名
- Q3：启用状态存 config，默认**禁用**
- Q5：启动读一次 + UI"刷新 SSH 配置"按钮
- Q7：孤儿状态保留（同名 host 再现时恢复）
- Q8：删除 `ServersView.vue` / `ServerForm.vue` / 路由 `/servers` / `i18n servers.*` / CRUD API
- Q9：保留"试连接"功能（移到服务器卡片上）


---

## 第四轮（2026-05-21）：host key 决策确认，讨论对齐完成

- **host key 处理**：用户确认 —— **不附加任何 `-o StrictHostKeyChecking` 选项**，完全沿用用户 ssh 默认行为。首次连接未知 host 失败时，界面提示用户先在终端手动 `ssh <alias>` 接受 host key。

### 所有核心决策已对齐，进入 Plan 模式

1. 方案 B：fork 系统 ssh + ControlMaster，重写连接/转发层
2. 列举 host：shell grep/awk 读 `~/.ssh/config`
3. host key：不附加任何选项
4. 删除：服务器 CRUD、`local_port_offset`、`only_public_bind`
5. UI：监控页按服务器分组卡片 + 监控开关；设置页显示默认值
6. 数据模型：config 仅存 scan_interval + rules + 启用别名集合（默认禁用）

# auto-port-forward

macOS 桌面应用：定时扫描远端 SSH 服务器上正在监听的 TCP 端口，自动建立同号 `ssh -L` 转发到本地，并在界面上展示端口状态（转发中 / 等待 / 本地占用 / 需 root 等）。

> 痛点：手动维护一堆 `ssh -L <port>:localhost:<port>` 太累，且远端端口会变（新跑了 docker 服务等）。本应用做"自动同步"，零干预。

## 技术栈

- **后端**：Go 1.25 + `golang.org/x/crypto/ssh` + Wails v2.11+
- **前端**：Vue 3 + Naive UI + Pinia + vue-router + TypeScript（hash 路由兼容 Wails 内嵌）
- **配置**：TOML（`~/Library/Application Support/auto-port-forward/config.toml`）
- **远端扫描**：通过 SSH 在远端跑 `ss -H -tlnp` / 读 `/proc/net/tcp{,6}` —— 不依赖远端装第三方工具
- **本地扫描**：`sonar list --json`（用户已装的 CLI）

## 功能

- 多 server 管理，热插拔启停（不需重启应用）
- 定时扫描（默认 15s），手动「立即扫描」一键触发
- 远端端口 N → 本地 N 默认同号；可选「本地端口偏移」把 80 → 20080 等
- 冲突识别：
  - 排除规则（默认 `22/53/80/443/111/631` 等）
  - 本地端口已被占用 → 标 `conflict`，不抢占
  - <1024 非 root → 标 `conflict_priv`，提示用 offset
  - `OnlyPublicBind` 仅转发 `0.0.0.0`/`::` 的端口
- SSH 连接断线自动重连，指数退避；超过 15 分钟仍连不上 → 上报 `degraded` 状态
- 三种认证：SSH Agent / 私钥 / 密码

## 目录结构

```
auto-port-forward/
├── main.go              # wails 入口
├── app.go               # *App: Wails 绑定门面（CRUD/扫描/启停/快照）
├── wails.json
├── internal/
│   ├── model/           # 纯数据模型
│   ├── config/          # TOML 读写 + Store（原子写、深拷贝、CRUD）
│   ├── scan/            # 远端 ss/proc/net + 本地 sonar 扫描
│   ├── conflict/        # 冲突分类规则（纯函数）
│   ├── sshpool/         # ssh.Client 生命周期 + 重连退避（NextDelay/IsDegraded）
│   ├── forward/         # 单条端口转发（listen→accept→bridge over ssh.Dial）
│   ├── engine/          # 编排：扫描→冲突→diff→启停 forward；连接守护循环
│   └── events/          # 事件名常量 + Emitter 接口（wails 适配）
└── frontend/
    ├── src/
    │   ├── types.ts           # 镜像 Go model 的 TS 类型
    │   ├── api/{wails,mock}.ts # wails detect + 浏览器开发模式 mock fallback
    │   ├── store/state.ts     # Pinia 全局状态 + 事件订阅
    │   ├── components/        # PortTable / ServerForm / StatusTag
    │   ├── views/             # MonitorView / ServersView / SettingsView
    │   └── i18n/zh.ts         # 中文文案
```

## 开发

### 后端单测

```bash
go test ./... -count=1
```

### 集成测试（需要本机 sshd + 用户私钥）

```bash
go test -tags=integration ./internal/forward/ ./internal/sshpool/ -count=1
```

### 前端独立开发（浏览器 mock 模式）

```bash
cd frontend && npm i && npm run dev
# 默认 http://localhost:5173
```

前端通过 `window.go` 是否存在判断当前是 wails 内嵌还是浏览器开发模式；浏览器模式走 `api/mock.ts` 中的本地 mock 数据。

### Wails 调试（Mac 上）

```bash
wails dev
```

## 构建 macOS App

```bash
# 准备工具链
brew install go node
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 编译 universal binary
cd auto-port-forward
wails build -platform darwin/universal -clean
# 产物: build/bin/auto-port-forward.app
```

## 配置

应用会在首次启动时创建：

```
~/Library/Application Support/auto-port-forward/config.toml
```

示例：

```toml
scan_interval_sec = 15

[[servers]]
id = "srv-xxxxx"
name = "ubt"
host = "10.0.0.42"
port = 22
user = "ubuntu"
auth_method = "ssh_agent"   # ssh_agent / ssh_key / password
key_path = ""
host_key = "known_hosts"    # known_hosts / insecure
enabled = true

[rules]
exclude_ports = [22, 53, 80, 443, 111, 631]
exclude_ranges = []
only_public_bind = false
local_port_offset = 0
```

## 端到端验收（Mac）

1. 打开 App，添加 `ubt` server（ssh-agent 认证），点扫描
2. 表格出现远端端口列表；非冲突端口在 30 秒内变成绿色 `forwarding`
3. `lsof -nP -iTCP -sTCP:LISTEN | grep auto-port-forward` 能看到对应本地监听
4. `curl http://localhost:<被转发的端口>/` 能透过隧道访问远端服务
5. 手工 `kill` 一个本地占用端口、再扫描，对应行从 `conflict` 变 `forwarding`
6. 断网模拟：拔网卡 / `ssh ubt sudo iptables -A INPUT -p tcp --dport 22 -j DROP` —— 服务器视图应在 30s 内显示 `断开`，恢复后回到 `已连接`

## 关键设计

- **业务垂直切片**：每个 `internal/*` 子包按业务维度（不按技术分层）拆分；每个文件 ≤300 行
- **TDD**：解析器 / 冲突规则 / diff / 退避 / 配置 / bridge / supervise 全部先写测试再实现
- **依赖注入**：`engine.Deps`（ClientFactory / LocalScan / Now / Sleep / Backoff）让所有逻辑都能在不连真实 SSH、不真实等待时间的情况下测试
- **接口隔离**：`engine.ClientHandle` 抽象 SSH 客户端能力；`events.Emitter` 抽象 wails runtime
- **热插拔**：服务器增/删/改通过 `engine.ApplyServers` 在运行时按 ID diff 启停，无需重启应用
- **mock fallback**：前端在 wails runtime 不存在时自动走 mock，便于在 Linux 上仅用浏览器开发 UI

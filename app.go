package main

import (
	"context"

	"autoportforward/internal/config"
	"autoportforward/internal/engine"
	"autoportforward/internal/events"
	"autoportforward/internal/model"
)

// App 是 Wails 绑定的门面，所有 Vue 调用都打到这些方法上。
type App struct {
	ctx    context.Context
	cfg    config.Config
	engine *engine.Engine
}

// NewApp 构造未启动的 App。
func NewApp() *App { return &App{} }

// Startup 由 wails 在窗口就绪后调用。
// TODO(M6): 读取 config → 构造 engine(emitter=wailsRuntime) → engine.StartAll(ctx)。
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	cfg, _ := config.Load("") // TODO(M6): config.DefaultPath()
	a.cfg = cfg
	a.engine = engine.New(cfg, events.NopEmitter{})
}

// Shutdown 由 wails 在退出前调用。
// TODO(M6): a.engine.StopAll()。
func (a *App) Shutdown(ctx context.Context) {
	_ = ctx
	if a.engine != nil {
		_ = a.engine.StopAll()
	}
}

// --- Wails 绑定: Server CRUD ---

// ListServers 返回当前配置中的全部服务器。
func (a *App) ListServers() []config.Server { return a.cfg.Servers }

// AddServer 新增一个服务器。
// TODO(M6): 生成 ID（uuid 或时间戳）→ 持久化 → engine.UpdateServers。
func (a *App) AddServer(s config.Server) (config.Server, error) { return s, nil }

// UpdateServer 更新现有服务器。
// TODO(M6): 按 ID 替换 → 持久化 → engine.UpdateServers。
func (a *App) UpdateServer(s config.Server) error { _ = s; return nil }

// DeleteServer 删除一个服务器。
// TODO(M6): 按 ID 删除 → 持久化 → engine.UpdateServers。
func (a *App) DeleteServer(id string) error { _ = id; return nil }

// TestServer 尝试连接给定服务器，返回错误说明。
// TODO(M6): 构造一次性 sshpool.Client → Connect → 关闭。
func (a *App) TestServer(id string) error { _ = id; return nil }

// --- Wails 绑定: 配置 ---

// GetConfig 返回完整配置（含规则）。
func (a *App) GetConfig() config.Config { return a.cfg }

// UpdateRules 替换规则。
func (a *App) UpdateRules(r config.Rules) error {
	a.cfg.Rules = r
	if a.engine != nil {
		return a.engine.UpdateRules(r)
	}
	return nil
}

// --- Wails 绑定: 运行控制 ---

// StartAll 启动所有 enabled 服务器的转发。
func (a *App) StartAll() error {
	if a.engine != nil {
		return a.engine.StartAll(a.ctx)
	}
	return nil
}

// StopAll 停止所有转发。
func (a *App) StopAll() error {
	if a.engine != nil {
		return a.engine.StopAll()
	}
	return nil
}

// ScanNow 立刻触发一次扫描。
func (a *App) ScanNow() error {
	if a.engine != nil {
		return a.engine.ScanNow()
	}
	return nil
}

// ToggleForward 临时启用/禁用某端口转发。
func (a *App) ToggleForward(serverID string, port int, on bool) error {
	if a.engine != nil {
		return a.engine.ToggleForward(serverID, port, on)
	}
	return nil
}

// GetSnapshot 返回所有 forward 当前快照。
func (a *App) GetSnapshot() []model.Forward {
	if a.engine != nil {
		return a.engine.Snapshot()
	}
	return nil
}

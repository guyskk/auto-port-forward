package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/engine"
	"auto-port-forward/internal/events"
	"auto-port-forward/internal/model"
	"auto-port-forward/internal/scan"
	"auto-port-forward/internal/sshpool"
)

// App 是 Wails 绑定的门面，所有 Vue 调用都打到这些方法上。
//
// 持久化路径由 config.DefaultPath() 决定，可被 NewAppWithStore 替换以便测试。
type App struct {
	ctx    context.Context
	store  *config.Store
	engine *engine.Engine
	emit   events.Emitter
}

// NewApp 构造未启动的 App。Startup 完成 store/engine 装配。
func NewApp() *App {
	// emit 在 Startup 时用 wails ctx 重建；这里先留 nil，setup 再赋值。
	return &App{}
}

// Startup 由 wails 在窗口就绪后调用：加载配置 + 装配 engine。
func (a *App) Startup(ctx context.Context) {
	path, err := config.DefaultPath()
	if err != nil {
		log.Printf("config path: %v", err)
		path = "config.toml"
	}
	if err := a.setup(ctx, path); err != nil {
		log.Printf("app setup: %v", err)
	}
}

// setup 装配 store + engine 并启动；从 Startup / 测试入口共用。
func (a *App) setup(ctx context.Context, path string) error {
	a.ctx = ctx
	store, err := config.NewStore(path)
	if err != nil {
		return err
	}
	a.store = store
	if a.emit == nil {
		a.emit = events.NewWailsEmitter(ctx)
	}
	a.engine = engine.New(store.Snapshot(), a.emit, engine.Deps{
		ClientFactory: func(s config.Server) engine.ClientHandle { return sshpool.NewClient(s) },
		LocalScan:     scan.ScanLocal,
		IsRoot:        os.Geteuid() == 0,
	})
	return a.engine.StartAll(ctx)
}

// Shutdown 由 wails 在退出前调用。
func (a *App) Shutdown(ctx context.Context) {
	_ = ctx
	if a.engine != nil {
		_ = a.engine.StopAll()
	}
}

// --- Wails 绑定: Server CRUD ---

// ListServers 返回当前配置中的全部服务器。
func (a *App) ListServers() []config.Server {
	if a.store == nil {
		return nil
	}
	return a.store.Servers()
}

// AddServer 新增一个服务器，返回带 ID 的副本。
func (a *App) AddServer(s config.Server) (config.Server, error) {
	if a.store == nil {
		return config.Server{}, errStoreNotReady
	}
	created, err := a.store.AddServer(s)
	if err != nil {
		return config.Server{}, err
	}
	a.syncEngineServers()
	return created, nil
}

// UpdateServer 更新现有服务器（按 ID）。
func (a *App) UpdateServer(s config.Server) error {
	if a.store == nil {
		return errStoreNotReady
	}
	if err := a.store.UpdateServer(s); err != nil {
		return err
	}
	a.syncEngineServers()
	return nil
}

// DeleteServer 删除一个服务器。
func (a *App) DeleteServer(id string) error {
	if a.store == nil {
		return errStoreNotReady
	}
	if err := a.store.DeleteServer(id); err != nil {
		return err
	}
	a.syncEngineServers()
	return nil
}

// TestServer 同步建立一次 SSH 连接并关闭，返回错误说明。
func (a *App) TestServer(id string) error {
	if a.store == nil {
		return errStoreNotReady
	}
	s, ok := a.store.GetServer(id)
	if !ok {
		return fmt.Errorf("server %s not found", id)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	c := sshpool.NewClient(s)
	if err := c.Connect(ctx); err != nil {
		return err
	}
	return c.Close()
}

// --- Wails 绑定: 配置 ---

// GetConfig 返回完整配置（含规则）。
func (a *App) GetConfig() config.Config {
	if a.store == nil {
		return config.DefaultConfig()
	}
	return a.store.Snapshot()
}

// UpdateRules 替换规则并持久化，同步给 engine 重新 reconcile。
func (a *App) UpdateRules(r config.Rules) error {
	if a.store == nil {
		return errStoreNotReady
	}
	if err := a.store.UpdateRules(r); err != nil {
		return err
	}
	if a.engine != nil {
		return a.engine.UpdateRules(r)
	}
	return nil
}

// UpdateScanInterval 调整扫描周期（秒）。
func (a *App) UpdateScanInterval(sec int) error {
	if a.store == nil {
		return errStoreNotReady
	}
	return a.store.UpdateScanInterval(sec)
	// 注：当前调度循环周期在 StartAll 时绑定；下次重启生效。
	// TODO(M8+): 支持运行时动态更新调度周期。
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
		return a.engine.ScanNow(a.ctx)
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

// syncEngineServers 把 store 中的最新 server 列表热推给 engine。
func (a *App) syncEngineServers() {
	if a.engine == nil || a.store == nil {
		return
	}
	if err := a.engine.ApplyServers(a.store.Servers()); err != nil {
		log.Printf("ApplyServers: %v", err)
	}
}

var errStoreNotReady = errors.New("app: config store not initialized; Startup must run first")

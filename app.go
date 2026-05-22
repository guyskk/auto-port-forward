package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/engine"
	"auto-port-forward/internal/events"
	"auto-port-forward/internal/model"
	"auto-port-forward/internal/scan"
	"auto-port-forward/internal/sshcfg"
	"auto-port-forward/internal/sshctl"
)

// App 是 Wails 绑定的门面，所有 Vue 调用都打到这些方法上。
//
// 持久化路径由 config.DefaultPath() 决定，可被 NewAppWithStore 替换以便测试。
type App struct {
	ctx        context.Context
	store      *config.Store
	engine     *engine.Engine
	emit       events.Emitter
	controlDir string
	sshRunner  sshcfg.Runner

	// clientFactory 可被测试覆盖；为 nil 时使用 sshctl.NewClient。
	clientFactory func(host sshcfg.Host) engine.ClientHandle
}

// NewApp 构造未启动的 App。Startup 完成 store/engine 装配。
func NewApp() *App {
	return &App{}
}

// Startup 由 wails 在窗口就绪后调用：加载配置 + 装配 engine + 启动。
func (a *App) Startup(ctx context.Context) {
	path, err := config.DefaultPath()
	if err != nil {
		log.Printf("config path: %v", err)
		path = "config.toml"
	}
	if err := a.setup(ctx, path); err != nil {
		log.Printf("app setup: %v", err)
		return
	}
	if err := a.engine.StartAll(ctx); err != nil {
		log.Printf("engine start: %v", err)
		return
	}
	// 启动后异步同步一次 ssh config 列表 → engine。
	if err := a.ReloadSSHConfig(); err != nil {
		log.Printf("reload ssh config: %v", err)
	}
}

// setup 装配 store + engine（不启动 engine — 测试可独立验证）。
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
	if a.sshRunner == nil {
		a.sshRunner = sshcfg.NewDefaultRunner()
	}
	if a.controlDir == "" {
		dir, derr := os.UserConfigDir()
		if derr != nil {
			dir = "."
		}
		a.controlDir = filepath.Join(dir, "auto-port-forward", "ctl")
	}
	factory := a.clientFactory
	if factory == nil {
		runner := sshctl.NewDefaultRunner()
		controlDir := a.controlDir
		factory = func(h sshcfg.Host) engine.ClientHandle {
			return sshctl.NewClient(h, runner, controlDir)
		}
	}
	a.engine = engine.New(store.Snapshot(), a.emit, engine.Deps{
		ClientFactory: factory,
		LocalScan:     scan.ScanLocal,
		IsRoot:        os.Geteuid() == 0,
	})
	return nil
}

// Shutdown 由 wails 在退出前调用。
func (a *App) Shutdown(ctx context.Context) {
	_ = ctx
	if a.engine != nil {
		_ = a.engine.StopAll()
	}
}

// --- Wails 绑定: SSH host 列举 / 启用 ---

// ListHosts 返回 ~/.ssh/config 中所有具体（非通配符）的别名及其 effective 配置。
func (a *App) ListHosts() ([]sshcfg.Host, error) {
	if a.sshRunner == nil {
		return nil, errStoreNotReady
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	return sshcfg.ListHosts(ctx, a.sshRunner)
}

// EnabledHosts 返回启用监控的别名列表（去重；可能包含已从 ssh config 移除的孤儿别名）。
func (a *App) EnabledHosts() []string {
	if a.store == nil {
		return nil
	}
	return a.store.EnabledHosts()
}

// SetHostEnabled 启用/停用一个别名的监控并持久化。on=true 触发 engine 启动该 host 的连接；
// on=false 触发 engine 断开并取消其所有 forward。
//
// 调用本方法不会重新列举 ssh config — 用 ReloadSSHConfig 强制刷新。
func (a *App) SetHostEnabled(alias string, on bool) error {
	if a.store == nil {
		return errStoreNotReady
	}
	if err := a.store.SetHostEnabled(alias, on); err != nil {
		return err
	}
	return a.applyEnabledHosts()
}

// ReloadSSHConfig 重新读取 ~/.ssh/config，更新 engine 应监控的 host 列表。
// 启用集合不变（孤儿别名状态保留，同名再现自动恢复）。
func (a *App) ReloadSSHConfig() error {
	return a.applyEnabledHosts()
}

// TestHost 试连一个别名：ssh -G 解析 + 启动临时 master + -O check + -O exit。
// 用于"试连接"按钮，不持久化任何状态。
func (a *App) TestHost(alias string) error {
	if a.sshRunner == nil {
		return errStoreNotReady
	}
	if alias == "" {
		return errors.New("empty alias")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	host, err := sshcfg.Resolve(ctx, a.sshRunner, alias)
	if err != nil {
		return err
	}
	c := sshctl.NewClient(host, sshctl.NewDefaultRunner(), a.controlDir)
	if err := c.Connect(ctx); err != nil {
		return err
	}
	return c.Close()
}

// --- Wails 绑定: 配置 ---

// GetConfig 返回完整配置（含规则 + 启用集合）。
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

// UpdateScanInterval 调整扫描周期（秒），下次重启生效。
func (a *App) UpdateScanInterval(sec int) error {
	if a.store == nil {
		return errStoreNotReady
	}
	return a.store.UpdateScanInterval(sec)
	// TODO(M8+): 支持运行时动态更新调度周期。
}

// --- Wails 绑定: 运行控制 ---

// StartAll 启动所有 enabled host 的连接 + 扫描循环。
func (a *App) StartAll() error {
	if a.engine == nil {
		return nil
	}
	if err := a.engine.StartAll(a.ctx); err != nil {
		return err
	}
	return a.applyEnabledHosts()
}

// StopAll 停止所有连接与转发。
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

// ToggleForward 持久化用户对单个端口的启用/禁用意图。
//
// on=false: 把 serverID:port 加入 store.DisabledPorts；重启后仍然记得，
//           reconcile 跳过该端口、若在运行则被 cancel。
// on=true:  从禁用集合中移除该端口，下次扫描如该端口仍 listen 则恢复 forwarding。
//
// store 写盘失败时不调用 engine；engine 失败时不回滚 store（与 SetHostEnabled 同语义，
// 失败留待下次 scan 自然纠正）。
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

// GetSnapshot 返回所有 forward 当前快照。
func (a *App) GetSnapshot() []model.Forward {
	if a.engine != nil {
		return a.engine.Snapshot()
	}
	return nil
}

// --- 内部 ---

// applyEnabledHosts 把 ListHosts() ∩ EnabledHosts() 推给 engine。
func (a *App) applyEnabledHosts() error {
	if a.engine == nil || a.store == nil {
		return nil
	}
	hosts, err := a.ListHosts()
	if err != nil {
		// 列举失败也要继续：用空列表 → engine 会关掉所有连接。
		// 这种情况通常是 ssh config 不存在；同步给 engine 是一致的语义。
		log.Printf("list hosts: %v", err)
		hosts = nil
	}
	enabled := a.store.EnabledHosts()
	enabledSet := make(map[string]bool, len(enabled))
	for _, alias := range enabled {
		enabledSet[alias] = true
	}
	var filtered []sshcfg.Host
	for _, h := range hosts {
		if enabledSet[h.Alias] {
			filtered = append(filtered, h)
		}
	}
	if err := a.engine.ApplyServers(filtered); err != nil {
		log.Printf("ApplyServers: %v", err)
		return fmt.Errorf("apply servers: %w", err)
	}
	return nil
}

var errStoreNotReady = errors.New("app: config store not initialized; Startup must run first")

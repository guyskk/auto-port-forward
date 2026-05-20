// Package engine 编排扫描、冲突识别、转发增删与事件推送。
package engine

import (
	"context"
	"sync"

	"autoportforward/internal/config"
	"autoportforward/internal/events"
	"autoportforward/internal/model"
)

// Engine 是顶层编排器，由 app.go 持有。
type Engine struct {
	cfg     config.Config
	emit    events.Emitter
	mu      sync.RWMutex
	running bool
	// TODO(M5): forwards map[serverID]map[port]*forward.Forward
	// TODO(M5): clients  map[serverID]*sshpool.Client
}

// New 构造 Engine。
func New(cfg config.Config, emit events.Emitter) *Engine {
	return &Engine{cfg: cfg, emit: emit}
}

// StartAll 启动所有 enabled server 的 ssh + 扫描 + 转发循环。
// TODO(M5): 启动定时扫描 goroutine（间隔 cfg.ScanIntervalSec）。
// TODO(M5): 启动 ssh 重连守护。
func (e *Engine) StartAll(ctx context.Context) error {
	_ = ctx
	return nil
}

// StopAll 停止所有循环并关闭 ssh client。
// TODO(M5): cancel ctx → 等待 wg → 关闭 clients。
func (e *Engine) StopAll() error { return nil }

// ScanNow 立刻触发一次扫描。
// TODO(M5): 给定时器发信号，或直接调用 scanOnce。
func (e *Engine) ScanNow() error { return nil }

// Snapshot 返回当前所有 forward 的状态快照（线程安全）。
// TODO(M5): 读 RLock；浅拷贝。
func (e *Engine) Snapshot() []model.Forward { return nil }

// ToggleForward 临时强制启用 / 禁用某端口转发。
// TODO(M5): 标记 forced；下次 diff 不会因 desired 中无该端口而删除。
func (e *Engine) ToggleForward(serverID string, port int, on bool) error {
	_ = serverID
	_ = port
	_ = on
	return nil
}

// UpdateRules 替换规则；触发一次 diff 重算。
// TODO(M5)。
func (e *Engine) UpdateRules(r config.Rules) error { _ = r; return nil }

// UpdateServers 替换 server 列表；新增/删除时同步 client。
// TODO(M5)。
func (e *Engine) UpdateServers(servers []config.Server) error { _ = servers; return nil }

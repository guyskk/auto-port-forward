package engine

import (
	"context"
	"errors"

	"autoportforward/internal/config"
)

// ToggleForward 临时强制启用 / 禁用某端口转发。
// TODO(M6): 接入 forced 集合后下次 diff 忽略该端口。
func (e *Engine) ToggleForward(serverID string, port int, on bool) error {
	_ = serverID
	_ = port
	_ = on
	return nil
}

// UpdateRules 替换规则；触发一次 diff 重算。
func (e *Engine) UpdateRules(r config.Rules) error {
	e.mu.Lock()
	e.cfg.Rules = r
	running := e.running
	e.mu.Unlock()
	if running {
		_ = e.ScanNow(context.Background())
	}
	return nil
}

// UpdateServers 替换 server 列表。仅在未运行时支持；运行时变更需 StopAll/StartAll。
// TODO(M6): 支持热插拔。
func (e *Engine) UpdateServers(servers []config.Server) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return errors.New("engine: stop before updating servers")
	}
	e.cfg.Servers = servers
	return nil
}

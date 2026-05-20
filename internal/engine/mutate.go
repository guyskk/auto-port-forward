package engine

import (
	"context"
	"errors"

	"autoportforward/internal/config"
)

// ToggleForward 临时强制启用 / 禁用某端口转发。
// TODO(M7+): 接入 forced 集合后下次 diff 忽略该端口。
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

// UpdateServers 替换 server 列表（仅在未运行时支持）。
// 运行时变更请使用 ApplyServers 做热插拔。
func (e *Engine) UpdateServers(servers []config.Server) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return errors.New("engine: use ApplyServers when running")
	}
	e.cfg.Servers = servers
	return nil
}

// ApplyServers 热插拔 server 列表。运行时按 ID 对比当前 vs 新列表：
//   - ID 新增且 Enabled=true → spawn 新 connectLoop
//   - ID 消失、被禁用、配置变化（任意字段不同）→ 停止旧 client，释放其 forward
//
// 非运行态直接覆盖 e.cfg.Servers，等同 UpdateServers。
func (e *Engine) ApplyServers(newList []config.Server) error {
	e.mu.Lock()
	if !e.running {
		e.cfg.Servers = newList
		e.mu.Unlock()
		return nil
	}

	newByID := make(map[string]config.Server, len(newList))
	for _, s := range newList {
		newByID[s.ID] = s
	}

	var toStop []*serverState
	for id, st := range e.servers {
		ns, ok := newByID[id]
		// 停止条件：被移除 / 被禁用 / 配置变更（Server 全部为值字段，可直接比较）
		if !ok || !ns.Enabled || ns != st.cfg {
			toStop = append(toStop, st)
			delete(e.servers, id)
		}
	}

	var toStart []config.Server
	for _, ns := range newList {
		if !ns.Enabled {
			continue
		}
		if _, ok := e.servers[ns.ID]; !ok {
			toStart = append(toStart, ns)
		}
	}

	e.cfg.Servers = newList
	startCtx := e.startCtx
	for _, ns := range toStart {
		e.spawnServer(startCtx, ns)
	}
	e.mu.Unlock()

	// 停止旧 server：取消 connectLoop、关闭所有 forward、关闭 client。
	for _, st := range toStop {
		st.cancel()
		st.mu.Lock()
		for _, h := range st.forwards {
			h.cancel()
		}
		st.forwards = nil
		c := st.client
		st.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
	}
	return nil
}

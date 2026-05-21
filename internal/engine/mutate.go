package engine

import (
	"context"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/sshcfg"
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

// ApplyServers 热插拔启用 host 列表。运行时按 alias 对比当前 vs 新列表：
//   - alias 新增 → spawn 新 connectLoop
//   - alias 消失 → 停止旧 client，释放其 forward
//   - alias 存在但 Host 值改变（hostname/user/port 变化）→ 重启
//
// 非运行态直接覆盖 e.hosts，等同保留状态待 StartAll 启动。
func (e *Engine) ApplyServers(enabled []sshcfg.Host) error {
	e.mu.Lock()
	if !e.running {
		e.hosts = append([]sshcfg.Host(nil), enabled...)
		e.mu.Unlock()
		return nil
	}

	newByAlias := make(map[string]sshcfg.Host, len(enabled))
	for _, h := range enabled {
		newByAlias[h.Alias] = h
	}

	var toStop []*serverState
	for alias, st := range e.servers {
		nh, ok := newByAlias[alias]
		// 停止条件：被移除 / 配置变更
		if !ok || nh != st.cfg {
			toStop = append(toStop, st)
			delete(e.servers, alias)
		}
	}

	var toStart []sshcfg.Host
	for _, nh := range enabled {
		if _, ok := e.servers[nh.Alias]; !ok {
			toStart = append(toStart, nh)
		}
	}

	e.hosts = append([]sshcfg.Host(nil), enabled...)
	startCtx := e.startCtx
	for _, nh := range toStart {
		e.spawnServer(startCtx, nh)
	}
	e.mu.Unlock()

	// 停止旧 host：取消 connectLoop、关闭所有 forward、关闭 client。
	for _, st := range toStop {
		st.cancel()
		st.mu.Lock()
		st.forwards = nil
		c := st.client
		st.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
	}
	return nil
}

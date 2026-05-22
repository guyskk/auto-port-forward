package engine

import (
	"context"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/sshcfg"
)

// ToggleForward 持久化用户对单个端口的启用/禁用意图，并在运行态触发一次重扫。
//
// engine 只更新自身内存中的 cfg.DisabledPorts；持久化由调用方（app.go）通过 store 完成。
// 这与 UpdateRules / SetHostEnabled 的现有分层一致：engine 管运行态，store 管落盘。
//
// on=true:  从禁用集合中移除该端口（幂等）。
// on=false: 加入禁用集合（去重排序，幂等）。
//
// serverID 实质就是 alias（与 EnabledHosts / scanServer 中的 cfg.Alias 同义）。
func (e *Engine) ToggleForward(serverID string, port int, on bool) error {
	e.mu.Lock()
	if e.cfg.DisabledPorts == nil {
		e.cfg.DisabledPorts = map[string][]int{}
	}
	cur := append([]int(nil), e.cfg.DisabledPorts[serverID]...)
	next := toggleInIntSet(cur, port, !on)
	if len(next) == 0 {
		delete(e.cfg.DisabledPorts, serverID)
	} else {
		e.cfg.DisabledPorts[serverID] = next
	}
	running := e.running
	e.mu.Unlock()
	if running {
		_ = e.ScanNow(context.Background())
	}
	return nil
}

// toggleInIntSet 把 port 是否在 set 内调整为 want。返回值去重升序。
// 与 config.toggleInSet 同语义，独立实现以避免跨包依赖。
func toggleInIntSet(set []int, port int, want bool) []int {
	has := false
	out := make([]int, 0, len(set)+1)
	for _, p := range set {
		if p == port {
			if has {
				continue
			}
			has = true
			if want {
				out = append(out, p)
			}
			continue
		}
		out = append(out, p)
	}
	if want && !has {
		out = append(out, port)
	}
	sortInts(out)
	return out
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

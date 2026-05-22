package engine

import (
	"context"
	"sort"

	"auto-port-forward/internal/events"
	"auto-port-forward/internal/model"
)

// scanTick 执行一次扫描：对每个 host 扫描远端 → reconcile → 执行 diff → 写入 snapshot。
//
// 不在 mu 内执行（耗时操作）；host 列表在 StartAll 时固定，StopAll 后 ctx.Done。
func (e *Engine) scanTick(ctx context.Context) error {
	// 复制 server 切片，避免持锁执行 I/O。
	e.mu.Lock()
	sts := make([]*serverState, 0, len(e.servers))
	for _, s := range e.servers {
		sts = append(sts, s)
	}
	e.mu.Unlock()

	// 全局本地扫描（一次性）：sonar 列出本机所有 LISTEN。
	var localPorts []model.LocalPort
	if e.deps.LocalScan != nil {
		lp, err := e.deps.LocalScan(ctx)
		if err == nil {
			localPorts = lp
		}
		// sonar 不可用不致命：localPorts 为空表示无法识别本地占用。
	}

	var combined []model.Forward
	var firstErr error

	for _, st := range sts {
		snap, err := e.scanServer(ctx, st, localPorts)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		combined = append(combined, snap...)
	}

	// 写入 planned 并广播。Snapshot() 调用时会叠加实时状态。
	sort.SliceStable(combined, func(i, j int) bool {
		if combined[i].ServerID != combined[j].ServerID {
			return combined[i].ServerID < combined[j].ServerID
		}
		return combined[i].RemotePort < combined[j].RemotePort
	})
	e.snapshotMu.Lock()
	e.planned = combined
	e.snapshotMu.Unlock()
	if e.emit != nil {
		e.emit.Emit(ctx, events.EventStateUpdate, e.Snapshot())
	}
	return firstErr
}

// scanServer 处理单个 host 的扫描循环：远端扫描 → 计算 Inputs → Reconcile → 启停 forward。
func (e *Engine) scanServer(ctx context.Context, st *serverState, local []model.LocalPort) ([]model.Forward, error) {
	remote, err := st.scanner.Scan(ctx, execAdapter{st.client})
	if err != nil {
		if e.emit != nil {
			e.emit.Emit(ctx, events.EventScanError, map[string]any{
				"server_id": st.cfg.Alias,
				"error":     err.Error(),
			})
		}
		// 即便扫描失败，也要保留现有 forward 的状态视图，避免 UI 短暂闪烁。
		return e.currentSnapshot(st), err
	}

	// 计算 LocalOccupied 映射（合并 sonar 输出与本程序自身 listen）。
	occupied := buildLocalOccupied(local)

	// 计算 CurrentForward：本程序当前已经在 listen 的 (remote port) 集合。
	st.mu.Lock()
	currentMap := make(map[int]bool, len(st.forwards))
	for p := range st.forwards {
		currentMap[p] = true
	}
	st.mu.Unlock()

	// 取该 host 的用户禁用端口集合（cfg 由 ToggleForward / 启动时 store snapshot 维护）。
	e.mu.Lock()
	disabled := make(map[int]bool, len(e.cfg.DisabledPorts[st.cfg.Alias]))
	for _, p := range e.cfg.DisabledPorts[st.cfg.Alias] {
		disabled[p] = true
	}
	e.mu.Unlock()

	in := Inputs{
		ServerID:       st.cfg.Alias,
		Remote:         remote,
		LocalOccupied:  occupied,
		CurrentForward: currentMap,
		DisabledPorts:  disabled,
		Rules:          e.cfg.Rules,
		IsRoot:         e.deps.IsRoot,
	}
	out := Reconcile(in)

	// 执行 diff。
	for _, op := range out.Diff {
		switch op.Kind {
		case "add":
			e.startForward(ctx, st, op.Port)
		case "del":
			e.stopForward(ctx, st, op.Port)
		}
	}

	// 返回 Reconcile 的原始 snapshot；实时状态由 Snapshot() 在读取时叠加。
	return out.Snapshot, nil
}

// execAdapter 适配 ClientHandle → scan.Executor。
type execAdapter struct{ c ClientHandle }

func (a execAdapter) Run(ctx context.Context, cmd string) ([]byte, error) {
	return a.c.Run(ctx, cmd)
}

// buildLocalOccupied 把 sonar 结果转换为 LocalOwnership map。
// 当前不区分占用者是否为自己（self 判定通过 CurrentForward → Reconcile 内部回填）。
func buildLocalOccupied(local []model.LocalPort) map[int]LocalOwnership {
	m := make(map[int]LocalOwnership, len(local))
	for _, p := range local {
		m[p.Port] = LocalOwnership{Occupied: true, BySelf: false}
	}
	return m
}

// currentSnapshot 在扫描失败时返回该 host 现有 forward 的快照。
func (e *Engine) currentSnapshot(st *serverState) []model.Forward {
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.forwards) == 0 {
		return nil
	}
	out := make([]model.Forward, 0, len(st.forwards))
	for port, h := range st.forwards {
		h.mu.Lock()
		f := model.Forward{
			ServerID:   st.cfg.Alias,
			RemotePort: port,
			LocalPort:  port,
			Status:     h.status,
			Error:      h.errMsg,
			LastActive: h.updated.Load(),
		}
		h.mu.Unlock()
		out = append(out, f)
	}
	return out
}

// startForward 通过 ssh ControlMaster 增加一条本地转发，并登记到 st.forwards。
//
// localPort = remotePort（不再支持 LocalPortOffset）。
// AddForward 失败时记录 error 状态，但仍登记 handle — 下次 reconcile 会重试。
func (e *Engine) startForward(ctx context.Context, st *serverState, remotePort int) {
	st.mu.Lock()
	if _, exists := st.forwards[remotePort]; exists {
		st.mu.Unlock()
		return
	}
	h := &forwardHandle{}
	st.forwards[remotePort] = h
	c := st.client
	st.mu.Unlock()

	err := c.AddForward(ctx, remotePort)
	h.mu.Lock()
	if err != nil {
		h.status = model.StatusConflict
		h.errMsg = err.Error()
	} else {
		h.status = model.StatusForwarding
		h.errMsg = ""
	}
	h.mu.Unlock()
	h.updated.Store(e.deps.Now().Unix())
	if e.emit != nil {
		status := string(model.StatusForwarding)
		if err != nil {
			status = string(model.StatusConflict)
		}
		e.emit.Emit(context.Background(), events.EventForwardUpdate, map[string]any{
			"server_id":   st.cfg.Alias,
			"remote_port": remotePort,
			"status":      status,
			"error":       errString(err),
		})
	}
}

// stopForward 通过 ssh ControlMaster 取消一条本地转发并从 st.forwards 摘除。
func (e *Engine) stopForward(ctx context.Context, st *serverState, remotePort int) {
	st.mu.Lock()
	_, ok := st.forwards[remotePort]
	if ok {
		delete(st.forwards, remotePort)
	}
	c := st.client
	st.mu.Unlock()
	if !ok {
		return
	}
	// 失败也无所谓 — handle 已经摘除；下次 reconcile 不会再列出。
	_ = c.CancelForward(ctx, remotePort)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

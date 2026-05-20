// Package engine 编排扫描、冲突识别、转发增删与事件推送。
//
// 模块边界：
//   - engine.go      —— Engine 类型、生命周期、对外 API
//   - runtime.go     —— 单次 scan tick 执行逻辑（scan/reconcile/启停 forward）
//   - reconcile.go   —— 纯函数：给定输入算出 desired/diff/snapshot
//   - diff.go        —— 纯函数：current → desired 的端口集合差
package engine

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"autoportforward/internal/config"
	"autoportforward/internal/events"
	"autoportforward/internal/model"
	"autoportforward/internal/scan"
	"autoportforward/internal/sshpool"
)

// ClientHandle 抽象 SSH 客户端的全部能力：连接、关闭、远端执行、远端 TCP 通道。
// sshpool.Client 直接满足；测试中用 fake 替代。
type ClientHandle interface {
	Connect(ctx context.Context) error
	Close() error
	Run(ctx context.Context, cmd string) ([]byte, error)
	Dial(ctx context.Context, addr string) (net.Conn, error)
	// Done 返回一个 channel，连接断开（keepalive 失败 / Close 调用）时关闭。
	// 未连接时可返回 nil — select 中 nil channel 永久阻塞，不会误触发重连。
	Done() <-chan struct{}
}

// Deps 是 Engine 的注入依赖；测试与运行时各自构造。
type Deps struct {
	ClientFactory func(cfg config.Server) ClientHandle
	LocalScan     func(ctx context.Context) ([]model.LocalPort, error)
	Now           func() time.Time
	IsRoot        bool

	// Backoff 重连退避参数。零值用 sshpool.DefaultBackoff()。
	Backoff sshpool.BackoffParams
	// Sleep 是 connectLoop 用的可注入睡眠函数。测试可传 instant-return 版以避免实际等待。
	// 零值用 ctx-aware 的 time.Sleep。返回 ctx.Err() 表示中断。
	Sleep func(ctx context.Context, d time.Duration) error
}

// Engine 是顶层编排器，由 app.go 持有。
type Engine struct {
	cfg  config.Config
	emit events.Emitter
	deps Deps

	mu        sync.Mutex
	running   bool
	startCtx  context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	servers   map[string]*serverState
	triggerCh chan scanRequest

	snapshotMu sync.RWMutex
	planned    []model.Forward // 上一次 Reconcile 输出（端口 → 期望状态）
}

// serverState 每个 server 的运行态：ssh client、远端扫描缓存、当前 forward 映射。
type serverState struct {
	mu       sync.Mutex
	cfg      config.Server
	client   ClientHandle
	scanner  *scan.RemoteScanner
	forwards map[int]*forwardHandle // remote port → handle
	cancel   context.CancelFunc     // 关闭本 server 的 connectLoop
}

// forwardHandle 一条转发的运行句柄。
type forwardHandle struct {
	cancel context.CancelFunc

	mu      sync.Mutex
	status  model.PortStatus
	errMsg  string
	updated atomic.Int64
}

// scanRequest 一次 ScanNow 调用的请求。
type scanRequest struct {
	done chan error
}

// ErrNotRunning 在 StartAll 之前调用 ScanNow 等操作返回。
var ErrNotRunning = errors.New("engine not running")

// New 构造 Engine。
func New(cfg config.Config, emit events.Emitter, deps Deps) *Engine {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Engine{
		cfg:     cfg,
		emit:    emit,
		deps:    deps,
		servers: map[string]*serverState{},
	}
}

// StartAll 启动 ssh 连接和扫描调度循环。
// 调用线程安全，重复调用直接返回。
func (e *Engine) StartAll(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return nil
	}
	if e.deps.ClientFactory == nil {
		return errors.New("engine: ClientFactory is nil")
	}
	ctx, cancel := context.WithCancel(ctx)
	e.startCtx = ctx
	e.cancel = cancel
	e.triggerCh = make(chan scanRequest, 8)
	e.running = true

	for _, s := range e.cfg.Servers {
		if !s.Enabled {
			continue
		}
		e.spawnServer(ctx, s)
	}

	e.wg.Add(1)
	go e.scheduleLoop(ctx)
	return nil
}

// spawnServer 启动一个 server 的 connectLoop 并注册到 e.servers。
// 必须在持有 e.mu 时调用。
func (e *Engine) spawnServer(ctx context.Context, s config.Server) {
	sctx, cancel := context.WithCancel(ctx)
	st := &serverState{
		cfg:      s,
		client:   e.deps.ClientFactory(s),
		scanner:  scan.NewRemoteScanner(),
		forwards: map[int]*forwardHandle{},
		cancel:   cancel,
	}
	e.servers[s.ID] = st
	e.wg.Add(1)
	go e.connectLoop(sctx, st)
}

// connectLoop 维护一个 server 的 ssh 连接：失败按 backoff 重试、断开后自动重连、
// 累计断开超过 Degraded 阈值时上报 degraded。
func (e *Engine) connectLoop(ctx context.Context, st *serverState) {
	defer e.wg.Done()
	backoff := e.deps.Backoff
	if backoff.Initial == 0 {
		backoff = sshpool.DefaultBackoff()
	}
	sleep := e.deps.Sleep
	if sleep == nil {
		sleep = defaultSleep
	}
	now := e.deps.Now

	attempt := 0
	var downSince time.Time // 零值表示当前在线（或刚启动）

	for {
		if ctx.Err() != nil {
			return
		}
		e.emitServerStatus(ctx, st.cfg.ID, "dialing", attempt, 0, "")
		err := st.client.Connect(ctx)
		if err == nil {
			attempt = 0
			downSince = time.Time{}
			e.emitServerStatus(ctx, st.cfg.ID, "connected", 0, 0, "")
			select {
			case <-ctx.Done():
				return
			case <-st.client.Done():
				downSince = now()
				e.emitServerStatus(ctx, st.cfg.ID, "broken", 0, 0, "connection lost")
				continue
			}
		}

		if downSince.IsZero() {
			downSince = now()
		}
		sinceDown := now().Sub(downSince)
		state := "broken"
		if sshpool.IsDegraded(backoff, sinceDown) {
			state = "degraded"
		}
		e.emitServerStatus(ctx, st.cfg.ID, state, attempt, sinceDown.Milliseconds(), err.Error())

		delay := sshpool.NextDelay(backoff, attempt)
		attempt++
		if err := sleep(ctx, delay); err != nil {
			return
		}
	}
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// emitServerStatus 发出 EventServerStatus 事件。
func (e *Engine) emitServerStatus(ctx context.Context, id, state string, attempt int, disconnectedMs int64, errMsg string) {
	if e.emit == nil {
		return
	}
	e.emit.Emit(ctx, events.EventServerStatus, events.ServerStatus{
		ServerID:       id,
		State:          state,
		Attempt:        attempt,
		DisconnectedMs: disconnectedMs,
		Error:          errMsg,
	})
}

// scheduleLoop 是定时扫描调度器：tick 或外部 trigger 触发 scanTick。
func (e *Engine) scheduleLoop(ctx context.Context) {
	defer e.wg.Done()
	interval := time.Duration(e.cfg.ScanIntervalSec) * time.Second
	if interval <= 0 {
		interval = 15 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = e.scanTick(ctx)
		case req := <-e.triggerCh:
			err := e.scanTick(ctx)
			req.done <- err
		}
	}
}

// StopAll 停止扫描循环、关闭所有 forward 监听、关闭 ssh client。
func (e *Engine) StopAll() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = false
	cancel := e.cancel
	e.startCtx = nil
	servers := e.servers
	e.servers = map[string]*serverState{}
	e.mu.Unlock()

	cancel()
	e.wg.Wait()

	for _, st := range servers {
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

	e.snapshotMu.Lock()
	e.planned = nil
	e.snapshotMu.Unlock()
	return nil
}

// ScanNow 触发一次同步扫描，等待执行完成。
func (e *Engine) ScanNow(ctx context.Context) error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return ErrNotRunning
	}
	ch := e.triggerCh
	e.mu.Unlock()
	req := scanRequest{done: make(chan error, 1)}
	select {
	case ch <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Snapshot 返回当前所有 forward 的状态快照（深拷贝，调用方可安全修改）。
// 在 Reconcile 的「计划状态」之上叠加 forwardHandle 的实时状态（forwarding/error）。
func (e *Engine) Snapshot() []model.Forward {
	e.snapshotMu.RLock()
	planned := make([]model.Forward, len(e.planned))
	copy(planned, e.planned)
	e.snapshotMu.RUnlock()
	if len(planned) == 0 {
		return nil
	}
	// 按 serverID 分组并叠加实时状态。
	e.mu.Lock()
	srvSnapshot := make(map[string]*serverState, len(e.servers))
	for k, v := range e.servers {
		srvSnapshot[k] = v
	}
	e.mu.Unlock()
	for i := range planned {
		st, ok := srvSnapshot[planned[i].ServerID]
		if !ok {
			continue
		}
		st.mu.Lock()
		h := st.forwards[planned[i].RemotePort]
		st.mu.Unlock()
		if h == nil {
			continue
		}
		h.mu.Lock()
		if h.status != "" {
			planned[i].Status = h.status
		}
		if h.errMsg != "" {
			planned[i].Error = h.errMsg
		}
		planned[i].LastActive = h.updated.Load()
		h.mu.Unlock()
	}
	return planned
}

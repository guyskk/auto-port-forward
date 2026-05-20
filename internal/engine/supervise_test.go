package engine

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"autoportforward/internal/config"
	"autoportforward/internal/events"
	"autoportforward/internal/model"
	"autoportforward/internal/sshpool"
)

// 测试: 连接成功 → 上报 dialing → connected；attempt=0；DisconnectedMs=0。
func TestSupervise_connectSuccessEmitsConnected(t *testing.T) {
	fc := newReconnectFakeClient()
	spy := newEmitSpy()
	eng := newSuperviseEngine(t, fc, spy, sshpool.DefaultBackoff(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	spy.waitForState(t, time.Second, "s1", "connected")
	st := spy.lastStatus("s1")
	if st.Attempt != 0 {
		t.Errorf("connected Attempt = %d, want 0", st.Attempt)
	}
	if st.DisconnectedMs != 0 {
		t.Errorf("connected DisconnectedMs = %d, want 0", st.DisconnectedMs)
	}
}

// 测试: 首次 Connect 失败 → 上报 broken（含错误）→ backoff 后重试 → 第二次成功 → connected。
func TestSupervise_retryAfterFailure(t *testing.T) {
	fc := newReconnectFakeClient()
	fc.queueConnect(errors.New("boom"))
	fc.queueConnect(nil)

	spy := newEmitSpy()
	sleeps := newSleepRecorder()
	eng := newSuperviseEngine(t, fc, spy, sshpool.BackoffParams{
		Initial: 500 * time.Millisecond, Max: 60 * time.Second, Degraded: 15 * time.Minute,
	}, sleeps.sleep)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	spy.waitForState(t, time.Second, "s1", "broken")
	st := spy.lastStateOf("s1", "broken")
	if st.Error != "boom" {
		t.Errorf("broken Error = %q, want %q", st.Error, "boom")
	}

	spy.waitForState(t, time.Second, "s1", "connected")
	if atomic.LoadInt32(&fc.connectCount) < 2 {
		t.Errorf("connectCount = %d, want >= 2", fc.connectCount)
	}
	// sleeps[0] 应为 Initial（attempt=0）
	if got := sleeps.delayAt(0); got != 500*time.Millisecond {
		t.Errorf("first backoff = %v, want 500ms", got)
	}
}

// 测试: 连接断开（Done 关闭）后 connectLoop 自动重连。
func TestSupervise_reconnectAfterDisconnect(t *testing.T) {
	fc := newReconnectFakeClient()
	spy := newEmitSpy()
	eng := newSuperviseEngine(t, fc, spy, sshpool.DefaultBackoff(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	spy.waitForState(t, time.Second, "s1", "connected")

	// 模拟连接断开。
	fc.signalDone()
	// 等待 reconnect。
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&fc.connectCount) >= 2 }, "second connect")
	spy.waitForCount(t, time.Second, "s1", "connected", 2)
}

// 测试: 累计断开时间超过 Degraded 阈值后状态上报为 degraded。
func TestSupervise_degradedAfterLongDisconnect(t *testing.T) {
	fc := newReconnectFakeClient()
	// 始终失败 — 这样 sinceDown 会持续增长。
	for i := 0; i < 200; i++ {
		fc.queueConnect(errors.New("net down"))
	}

	spy := newEmitSpy()
	// 把 Degraded 设得很短 — 50ms，便于在测试时长内推进到 degraded。
	backoff := sshpool.BackoffParams{
		Initial:  1 * time.Millisecond,
		Max:      5 * time.Millisecond,
		Degraded: 50 * time.Millisecond,
	}
	// 不注入 Sleep — 让真实 time.Sleep 跑 1~5ms，几次循环后 sinceDown > 50ms。
	eng := newSuperviseEngine(t, fc, spy, backoff, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	spy.waitForState(t, 2*time.Second, "s1", "degraded")
	st := spy.lastStateOf("s1", "degraded")
	if st.DisconnectedMs < 50 {
		t.Errorf("degraded DisconnectedMs = %d, want >= 50", st.DisconnectedMs)
	}
}

// ===== helpers =====

// reconnectFakeClient: 比 fakeClient 更细的版本，支持队列化 Connect 结果 + Done 信号。
type reconnectFakeClient struct {
	mu           sync.Mutex
	connectQueue []error // 按顺序消费；空了后默认返回 nil（成功）
	doneCh       chan struct{}
	connectCount int32
	closeCount   int32
}

func newReconnectFakeClient() *reconnectFakeClient {
	return &reconnectFakeClient{}
}

func (f *reconnectFakeClient) queueConnect(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectQueue = append(f.connectQueue, err)
}

func (f *reconnectFakeClient) Connect(ctx context.Context) error {
	atomic.AddInt32(&f.connectCount, 1)
	f.mu.Lock()
	var err error
	if len(f.connectQueue) > 0 {
		err = f.connectQueue[0]
		f.connectQueue = f.connectQueue[1:]
	}
	if err == nil {
		f.doneCh = make(chan struct{})
	}
	f.mu.Unlock()
	return err
}

func (f *reconnectFakeClient) Close() error {
	atomic.AddInt32(&f.closeCount, 1)
	f.signalDone()
	return nil
}

func (f *reconnectFakeClient) Run(ctx context.Context, cmd string) ([]byte, error) {
	return nil, nil
}

func (f *reconnectFakeClient) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return nil, errors.New("not implemented")
}

func (f *reconnectFakeClient) Done() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.doneCh
}

func (f *reconnectFakeClient) signalDone() {
	f.mu.Lock()
	if f.doneCh != nil {
		close(f.doneCh)
		f.doneCh = nil
	}
	f.mu.Unlock()
}

// emitSpy 收集所有 ServerStatus 事件，可按 server / state 查询。
type emitSpy struct {
	mu       sync.Mutex
	events   []events.ServerStatus
	signal   chan struct{}
}

func newEmitSpy() *emitSpy {
	return &emitSpy{signal: make(chan struct{}, 64)}
}

func (s *emitSpy) Emit(ctx context.Context, name string, data any) {
	if name != events.EventServerStatus {
		return
	}
	st, ok := data.(events.ServerStatus)
	if !ok {
		return
	}
	s.mu.Lock()
	s.events = append(s.events, st)
	s.mu.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

func (s *emitSpy) snapshot() []events.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]events.ServerStatus, len(s.events))
	copy(cp, s.events)
	return cp
}

func (s *emitSpy) lastStatus(serverID string) events.ServerStatus {
	all := s.snapshot()
	var last events.ServerStatus
	for _, e := range all {
		if e.ServerID == serverID {
			last = e
		}
	}
	return last
}

func (s *emitSpy) lastStateOf(serverID, state string) events.ServerStatus {
	all := s.snapshot()
	var last events.ServerStatus
	for _, e := range all {
		if e.ServerID == serverID && e.State == state {
			last = e
		}
	}
	return last
}

func (s *emitSpy) countState(serverID, state string) int {
	all := s.snapshot()
	n := 0
	for _, e := range all {
		if e.ServerID == serverID && e.State == state {
			n++
		}
	}
	return n
}

func (s *emitSpy) waitForState(t *testing.T, d time.Duration, serverID, state string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		all := s.snapshot()
		for _, e := range all {
			if e.ServerID == serverID && e.State == state {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitForState timeout: server=%s state=%s, got events: %+v", serverID, state, s.snapshot())
}

func (s *emitSpy) waitForCount(t *testing.T, d time.Duration, serverID, state string, want int) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if s.countState(serverID, state) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitForCount timeout: server=%s state=%s want>=%d got=%d", serverID, state, want, s.countState(serverID, state))
}

// sleepRecorder 记录所有 backoff Sleep 调用并立即返回（不真等待）。
type sleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func newSleepRecorder() *sleepRecorder { return &sleepRecorder{} }

func (r *sleepRecorder) sleep(ctx context.Context, d time.Duration) error {
	r.mu.Lock()
	r.delays = append(r.delays, d)
	r.mu.Unlock()
	return ctx.Err()
}

func (r *sleepRecorder) delayAt(i int) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i < 0 || i >= len(r.delays) {
		return -1
	}
	return r.delays[i]
}

// newSuperviseEngine 构造一个注入 supervise 相关 deps 的 Engine。
func newSuperviseEngine(
	t *testing.T,
	fc *reconnectFakeClient,
	emit events.Emitter,
	backoff sshpool.BackoffParams,
	sleep func(ctx context.Context, d time.Duration) error,
) *Engine {
	t.Helper()
	cfg := config.Config{
		ScanIntervalSec: 3600,
		Servers:         []config.Server{{ID: "s1", Host: "h", Enabled: true}},
	}
	return New(cfg, emit, Deps{
		ClientFactory: func(s config.Server) ClientHandle { return fc },
		LocalScan:     func(ctx context.Context) ([]model.LocalPort, error) { return nil, nil },
		IsRoot:        true,
		Backoff:       backoff,
		Sleep:         sleep,
	})
}

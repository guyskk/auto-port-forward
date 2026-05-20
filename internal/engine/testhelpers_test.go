package engine

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"autoportforward/internal/config"
	"autoportforward/internal/events"
	"autoportforward/internal/model"
)

// fakeClient 实现 ClientHandle，可控产出。
type fakeClient struct {
	mu           sync.Mutex
	connectCount int32
	closed       int32
	dialCount    int32
	runOutput    []byte
	runError     error
	connectError error
	dialError    error
}

func (f *fakeClient) Connect(ctx context.Context) error {
	atomic.AddInt32(&f.connectCount, 1)
	return f.connectError
}

func (f *fakeClient) Close() error {
	atomic.StoreInt32(&f.closed, 1)
	return nil
}

func (f *fakeClient) Run(ctx context.Context, cmd string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runError != nil {
		return nil, f.runError
	}
	return f.runOutput, nil
}

func (f *fakeClient) Dial(ctx context.Context, addr string) (net.Conn, error) {
	atomic.AddInt32(&f.dialCount, 1)
	if f.dialError != nil {
		return nil, f.dialError
	}
	// 用 net.Pipe 模拟远端 socket — 对端立即 discard。
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		_, _ = io.Copy(io.Discard, b)
	}()
	return a, nil
}

// Done 返回 nil channel — 永久阻塞，模拟"连接保持"。
// 测试不关心断开重连时直接用 fakeClient；需要触发断开请用 reconnectFakeClient。
func (f *fakeClient) Done() <-chan struct{} { return nil }

// reservePort 占住一个端口然后立刻释放，返回端口号。
// 有理论竞态，仅测试用：随后另一进程占住该端口将让测试失败。
func reservePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

// waitFor 反复检查 cond 直到为 true 或超时。
func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitFor timeout: %s", msg)
}

// newEngineWith 构造一个注入 fakeClient 的 Engine，默认 IsRoot=true，禁用自动 tick。
func newEngineWith(t *testing.T, fc *fakeClient, cfg config.Config) *Engine {
	t.Helper()
	if cfg.ScanIntervalSec == 0 {
		cfg.ScanIntervalSec = 3600
	}
	return New(cfg, events.NopEmitter{}, Deps{
		ClientFactory: func(s config.Server) ClientHandle { return fc },
		LocalScan:     func(ctx context.Context) ([]model.LocalPort, error) { return nil, nil },
		IsRoot:        true,
	})
}

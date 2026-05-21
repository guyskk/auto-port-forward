package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/events"
	"auto-port-forward/internal/model"
	"auto-port-forward/internal/sshcfg"
)

// 公共错误（供测试使用）
var errBindFailed = errors.New("bind: address already in use")

// fakeClient 实现 ClientHandle，可控产出。
type fakeClient struct {
	mu                 sync.Mutex
	connectCount       int32
	closed             int32
	addForwardCount    int32
	cancelForwardCount int32
	addedPorts         []int
	cancelledPorts     []int
	runOutput          []byte
	runError           error
	connectError       error
	addForwardError    error
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

func (f *fakeClient) AddForward(ctx context.Context, port int) error {
	atomic.AddInt32(&f.addForwardCount, 1)
	f.mu.Lock()
	f.addedPorts = append(f.addedPorts, port)
	err := f.addForwardError
	f.mu.Unlock()
	return err
}

func (f *fakeClient) CancelForward(ctx context.Context, port int) error {
	atomic.AddInt32(&f.cancelForwardCount, 1)
	f.mu.Lock()
	f.cancelledPorts = append(f.cancelledPorts, port)
	f.mu.Unlock()
	return nil
}

// Done 返回 nil channel — 永久阻塞，模拟"连接保持"。
// 测试不关心断开重连时直接用 fakeClient；需要触发断开请用 reconnectFakeClient。
func (f *fakeClient) Done() <-chan struct{} { return nil }

func (f *fakeClient) snapshotAddedPorts() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]int(nil), f.addedPorts...)
	return cp
}

func (f *fakeClient) snapshotCancelledPorts() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]int(nil), f.cancelledPorts...)
	return cp
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

// newEngineWithHosts 构造一个注入 fakeClient 的 Engine，默认 IsRoot=true，禁用自动 tick。
// 在 StartAll 之前用 ApplyServers 注入 enabled hosts。
func newEngineWithHosts(t *testing.T, fc *fakeClient, cfg config.Config, hosts []sshcfg.Host) *Engine {
	t.Helper()
	if cfg.ScanIntervalSec == 0 {
		cfg.ScanIntervalSec = 3600
	}
	e := New(cfg, events.NopEmitter{}, Deps{
		ClientFactory: func(h sshcfg.Host) ClientHandle { return fc },
		LocalScan:     func(ctx context.Context) ([]model.LocalPort, error) { return nil, nil },
		IsRoot:        true,
	})
	if len(hosts) > 0 {
		_ = e.ApplyServers(hosts)
	}
	return e
}

// stubHost: 便于在测试里构造启用的 host。
func stubHost(alias string) sshcfg.Host {
	return sshcfg.Host{Alias: alias, HostName: "h", User: "u", Port: 22}
}

// 哨兵：确保 fakeClient 满足 ClientHandle 接口。
var _ ClientHandle = (*fakeClient)(nil)

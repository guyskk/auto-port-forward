package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/events"
	"auto-port-forward/internal/model"
)

// 测试: ApplyServers 在运行时添加新 server，启动它的 Connect。
func TestEngine_ApplyServers_addNewServer(t *testing.T) {
	created := newClientRegistry()
	eng := New(config.Config{ScanIntervalSec: 3600}, events.NopEmitter{}, Deps{
		ClientFactory: created.factory,
		LocalScan:     func(ctx context.Context) ([]model.LocalPort, error) { return nil, nil },
		IsRoot:        true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	// 起初无 server。
	if got := created.count(); got != 0 {
		t.Fatalf("initial client count = %d, want 0", got)
	}

	// 热插拔加入一个 enabled server。
	if err := eng.ApplyServers([]config.Server{{ID: "s1", Host: "h", Enabled: true}}); err != nil {
		t.Fatalf("ApplyServers: %v", err)
	}
	waitFor(t, time.Second, func() bool { return created.count() == 1 }, "client factory called once")
	c := created.byID("s1")
	if c == nil {
		t.Fatal("no client for s1")
	}
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&c.connectCount) >= 1 }, "Connect called for new server")
}

// 测试: ApplyServers 移除一个 server → 它的 client 被 Close。
func TestEngine_ApplyServers_removeServer(t *testing.T) {
	created := newClientRegistry()
	eng := New(config.Config{
		ScanIntervalSec: 3600,
		Servers:         []config.Server{{ID: "s1", Host: "h", Enabled: true}},
	}, events.NopEmitter{}, Deps{
		ClientFactory: created.factory,
		LocalScan:     func(ctx context.Context) ([]model.LocalPort, error) { return nil, nil },
		IsRoot:        true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()
	waitFor(t, time.Second, func() bool { return created.count() >= 1 }, "initial Connect")
	c := created.byID("s1")

	// 移除 s1。
	if err := eng.ApplyServers(nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&c.closed) == 1 }, "removed client closed")
}

// 测试: ApplyServers 替换 server 配置（host 改变）→ 旧 client Close、新 client Connect。
func TestEngine_ApplyServers_replaceConfig(t *testing.T) {
	created := newClientRegistry()
	eng := New(config.Config{
		ScanIntervalSec: 3600,
		Servers:         []config.Server{{ID: "s1", Host: "old", Enabled: true}},
	}, events.NopEmitter{}, Deps{
		ClientFactory: created.factory,
		LocalScan:     func(ctx context.Context) ([]model.LocalPort, error) { return nil, nil },
		IsRoot:        true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()
	waitFor(t, time.Second, func() bool { return created.count() >= 1 }, "initial client")
	oldClient := created.byID("s1")

	// host 改变 → 触发重建。
	if err := eng.ApplyServers([]config.Server{{ID: "s1", Host: "new", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&oldClient.closed) == 1 }, "old client closed")
	waitFor(t, time.Second, func() bool { return created.count() == 2 }, "second factory call")
}

// 测试: ApplyServers 仅在 Enabled=false → true 转换时创建 client。
func TestEngine_ApplyServers_disabledNotConnected(t *testing.T) {
	created := newClientRegistry()
	eng := New(config.Config{ScanIntervalSec: 3600}, events.NopEmitter{}, Deps{
		ClientFactory: created.factory,
		LocalScan:     func(ctx context.Context) ([]model.LocalPort, error) { return nil, nil },
		IsRoot:        true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	if err := eng.ApplyServers([]config.Server{{ID: "s1", Host: "h", Enabled: false}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := created.count(); got != 0 {
		t.Errorf("disabled server should not create client, got count=%d", got)
	}
}

// clientRegistry 收集 ClientFactory 被创建的所有 fakeClient，按 server ID 索引。
type clientRegistry struct {
	mu      sync.Mutex
	clients map[string]*fakeClient
	all     []*fakeClient
}

func newClientRegistry() *clientRegistry {
	return &clientRegistry{clients: map[string]*fakeClient{}}
}

func (r *clientRegistry) factory(s config.Server) ClientHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	fc := &fakeClient{}
	r.clients[s.ID] = fc
	r.all = append(r.all, fc)
	return fc
}

func (r *clientRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.all)
}

func (r *clientRegistry) byID(id string) *fakeClient {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.clients[id]
}

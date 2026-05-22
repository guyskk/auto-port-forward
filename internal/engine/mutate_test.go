package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/model"
	"auto-port-forward/internal/sshcfg"
)

// ToggleForward(on=false) 之后下次扫描该端口不应被启动；
// 如果当前已经在跑，应被 CancelForward。
func TestEngine_ToggleForward_disabledPortGetsCancelled(t *testing.T) {
	remotePort := 19710
	ssOut := fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort)
	fc := &fakeClient{runOutput: []byte(ssOut)}
	eng := newEngineWithHosts(t, fc, config.Config{}, []sshcfg.Host{stubHost("s1")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	if err := eng.ScanNow(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&fc.addForwardCount) >= 1
	}, "first scan adds forward")

	// 禁用该端口 —— engine 内部会触发 ScanNow，应当调 CancelForward。
	if err := eng.ToggleForward("s1", remotePort, false); err != nil {
		t.Fatalf("ToggleForward: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&fc.cancelForwardCount) >= 1
	}, "ToggleForward off triggers cancel")

	// Snapshot 上该端口应显示 excluded。
	waitFor(t, 2*time.Second, func() bool {
		for _, f := range eng.Snapshot() {
			if f.RemotePort == remotePort && f.Status == model.StatusExcluded {
				return true
			}
		}
		return false
	}, "port becomes excluded")
}

// ToggleForward(on=true) 把端口从禁用列表移除，下次扫描应被重新启动。
func TestEngine_ToggleForward_reenablesPort(t *testing.T) {
	remotePort := 19711
	ssOut := fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort)
	fc := &fakeClient{runOutput: []byte(ssOut)}
	eng := newEngineWithHosts(t, fc, config.Config{}, []sshcfg.Host{stubHost("s1")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	// 先禁用再启用
	if err := eng.ToggleForward("s1", remotePort, false); err != nil {
		t.Fatal(err)
	}
	if err := eng.ScanNow(ctx); err != nil {
		t.Fatal(err)
	}
	// 等 reconcile 跑完一轮，addForward 不应被调用。
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&fc.addForwardCount) != 0 {
		t.Errorf("addForward called %d times for disabled port", fc.addForwardCount)
	}

	if err := eng.ToggleForward("s1", remotePort, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&fc.addForwardCount) >= 1
	}, "re-enable triggers add")
}

// ToggleForward 在 engine 未运行时只更新内部状态，不应崩溃也不应触发扫描。
func TestEngine_ToggleForward_beforeStartIsSafe(t *testing.T) {
	fc := &fakeClient{}
	eng := newEngineWithHosts(t, fc, config.Config{}, []sshcfg.Host{stubHost("s1")})
	if err := eng.ToggleForward("s1", 8080, false); err != nil {
		t.Fatalf("ToggleForward before StartAll: %v", err)
	}
	if got := atomic.LoadInt32(&fc.connectCount); got != 0 {
		t.Errorf("connectCount = %d, want 0 before StartAll", got)
	}
}

// ToggleForward 与 NewWithDisabledPorts 串联：初始 cfg.DisabledPorts 有效。
func TestEngine_ScanWithInitialDisabledPorts(t *testing.T) {
	remotePort := 19712
	ssOut := fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort)
	fc := &fakeClient{runOutput: []byte(ssOut)}

	cfg := config.Config{
		DisabledPorts: map[string][]int{"s1": {remotePort}},
	}
	eng := newEngineWithHosts(t, fc, cfg, []sshcfg.Host{stubHost("s1")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()

	if err := eng.ScanNow(ctx); err != nil {
		t.Fatal(err)
	}
	// 启动时端口已被禁用 → 永远不会被 AddForward。
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&fc.addForwardCount); got != 0 {
		t.Errorf("addForward called %d times for initial-disabled port", got)
	}
	// 但 snapshot 应当包含该端口（status=excluded）。
	found := false
	for _, f := range eng.Snapshot() {
		if f.RemotePort == remotePort && f.Status == model.StatusExcluded {
			found = true
		}
	}
	if !found {
		t.Errorf("snapshot missing disabled port as excluded: %#v", eng.Snapshot())
	}
}

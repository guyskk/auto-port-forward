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

// 测试 1: StartAll 连接所有 enabled host。
func TestEngine_StartAll_connectsEnabledHosts(t *testing.T) {
	fc := &fakeClient{}
	eng := newEngineWithHosts(t, fc, config.Config{}, []sshcfg.Host{stubHost("s1")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&fc.connectCount) >= 1 }, "Connect called")
}

// 测试 2: 没有任何启用 host 时不连接。
func TestEngine_StartAll_skipsWhenNoHosts(t *testing.T) {
	fc := &fakeClient{}
	eng := newEngineWithHosts(t, fc, config.Config{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&fc.connectCount) != 0 {
		t.Errorf("connectCount=%d, want 0", fc.connectCount)
	}
}

// 测试 3: ScanNow 发现远端端口 → 调 AddForward → 状态变 forwarding。
func TestEngine_ScanNow_addsForwardForNewPort(t *testing.T) {
	remotePort := 19527
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
		for _, f := range eng.Snapshot() {
			if f.RemotePort == remotePort && f.Status == model.StatusForwarding {
				return true
			}
		}
		return false
	}, "forward becomes forwarding")

	if atomic.LoadInt32(&fc.addForwardCount) < 1 {
		t.Errorf("AddForward not called: count=%d", fc.addForwardCount)
	}
	ports := fc.snapshotAddedPorts()
	if len(ports) == 0 || ports[0] != remotePort {
		t.Errorf("AddForward ports = %v, want first %d", ports, remotePort)
	}
}

// 测试 4: 远端端口消失 → 下次 scan 调 CancelForward。
func TestEngine_ScanNow_cancelsForwardWhenPortGone(t *testing.T) {
	remotePort := 19528

	fc := &fakeClient{
		runOutput: []byte(fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort)),
	}
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

	fc.mu.Lock()
	fc.runOutput = []byte("")
	fc.mu.Unlock()
	if err := eng.ScanNow(ctx); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&fc.cancelForwardCount) >= 1
	}, "second scan cancels forward")
	cancelled := fc.snapshotCancelledPorts()
	if len(cancelled) == 0 || cancelled[0] != remotePort {
		t.Errorf("Cancel ports = %v, want first %d", cancelled, remotePort)
	}
}

// 测试 5: StopAll 关闭所有 client。
func TestEngine_StopAll_closesClients(t *testing.T) {
	remotePort := 19529
	fc := &fakeClient{runOutput: []byte(fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort))}
	eng := newEngineWithHosts(t, fc, config.Config{}, []sshcfg.Host{stubHost("s1")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := eng.ScanNow(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&fc.addForwardCount) >= 1
	}, "forward added")

	if err := eng.StopAll(); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&fc.closed) != 1 {
		t.Errorf("fakeClient.Close not called")
	}
}

// 测试 6: ScanNow 在 StartAll 之前调用应返回 ErrNotRunning。
func TestEngine_ScanNow_returnsErrorWhenNotRunning(t *testing.T) {
	fc := &fakeClient{}
	eng := newEngineWithHosts(t, fc, config.Config{}, []sshcfg.Host{stubHost("s1")})
	if err := eng.ScanNow(context.Background()); err == nil {
		t.Errorf("expected error when not running")
	}
}

// 测试 7: Snapshot 返回深拷贝，调用方修改不影响内部状态。
func TestEngine_Snapshot_returnsCopy(t *testing.T) {
	remotePort := 19530
	fc := &fakeClient{runOutput: []byte(fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort))}
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
	waitFor(t, 2*time.Second, func() bool { return len(eng.Snapshot()) > 0 }, "snapshot populated")

	s1 := eng.Snapshot()
	if len(s1) == 0 {
		t.Fatal("empty snapshot")
	}
	s1[0].Status = "tampered"
	s2 := eng.Snapshot()
	if s2[0].Status == "tampered" {
		t.Errorf("snapshot is not a copy: caller mutation leaked")
	}
}

// 测试 8: AddForward 失败时端口状态变 conflict，错误消息透传。
func TestEngine_ScanNow_addForwardErrorBecomesConflict(t *testing.T) {
	remotePort := 19531
	fc := &fakeClient{
		runOutput:       []byte(fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort)),
		addForwardError: errBindFailed,
	}
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
		for _, f := range eng.Snapshot() {
			if f.RemotePort == remotePort && f.Status == model.StatusConflict {
				return true
			}
		}
		return false
	}, "AddForward failure becomes conflict")
}

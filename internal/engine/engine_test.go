package engine

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/model"
)

// 测试 1: StartAll 连接所有 enabled server。
func TestEngine_StartAll_connectsEnabledServers(t *testing.T) {
	fc := &fakeClient{}
	eng := newEngineWith(t, fc, config.Config{
		Servers: []config.Server{{ID: "s1", Host: "h", Enabled: true}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	defer eng.StopAll()
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&fc.connectCount) >= 1 }, "Connect called")
}

// 测试 2: 禁用 server 不连接。
func TestEngine_StartAll_skipsDisabledServers(t *testing.T) {
	fc := &fakeClient{}
	eng := newEngineWith(t, fc, config.Config{
		Servers: []config.Server{{ID: "s1", Host: "h", Enabled: false}},
	})
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

// 测试 3: ScanNow 发现远端端口并启动 forward。
func TestEngine_ScanNow_startsForwardForNewPort(t *testing.T) {
	remotePort := 19527
	localPort := reservePort(t)
	offset := localPort - remotePort

	ssOut := fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort)
	fc := &fakeClient{runOutput: []byte(ssOut)}

	eng := newEngineWith(t, fc, config.Config{
		Servers: []config.Server{{ID: "s1", Host: "h", Enabled: true}},
		Rules:   config.Rules{LocalPortOffset: offset},
	})
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

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		t.Fatalf("dial local forward: %v", err)
	}
	conn.Close()

	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&fc.dialCount) >= 1 }, "fakeClient.Dial called")
}

// 测试 4: 远端端口消失 → 下次 scan 删除 forward。
func TestEngine_ScanNow_removesForwardWhenPortGone(t *testing.T) {
	remotePort := 19528
	localPort := reservePort(t)
	offset := localPort - remotePort

	fc := &fakeClient{
		runOutput: []byte(fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort)),
	}
	eng := newEngineWith(t, fc, config.Config{
		Servers: []config.Server{{ID: "s1", Host: "h", Enabled: true}},
		Rules:   config.Rules{LocalPortOffset: offset},
	})
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
			if f.Status == model.StatusForwarding {
				return true
			}
		}
		return false
	}, "first scan starts forward")

	fc.mu.Lock()
	fc.runOutput = []byte("")
	fc.mu.Unlock()
	if err := eng.ScanNow(ctx); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 2*time.Second, func() bool {
		_, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 200*time.Millisecond)
		return err != nil
	}, "local listener closes after port gone")
}

// 测试 5: StopAll 关闭所有 client + listener。
func TestEngine_StopAll_closesClients(t *testing.T) {
	remotePort := 19529
	localPort := reservePort(t)
	offset := localPort - remotePort

	fc := &fakeClient{runOutput: []byte(fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort))}
	eng := newEngineWith(t, fc, config.Config{
		Servers: []config.Server{{ID: "s1", Host: "h", Enabled: true}},
		Rules:   config.Rules{LocalPortOffset: offset},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := eng.ScanNow(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		for _, f := range eng.Snapshot() {
			if f.Status == model.StatusForwarding {
				return true
			}
		}
		return false
	}, "forward started")

	if err := eng.StopAll(); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&fc.closed) != 1 {
		t.Errorf("fakeClient.Close not called")
	}
	waitFor(t, time.Second, func() bool {
		_, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 200*time.Millisecond)
		return err != nil
	}, "local listener closes after StopAll")
}

// 测试 6: ScanNow 在 StartAll 之前调用应返回 ErrNotRunning。
func TestEngine_ScanNow_returnsErrorWhenNotRunning(t *testing.T) {
	fc := &fakeClient{}
	eng := newEngineWith(t, fc, config.Config{
		Servers: []config.Server{{ID: "s1", Host: "h", Enabled: true}},
	})
	if err := eng.ScanNow(context.Background()); err == nil {
		t.Errorf("expected error when not running")
	}
}

// 测试 7: Snapshot 返回深拷贝，调用方修改不影响内部状态。
func TestEngine_Snapshot_returnsCopy(t *testing.T) {
	remotePort := 19530
	localPort := reservePort(t)
	offset := localPort - remotePort

	fc := &fakeClient{runOutput: []byte(fmt.Sprintf("LISTEN 0 128 0.0.0.0:%d 0.0.0.0:*\n", remotePort))}
	eng := newEngineWith(t, fc, config.Config{
		Servers: []config.Server{{ID: "s1", Host: "h", Enabled: true}},
		Rules:   config.Rules{LocalPortOffset: offset},
	})
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

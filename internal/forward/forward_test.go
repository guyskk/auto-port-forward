package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockDialer 把 Dial 重定向到一个 server goroutine，模拟 SSH 远端的本机服务。
type mockDialer struct {
	dialCount int32
	// remoteHandler 收到一条新连接时被调用，模拟远端服务行为。
	remoteHandler func(c net.Conn)
	failWith      error
}

func (m *mockDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	atomic.AddInt32(&m.dialCount, 1)
	if m.failWith != nil {
		return nil, m.failWith
	}
	local, remote := net.Pipe()
	if m.remoteHandler != nil {
		go m.remoteHandler(remote)
	} else {
		go func() { _, _ = io.Copy(remote, remote) }() // echo
	}
	_ = ctx
	_ = addr
	return local, nil
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestForward_listensAndProxies(t *testing.T) {
	localPort := freePort(t)
	d := &mockDialer{
		remoteHandler: func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 1024)
			n, err := c.Read(buf)
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("echo:" + string(buf[:n])))
		},
	}
	f := &Forward{RemotePort: 9527, LocalPort: localPort, Bind: "127.0.0.1"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- f.Run(ctx, d) }()

	// 等 Listen 起来。
	conn := dialWithRetry(t, "127.0.0.1", localPort, 2*time.Second)
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:n]) != "echo:ping" {
		t.Errorf("got %q, want echo:ping", string(buf[:n]))
	}
	if atomic.LoadInt32(&d.dialCount) != 1 {
		t.Errorf("dialCount = %d, want 1", d.dialCount)
	}
	cancel()
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestForward_reportsConflictWhenListenFails(t *testing.T) {
	// 先占住端口。
	port := freePort(t)
	hold, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()

	var gotStatus string
	var gotErr error
	f := &Forward{
		RemotePort: 9527,
		LocalPort:  port,
		Bind:       "127.0.0.1",
		Report: func(status string, err error) {
			gotStatus = status
			gotErr = err
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.Run(ctx, &mockDialer{}); err == nil {
		t.Errorf("Run should return error when listen fails")
	}
	if gotStatus != "conflict" {
		t.Errorf("status = %q, want conflict", gotStatus)
	}
	if gotErr == nil {
		t.Errorf("report err is nil")
	}
}

func TestForward_dialFailureClosesClient(t *testing.T) {
	localPort := freePort(t)
	d := &mockDialer{failWith: errors.New("dial fail")}
	var (
		mu        sync.Mutex
		gotStatus string
	)
	setStatus := func(s string) { mu.Lock(); gotStatus = s; mu.Unlock() }
	readStatus := func() string { mu.Lock(); defer mu.Unlock(); return gotStatus }
	f := &Forward{
		RemotePort: 9527,
		LocalPort:  localPort,
		Bind:       "127.0.0.1",
		Report:     func(status string, err error) { setStatus(status) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx, d)

	conn := dialWithRetry(t, "127.0.0.1", localPort, 2*time.Second)
	defer conn.Close()
	// dialer 失败 → forward 应关闭本端连接。
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1)
	_, err := conn.Read(buf)
	if err == nil {
		t.Errorf("expected EOF/err on local conn when remote dial fails")
	}
	// 等 Report("error") 到达（与本 goroutine 异步）。
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if readStatus() == "error" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if readStatus() != "error" {
		t.Errorf("status = %q, want error", readStatus())
	}
}

func dialWithRetry(t *testing.T, host string, port int, timeout time.Duration) net.Conn {
	t.Helper()
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			return c
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dial %s timeout", addr)
	return nil
}

package sshctl

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"auto-port-forward/internal/sshcfg"
)

// --- fake Runner + Process ---

type fakeProc struct {
	mu      sync.Mutex
	waitCh  chan error
	killed  bool
	exited  bool
	killErr error
}

func newFakeProc() *fakeProc { return &fakeProc{waitCh: make(chan error, 1)} }

func (p *fakeProc) Wait() <-chan error { return p.waitCh }

func (p *fakeProc) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killed = true
	if !p.exited {
		p.exited = true
		p.waitCh <- errors.New("killed")
	}
	return p.killErr
}

// exit lets a test drive the master's exit (with an optional error).
func (p *fakeProc) exit(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.exited {
		p.exited = true
		p.waitCh <- err
	}
}

type fakeCall struct {
	kind string // "run" | "start"
	name string
	args []string
}

type fakeRunner struct {
	mu      sync.Mutex
	calls   []fakeCall
	onRun   func(ctx context.Context, name string, args []string) ([]byte, error)
	onStart func(ctx context.Context, name string, args []string) (Process, error)
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	cp := append([]string(nil), args...)
	f.calls = append(f.calls, fakeCall{kind: "run", name: name, args: cp})
	f.mu.Unlock()
	if f.onRun == nil {
		return nil, nil
	}
	return f.onRun(ctx, name, cp)
}

func (f *fakeRunner) Start(ctx context.Context, name string, args ...string) (Process, error) {
	f.mu.Lock()
	cp := append([]string(nil), args...)
	f.calls = append(f.calls, fakeCall{kind: "start", name: name, args: cp})
	f.mu.Unlock()
	if f.onStart == nil {
		return newFakeProc(), nil
	}
	return f.onStart(ctx, name, cp)
}

func (f *fakeRunner) lastCall() fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fakeCall{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeRunner) findCall(kind string, mustContain string) (fakeCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.kind != kind {
			continue
		}
		joined := strings.Join(c.args, " ")
		if strings.Contains(joined, mustContain) {
			return c, true
		}
	}
	return fakeCall{}, false
}

// --- SocketPath tests ---

func TestSocketPath_simpleAlias(t *testing.T) {
	got := SocketPath("/tmp/ctl", "ubt")
	want := filepath.Join("/tmp/ctl", "ubt.sock")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSocketPath_sanitizesUnsafeChars(t *testing.T) {
	got := SocketPath("/tmp/ctl", "user@host:22")
	if strings.ContainsAny(got, "@:") {
		t.Errorf("unsafe chars not sanitized: %q", got)
	}
}

func TestSocketPath_longAliasHashed(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := SocketPath("/Users/kk/Library/Application Support/auto-port-forward/ctl", long)
	if len(got) > 104 {
		t.Errorf("path too long: %d chars: %q", len(got), got)
	}
	if !strings.Contains(got, "_") {
		t.Errorf("expected hash suffix marker `_`: %q", got)
	}
}

func TestSocketPath_deterministicForSameAlias(t *testing.T) {
	a := SocketPath("/tmp/ctl", "ubt")
	b := SocketPath("/tmp/ctl", "ubt")
	if a != b {
		t.Errorf("not deterministic: %q vs %q", a, b)
	}
}

// --- Connect: master process spawn + check polling ---

func TestConnect_spawnsMasterWithCorrectArgs(t *testing.T) {
	proc := newFakeProc()
	r := &fakeRunner{
		onStart: func(_ context.Context, _ string, _ []string) (Process, error) {
			return proc, nil
		},
		onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
			// 接受 -O check 立刻成功
			if len(args) >= 3 && args[len(args)-2] == "check" {
				return nil, nil
			}
			return nil, nil
		},
	}
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, t.TempDir())
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	// 验证 master start 调用
	start, ok := r.findCall("start", "-M")
	if !ok {
		t.Fatalf("master not started; calls=%+v", r.calls)
	}
	if start.name != "ssh" {
		t.Errorf("name = %q, want ssh", start.name)
	}
	joined := strings.Join(start.args, " ")
	for _, sub := range []string{
		"-M", "-S", "-N",
		"ServerAliveInterval=15", "ServerAliveCountMax=3", "ConnectTimeout=10",
		"ubt",
	} {
		if !strings.Contains(joined, sub) {
			t.Errorf("master args missing %q: %s", sub, joined)
		}
	}
	// 必须不带 -f（前台 master）
	for _, a := range start.args {
		if a == "-f" {
			t.Errorf("master must not use -f; args=%v", start.args)
		}
	}
}

func TestConnect_pollsCheckUntilSuccess(t *testing.T) {
	proc := newFakeProc()
	var checks int
	r := &fakeRunner{
		onStart: func(_ context.Context, _ string, _ []string) (Process, error) {
			return proc, nil
		},
		onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
			if len(args) >= 3 && args[len(args)-2] == "check" {
				checks++
				if checks < 3 {
					return nil, errors.New("not yet")
				}
				return nil, nil
			}
			return nil, nil
		},
	}
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, t.TempDir())
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	if checks < 3 {
		t.Errorf("expected at least 3 checks, got %d", checks)
	}
}

func TestConnect_masterExitsImmediatelyReturnsError(t *testing.T) {
	proc := newFakeProc()
	proc.exit(errors.New("auth failed"))
	r := &fakeRunner{
		onStart: func(_ context.Context, _ string, _ []string) (Process, error) {
			return proc, nil
		},
		onRun: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return nil, errors.New("no socket")
		},
	}
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, t.TempDir())
	err := c.Connect(context.Background())
	if err == nil {
		t.Errorf("expected error when master exits during connect")
	}
}

func TestConnect_ctxCancelReturnsError(t *testing.T) {
	proc := newFakeProc()
	r := &fakeRunner{
		onStart: func(_ context.Context, _ string, _ []string) (Process, error) {
			return proc, nil
		},
		onRun: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return nil, errors.New("never ready")
		},
	}
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if err := c.Connect(ctx); err == nil {
		t.Errorf("expected ctx cancel error")
	}
}

// --- Run / AddForward / CancelForward ---

func TestRun_buildsCommandViaSocket(t *testing.T) {
	r := newConnectedFake(t)
	c := connectedClient(t, r)
	defer c.Close()
	r.mu.Lock()
	r.onRun = func(_ context.Context, _ string, args []string) ([]byte, error) {
		// skip lifecycle calls (-O check / -O exit / forward / cancel)
		for _, a := range args {
			if a == "-O" {
				return nil, nil
			}
		}
		joined := strings.Join(args, " ")
		// 验证 -S sock alias cmd
		if !strings.Contains(joined, "-S") {
			t.Errorf("missing -S: %s", joined)
		}
		if args[len(args)-1] != "ss -H -tlnp 2>/dev/null" {
			t.Errorf("cmd not at tail: %v", args)
		}
		return []byte("ok\n"), nil
	}
	r.mu.Unlock()
	out, err := c.Run(context.Background(), "ss -H -tlnp 2>/dev/null")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(out) != "ok\n" {
		t.Errorf("out = %q", out)
	}
}

func TestAddForward_buildsForwardSpec(t *testing.T) {
	r := newConnectedFake(t)
	c := connectedClient(t, r)
	defer c.Close()
	var captured []string
	r.mu.Lock()
	r.onRun = func(_ context.Context, _ string, args []string) ([]byte, error) {
		captured = args
		return nil, nil
	}
	r.mu.Unlock()
	if err := c.AddForward(context.Background(), 5432); err != nil {
		t.Fatalf("AddForward: %v", err)
	}
	joined := strings.Join(captured, " ")
	for _, sub := range []string{"-O", "forward", "-L", "127.0.0.1:5432:127.0.0.1:5432"} {
		if !strings.Contains(joined, sub) {
			t.Errorf("missing %q: %s", sub, joined)
		}
	}
}

func TestAddForward_bindErrorPropagated(t *testing.T) {
	r := newConnectedFake(t)
	c := connectedClient(t, r)
	defer c.Close()
	r.mu.Lock()
	r.onRun = func(_ context.Context, _ string, _ []string) ([]byte, error) {
		return []byte("bind: Address already in use\n"), errors.New("exit status 255")
	}
	r.mu.Unlock()
	err := c.AddForward(context.Background(), 5432)
	if err == nil {
		t.Errorf("expected error on bind failure")
	}
	if !strings.Contains(err.Error(), "5432") {
		t.Errorf("error should mention port: %v", err)
	}
}

func TestCancelForward_buildsCancelSpec(t *testing.T) {
	r := newConnectedFake(t)
	c := connectedClient(t, r)
	defer c.Close()
	var captured []string
	r.mu.Lock()
	r.onRun = func(_ context.Context, _ string, args []string) ([]byte, error) {
		captured = args
		return nil, nil
	}
	r.mu.Unlock()
	if err := c.CancelForward(context.Background(), 5432); err != nil {
		t.Fatalf("CancelForward: %v", err)
	}
	joined := strings.Join(captured, " ")
	for _, sub := range []string{"-O", "cancel", "-L", "127.0.0.1:5432:127.0.0.1:5432"} {
		if !strings.Contains(joined, sub) {
			t.Errorf("missing %q: %s", sub, joined)
		}
	}
}

// --- Close / Done ---

func TestClose_sendsExitAndStopsProc(t *testing.T) {
	proc := newFakeProc()
	r := &fakeRunner{
		onStart: func(_ context.Context, _ string, _ []string) (Process, error) { return proc, nil },
		onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
			// 任何 -O check / -O exit 都直接成功
			return nil, nil
		},
	}
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, t.TempDir())
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// 让 master 在 Close 内 -O exit 后立刻退出
	go func() {
		time.Sleep(20 * time.Millisecond)
		proc.exit(nil)
	}()
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// 验证 -O exit 被调用
	if _, ok := r.findCall("run", "exit"); !ok {
		t.Errorf("Close did not invoke -O exit; calls=%+v", r.calls)
	}
}

func TestDone_closesOnMasterExit(t *testing.T) {
	proc := newFakeProc()
	r := &fakeRunner{
		onStart: func(_ context.Context, _ string, _ []string) (Process, error) { return proc, nil },
		onRun:   func(_ context.Context, _ string, _ []string) ([]byte, error) { return nil, nil },
	}
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, t.TempDir())
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	done := c.Done()
	if done == nil {
		t.Fatal("Done returned nil after Connect")
	}
	proc.exit(errors.New("master crashed"))
	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		t.Errorf("Done not closed after master exit")
	}
}

func TestDone_nilBeforeConnect(t *testing.T) {
	r := &fakeRunner{}
	c := NewClient(sshcfg.Host{Alias: "x"}, r, t.TempDir())
	if c.Done() != nil {
		t.Errorf("Done should be nil before Connect")
	}
}

// --- helpers ---

func newConnectedFake(t *testing.T) *fakeRunner {
	t.Helper()
	proc := newFakeProc()
	r := &fakeRunner{
		onStart: func(_ context.Context, _ string, _ []string) (Process, error) { return proc, nil },
		onRun:   func(_ context.Context, _ string, _ []string) ([]byte, error) { return nil, nil },
	}
	return r
}

func connectedClient(t *testing.T, r *fakeRunner) *Client {
	t.Helper()
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, t.TempDir())
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return c
}

// Sanity: ensure Client struct exposes alias for debugging printouts.
func TestClient_aliasAccessor(t *testing.T) {
	r := &fakeRunner{}
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, t.TempDir())
	if c.Alias() != "ubt" {
		t.Errorf("alias = %q, want ubt", c.Alias())
	}
}

// Sanity: socket path must live under the supplied control dir.
func TestNewClient_socketUnderControlDir(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{}
	c := NewClient(sshcfg.Host{Alias: "ubt"}, r, dir)
	if !strings.HasPrefix(c.SocketPath(), dir) {
		t.Errorf("sock %q not under %q", c.SocketPath(), dir)
	}
}

// Sanity: ensure NewClient does not panic on empty alias (defensive).
func TestNewClient_emptyAliasOk(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic on empty alias: %v", r)
		}
	}()
	_ = NewClient(sshcfg.Host{Alias: ""}, &fakeRunner{}, t.TempDir())
}

// Sanity: SocketPath sanitize behavior for special chars used in compound aliases.
func TestSocketPath_sanitizeMapping(t *testing.T) {
	cases := []struct{ in, mustContain string }{
		{"a/b", "a_b"},
		{"foo.bar-baz", "foo.bar-baz"},
		{"中文", "_"},
	}
	for _, c := range cases {
		got := SocketPath("/tmp", c.in)
		base := filepath.Base(got)
		if !strings.Contains(base, c.mustContain) {
			t.Errorf("alias %q -> %q, want contain %q", c.in, base, c.mustContain)
		}
	}
}

// Sanity: Runner interface is exported and DefaultRunner satisfies it.
func TestDefaultRunner_implementsRunner(t *testing.T) {
	var r Runner = NewDefaultRunner()
	out, err := r.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("out = %q", out)
	}
}

// Sanity: ensure args list passed to Start is preserved (slice copy safety).
func TestFakeRunner_argsAreCopied(t *testing.T) {
	r := &fakeRunner{}
	args := []string{"a", "b"}
	_, _ = r.Run(context.Background(), "x", args...)
	args[0] = "mutated"
	if !reflect.DeepEqual(r.calls[0].args, []string{"a", "b"}) {
		t.Errorf("calls leaked: %v", r.calls[0].args)
	}
}

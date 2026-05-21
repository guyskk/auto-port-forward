package scan

import (
	"context"
	"errors"
	"testing"

	"auto-port-forward/internal/model"
)

// fakeExecutor 按命令前缀路由到固定的响应或错误，供 Scan 测试使用。
type fakeExecutor struct {
	calls    []string
	bySubstr map[string]execResult
}

type execResult struct {
	out []byte
	err error
}

func (f *fakeExecutor) Run(_ context.Context, cmd string) ([]byte, error) {
	f.calls = append(f.calls, cmd)
	for sub, r := range f.bySubstr {
		if contains(cmd, sub) {
			return r.out, r.err
		}
	}
	return nil, errors.New("unconfigured cmd: " + cmd)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRemoteScanner_prefersSS(t *testing.T) {
	ex := &fakeExecutor{bySubstr: map[string]execResult{
		"tlnp": {out: []byte(ssSampleBasic)},
	}}
	s := NewRemoteScanner()
	ports, err := s.Scan(context.Background(), ex)
	if err != nil {
		t.Fatalf("Scan err: %v", err)
	}
	if _, ok := findPortRP(ports, 9527); !ok {
		t.Errorf("missing 9527: %#v", ports)
	}
}

func TestRemoteScanner_fallsBackToProc(t *testing.T) {
	ex := &fakeExecutor{bySubstr: map[string]execResult{
		"tlnp":          {err: errors.New("not installed")},
		"tln ":          {err: errors.New("not installed")},
		"/proc/net/tcp": {out: []byte(procV4Sample + procV6Sample)},
	}}
	s := NewRemoteScanner()
	ports, err := s.Scan(context.Background(), ex)
	if err != nil {
		t.Fatalf("Scan err: %v", err)
	}
	// 9527 来自 procV4Sample。
	if _, ok := findPortRP(ports, 9527); !ok {
		t.Errorf("expected 9527 from proc fallback: %#v", ports)
	}
	// 631 来自 procV6Sample。
	if _, ok := findPortRP(ports, 631); !ok {
		t.Errorf("expected 631 (v6 ::1) from proc fallback")
	}
}

func TestRemoteScanner_allMethodsFail(t *testing.T) {
	ex := &fakeExecutor{bySubstr: map[string]execResult{
		"ss":   {err: errors.New("nope")},
		"/proc": {err: errors.New("nope")},
	}}
	s := NewRemoteScanner()
	_, err := s.Scan(context.Background(), ex)
	if !errors.Is(err, ErrNoMethod) {
		t.Errorf("err = %v, want ErrNoMethod", err)
	}
}

func TestRemoteScanner_cachesPreferredMethod(t *testing.T) {
	ex := &fakeExecutor{bySubstr: map[string]execResult{
		"tlnp": {err: errors.New("perm")},
		"tln ": {out: []byte(ssSampleBasic)}, // 注意尾部空格避免匹配 "tlnp"
	}}
	s := NewRemoteScanner()
	if _, err := s.Scan(context.Background(), ex); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	firstCalls := len(ex.calls)
	if _, err := s.Scan(context.Background(), ex); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	// 第二次应该直接走 preferred（ss -H -tln），不再先试 -tlnp。
	if len(ex.calls)-firstCalls != 1 {
		t.Errorf("second scan ran %d cmds, want 1 (cached preferred)", len(ex.calls)-firstCalls)
	}
}

// findPortRP 在 []model.RemotePort 中查找端口。
func findPortRP(rps []model.RemotePort, port int) (model.RemotePort, bool) {
	for _, p := range rps {
		if p.Port == port {
			return p, true
		}
	}
	return model.RemotePort{}, false
}

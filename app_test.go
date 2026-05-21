package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/engine"
	"auto-port-forward/internal/events"
	"auto-port-forward/internal/sshcfg"
)

// fakeRunner 用于 app_test，模拟 sshcfg.Runner 的 sh / ssh -G 输出。
type fakeRunner struct {
	// aliases: sh -c 列举命令应返回的别名，每行一个。空列表 → 命令返回 ("", nil)。
	aliases []string
	// hosts: ssh -G <alias> 应返回的 effective 配置；缺失 alias 返回 error。
	hosts map[string]sshcfg.Host
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "sh" && len(args) >= 2 && args[0] == "-c" {
		out := ""
		for _, a := range f.aliases {
			out += a + "\n"
		}
		return []byte(out), nil
	}
	if name == "ssh" && len(args) >= 2 && args[0] == "-G" {
		alias := args[1]
		h, ok := f.hosts[alias]
		if !ok {
			return nil, errors.New("unknown alias")
		}
		out := ""
		if h.HostName != "" {
			out += "hostname " + h.HostName + "\n"
		}
		if h.User != "" {
			out += "user " + h.User + "\n"
		}
		if h.Port > 0 {
			out += "port " + itoa(h.Port) + "\n"
		}
		return []byte(out), nil
	}
	return nil, errors.New("fakeRunner: unexpected command " + name)
}

func itoa(i int) string {
	// 避免引入 strconv 导致 import 顺序乱
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	buf := [20]byte{}
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}

// fakeClient 实现 engine.ClientHandle，所有操作都是 no-op。
// 用于隔离 app 测试：避免真 fork ssh。
type fakeClient struct{}

func (fakeClient) Connect(_ context.Context) error                        { return nil }
func (fakeClient) Close() error                                           { return nil }
func (fakeClient) Run(_ context.Context, _ string) ([]byte, error)        { return nil, nil }
func (fakeClient) AddForward(_ context.Context, _ int) error              { return nil }
func (fakeClient) CancelForward(_ context.Context, _ int) error           { return nil }
func (fakeClient) Done() <-chan struct{}                                  { return nil }

// newTestApp 构造一个完全注入 fake 的 App：不 fork ssh，不依赖磁盘 controlDir。
func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	a := NewApp()
	a.emit = events.NopEmitter{}
	a.sshRunner = &fakeRunner{}
	a.controlDir = filepath.Join(dir, "ctl")
	a.clientFactory = func(_ sshcfg.Host) engine.ClientHandle { return fakeClient{} }
	return a, path
}

// 测试: SetHostEnabled 把别名加入 EnabledHosts 并落盘；重新 setup 后仍能读到。
func TestApp_SetHostEnabled_persists(t *testing.T) {
	a, path := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer a.Shutdown(ctx)

	if err := a.SetHostEnabled("ubt", true); err != nil {
		t.Fatalf("SetHostEnabled on: %v", err)
	}
	if got := a.EnabledHosts(); !reflect.DeepEqual(got, []string{"ubt"}) {
		t.Errorf("EnabledHosts after on = %#v, want [ubt]", got)
	}

	// 重新 setup 同一 path，确认落盘。
	a2, _ := newTestApp(t)
	if err := a2.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a2.Shutdown(ctx)
	if got := a2.EnabledHosts(); !reflect.DeepEqual(got, []string{"ubt"}) {
		t.Errorf("after reload, EnabledHosts = %#v, want [ubt]", got)
	}

	// 关掉后也应该落盘。
	if err := a2.SetHostEnabled("ubt", false); err != nil {
		t.Fatalf("SetHostEnabled off: %v", err)
	}
	if got := a2.EnabledHosts(); len(got) != 0 {
		t.Errorf("after off, EnabledHosts = %#v, want empty", got)
	}
}

// 测试: SetHostEnabled 对未知别名也能保存（孤儿别名保留语义）。
// 这是关键场景：用户先在 ssh config 里 enable 一个 alias，然后从 ssh config 里删了它。
// APP 应该保留它的 enabled 状态，等同名 host 再次出现自动恢复。
func TestApp_SetHostEnabled_orphanAlias(t *testing.T) {
	a, path := newTestApp(t)
	// 没有 host 在 fakeRunner 里 → ListHosts 返回空，但 SetHostEnabled 不依赖 ListHosts。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)

	if err := a.SetHostEnabled("ghost", true); err != nil {
		t.Fatalf("SetHostEnabled: %v", err)
	}
	if got := a.EnabledHosts(); !reflect.DeepEqual(got, []string{"ghost"}) {
		t.Errorf("EnabledHosts = %#v, want [ghost]", got)
	}
}

// 测试: UpdateRules 替换规则并落盘（无 LocalPortOffset 字段了）。
func TestApp_UpdateRules_persists(t *testing.T) {
	a, path := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)

	want := config.Rules{
		ExcludePorts:  []int{8080, 9000},
		ExcludeRanges: []config.Span{{Lo: 30000, Hi: 31000}},
	}
	if err := a.UpdateRules(want); err != nil {
		t.Fatalf("UpdateRules: %v", err)
	}
	got := a.GetConfig().Rules
	sort.Ints(got.ExcludePorts)
	sort.Ints(want.ExcludePorts)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after UpdateRules, Rules = %#v, want %#v", got, want)
	}

	a2, _ := newTestApp(t)
	if err := a2.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a2.Shutdown(ctx)
	got2 := a2.GetConfig().Rules
	sort.Ints(got2.ExcludePorts)
	if !reflect.DeepEqual(got2, want) {
		t.Errorf("after reload, Rules = %#v, want %#v", got2, want)
	}
}

// 测试: ListHosts 透传 fakeRunner 的输出。
func TestApp_ListHosts_returnsAllAliases(t *testing.T) {
	a, path := newTestApp(t)
	fr := &fakeRunner{
		aliases: []string{"ubt", "prod-db"},
		hosts: map[string]sshcfg.Host{
			"ubt":     {Alias: "ubt", HostName: "10.0.0.1", User: "ubuntu", Port: 22},
			"prod-db": {Alias: "prod-db", HostName: "db.example.com", User: "root", Port: 2222},
		},
	}
	a.sshRunner = fr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)
	// setup 会重新装 sshRunner 防御性赋值 → 我们再覆写一次。
	a.sshRunner = fr

	hosts, err := a.ListHosts()
	if err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("ListHosts len = %d, want 2; got %#v", len(hosts), hosts)
	}
	// 按 alias 排序保证断言稳定（ListHosts 顺序依赖 ListAliases 的 sort -u）。
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Alias < hosts[j].Alias })
	if hosts[0].Alias != "prod-db" || hosts[0].HostName != "db.example.com" {
		t.Errorf("hosts[0] = %#v", hosts[0])
	}
	if hosts[1].Alias != "ubt" || hosts[1].User != "ubuntu" {
		t.Errorf("hosts[1] = %#v", hosts[1])
	}
}

// 测试: TestHost 对空别名返回错误（避免无意义的连接尝试）。
func TestApp_TestHost_emptyAliasErr(t *testing.T) {
	a, path := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)

	if err := a.TestHost(""); err == nil {
		t.Errorf("TestHost('') should error")
	}
}

// 测试: UpdateScanInterval 落盘。
func TestApp_UpdateScanInterval_persists(t *testing.T) {
	a, path := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)

	if err := a.UpdateScanInterval(30); err != nil {
		t.Fatalf("UpdateScanInterval: %v", err)
	}
	if got := a.GetConfig().ScanIntervalSec; got != 30 {
		t.Errorf("ScanIntervalSec = %d, want 30", got)
	}

	a2, _ := newTestApp(t)
	if err := a2.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a2.Shutdown(ctx)
	if got := a2.GetConfig().ScanIntervalSec; got != 30 {
		t.Errorf("after reload, ScanIntervalSec = %d, want 30", got)
	}
}

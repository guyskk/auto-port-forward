package sshcfg

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeRunner: 命令行参数匹配 + 注入输出/错误。
type fakeRunner struct {
	calls   []fakeCall
	handler func(ctx context.Context, name string, args []string) ([]byte, error)
}

type fakeCall struct {
	name string
	args []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cp := make([]string, len(args))
	copy(cp, args)
	f.calls = append(f.calls, fakeCall{name: name, args: cp})
	if f.handler == nil {
		return nil, nil
	}
	return f.handler(ctx, name, cp)
}

// ListAliases 用 sh -c 运行 grep+awk pipeline，把每行别名读回；空行忽略。
func TestListAliases_parsesOutputLines(t *testing.T) {
	r := &fakeRunner{
		handler: func(_ context.Context, name string, args []string) ([]byte, error) {
			if name != "sh" {
				t.Errorf("name = %q, want sh", name)
			}
			if len(args) != 2 || args[0] != "-c" {
				t.Errorf("args = %v, want [-c <script>]", args)
			}
			// pipeline 必须既调用 grep（找 Host 行），也调用 awk（拆字段），也过滤通配符。
			script := args[1]
			for _, sub := range []string{"grep", "awk", "Host", "[*?!]"} {
				if !strings.Contains(script, sub) {
					t.Errorf("script missing %q: %s", sub, script)
				}
			}
			return []byte("alpha\nbeta\ngamma\n"), nil
		},
	}
	got, err := ListAliases(context.Background(), r)
	if err != nil {
		t.Fatalf("ListAliases: %v", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// 实际 ssh config 含混合：通配符、缩进、多别名一行、大写 Host —— 真实 grep+awk 管道
// 输出应该已经过滤通配符并展开多别名；ListAliases 只需把 stdout 按行收集。
func TestListAliases_handlesRealisticOutput(t *testing.T) {
	// 模拟 shell pipeline 已处理过的输出（unsorted，含空行容错）。
	stdout := "alpha\nbeta\ngamma\n\nindented-host\nupper-case-host\n"
	r := &fakeRunner{
		handler: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return []byte(stdout), nil
		},
	}
	got, err := ListAliases(context.Background(), r)
	if err != nil {
		t.Fatalf("ListAliases: %v", err)
	}
	want := []string{"alpha", "beta", "gamma", "indented-host", "upper-case-host"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// grep 找不到任何 Host 行时 exit code = 1（视为 grep 语义的"无匹配"，非错误）。
// 即便 runner 返回 err（exit code != 0），只要输出为空就当作"无 host"返回 nil。
func TestListAliases_grepNoMatchReturnsEmpty(t *testing.T) {
	r := &fakeRunner{
		handler: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return nil, errors.New("exit status 1")
		},
	}
	got, err := ListAliases(context.Background(), r)
	if err != nil {
		t.Errorf("expected nil error when output empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

// 真正的 runner 错误（如 sh not found）应该返回错误。
func TestListAliases_runnerErrorWithOutputPropagates(t *testing.T) {
	r := &fakeRunner{
		handler: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return []byte("partial-output\n"), errors.New("sh: command not found")
		},
	}
	_, err := ListAliases(context.Background(), r)
	if err == nil {
		t.Errorf("expected error to propagate when output present")
	}
}

// Resolve 调 ssh -G <alias>，解析 hostname/user/port 三行。
func TestResolve_parsesEffectiveConfig(t *testing.T) {
	out := `host ubt
user ubuntu
hostname 192.168.31.55
port 22
addressfamily any
batchmode no
`
	r := &fakeRunner{
		handler: func(_ context.Context, name string, args []string) ([]byte, error) {
			if name != "ssh" {
				t.Errorf("name = %q, want ssh", name)
			}
			if len(args) != 2 || args[0] != "-G" || args[1] != "ubt" {
				t.Errorf("args = %v, want [-G ubt]", args)
			}
			return []byte(out), nil
		},
	}
	got, err := Resolve(context.Background(), r, "ubt")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := Host{Alias: "ubt", HostName: "192.168.31.55", User: "ubuntu", Port: 22}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// ssh -G 输出大小写敏感 key 应该都匹配（小写）；port 非数字应回落默认 22。
func TestResolve_handlesCustomPort(t *testing.T) {
	out := "hostname example.com\nuser root\nport 2222\n"
	r := &fakeRunner{
		handler: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return []byte(out), nil
		},
	}
	got, _ := Resolve(context.Background(), r, "foo")
	if got.Port != 2222 {
		t.Errorf("Port = %d, want 2222", got.Port)
	}
}

func TestResolve_defaultsPortTo22(t *testing.T) {
	out := "hostname x\nuser y\n" // 没有 port 行
	r := &fakeRunner{
		handler: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return []byte(out), nil
		},
	}
	got, _ := Resolve(context.Background(), r, "foo")
	if got.Port != 22 {
		t.Errorf("Port = %d, want default 22", got.Port)
	}
}

func TestResolve_emptyAliasReturnsError(t *testing.T) {
	if _, err := Resolve(context.Background(), &fakeRunner{}, ""); err == nil {
		t.Errorf("empty alias should error")
	}
}

func TestResolve_runnerErrorPropagates(t *testing.T) {
	r := &fakeRunner{
		handler: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return nil, errors.New("ssh: unknown host")
		},
	}
	if _, err := Resolve(context.Background(), r, "ghost"); err == nil {
		t.Errorf("expected error to propagate")
	}
}

// ListHosts: 组合 ListAliases + Resolve；一次 sh + N 次 ssh -G。
func TestListHosts_combinesAliasesAndResolve(t *testing.T) {
	r := &fakeRunner{
		handler: func(_ context.Context, name string, args []string) ([]byte, error) {
			if name == "sh" {
				return []byte("alpha\nbeta\n"), nil
			}
			if name == "ssh" && len(args) == 2 && args[0] == "-G" {
				switch args[1] {
				case "alpha":
					return []byte("hostname 10.0.0.1\nuser root\nport 22\n"), nil
				case "beta":
					return []byte("hostname 10.0.0.2\nuser ops\nport 2200\n"), nil
				}
			}
			t.Fatalf("unexpected call: %s %v", name, args)
			return nil, nil
		},
	}
	got, err := ListHosts(context.Background(), r)
	if err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	want := []Host{
		{Alias: "alpha", HostName: "10.0.0.1", User: "root", Port: 22},
		{Alias: "beta", HostName: "10.0.0.2", User: "ops", Port: 2200},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Resolve 失败时跳过该 host，不影响其他 host。
func TestListHosts_skipsResolveFailures(t *testing.T) {
	r := &fakeRunner{
		handler: func(_ context.Context, name string, args []string) ([]byte, error) {
			if name == "sh" {
				return []byte("ok\nbroken\n"), nil
			}
			if name == "ssh" && args[1] == "broken" {
				return nil, errors.New("resolve failure")
			}
			return []byte("hostname x\nuser u\nport 22\n"), nil
		},
	}
	got, err := ListHosts(context.Background(), r)
	if err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	if len(got) != 1 || got[0].Alias != "ok" {
		t.Errorf("got %+v, want only [ok]", got)
	}
}

func TestListHosts_emptyAliasListReturnsEmpty(t *testing.T) {
	r := &fakeRunner{
		handler: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return nil, errors.New("exit status 1")
		},
	}
	got, err := ListHosts(context.Background(), r)
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// DefaultRunner 接口存在性 + 行为：调一个明确成功的命令（echo）。
func TestDefaultRunner_realExec(t *testing.T) {
	r := NewDefaultRunner()
	out, err := r.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("output = %q", out)
	}
}

package scan_test

import (
	"context"
	"os/exec"
	"testing"

	"auto-port-forward/internal/scan"
)

// sshExecutor 通过 ssh shell out 调用远端命令；仅集成测试用。
type sshExecutor struct{ host string }

func (e sshExecutor) Run(ctx context.Context, cmd string) ([]byte, error) {
	return exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", e.host, cmd).Output()
}

// TestRemoteScanner_RealUBT 是一个非默认运行的集成测试。
// 需要 `ssh ubt` 能免密登录。`go test -tags=integration` 才会跑。
func TestRemoteScanner_RealUBT_smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("smoke test against real ubt, skipped in -short")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh in PATH")
	}
	// 探测一次 — 失败/没有 ubt 也只是 t.Skip。
	out, err := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "ubt", "true").CombinedOutput()
	if err != nil {
		t.Skipf("cannot ssh ubt (skip): %v %s", err, out)
	}
	s := scan.NewRemoteScanner()
	ports, err := s.Scan(context.Background(), sshExecutor{host: "ubt"})
	if err != nil {
		t.Fatalf("Scan err: %v", err)
	}
	if len(ports) == 0 {
		t.Fatalf("got 0 ports — expected at least sshd:22")
	}
	hasSSH := false
	for _, p := range ports {
		if p.Port == 22 {
			hasSSH = true
		}
	}
	if !hasSSH {
		t.Errorf("no port 22 in result; got %d entries", len(ports))
	}
	t.Logf("ubt has %d listening ports", len(ports))
}

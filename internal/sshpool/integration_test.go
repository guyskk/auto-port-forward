//go:build integration

package sshpool_test

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"autoportforward/internal/config"
	"autoportforward/internal/sshpool"
)

// resolveSSHConfig 调用 `ssh -G <host>` 解析有效配置。
func resolveSSHConfig(t *testing.T, host string) (h, user string, port int, keyPath string, ok bool) {
	t.Helper()
	out, err := exec.Command("ssh", "-G", host).Output()
	if err != nil {
		return "", "", 0, "", false
	}
	port = 22
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "hostname":
			h = fields[1]
		case "user":
			user = fields[1]
		case "port":
			if p, e := atoi(fields[1]); e == nil {
				port = p
			}
		case "identityfile":
			// 取第一个存在的。
			if keyPath == "" {
				p := expandHome(fields[1])
				if _, err := os.Stat(p); err == nil {
					keyPath = p
				}
			}
		}
	}
	if h == "" || user == "" || keyPath == "" {
		return "", "", 0, "", false
	}
	return h, user, port, keyPath, true
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		hd, _ := os.UserHomeDir()
		return filepath.Join(hd, p[2:])
	}
	return p
}

func atoi(s string) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, &numErr{}
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, nil
}

type numErr struct{}

func (numErr) Error() string { return "not a number" }

func TestClient_RealUBT(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh")
	}
	host, user, port, keyPath, ok := resolveSSHConfig(t, "ubt")
	if !ok {
		t.Skip("cannot resolve ssh config for 'ubt'")
	}
	if out, err := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "ubt", "true").CombinedOutput(); err != nil {
		t.Skipf("ssh ubt unreachable: %v %s", err, out)
	}

	cli := sshpool.NewClient(config.Server{
		Host: host, Port: port, User: user,
		AuthMethod: "ssh_key", KeyPath: keyPath, HostKey: "insecure",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := cli.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer cli.Close()

	got, err := cli.Run(ctx, "echo hi-from-ssh")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(string(got)) != "hi-from-ssh" {
		t.Errorf("got %q, want hi-from-ssh", got)
	}
	if cli.State() != sshpool.StateConnected {
		t.Errorf("state = %v, want connected", cli.State())
	}
}

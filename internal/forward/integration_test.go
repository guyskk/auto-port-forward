//go:build integration

package forward_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/forward"
	"auto-port-forward/internal/sshpool"
)

// 与 sshpool integration test 同样的 ssh -G 解析。
func resolveSSHConfig(t *testing.T, host string) (h, user string, port int, keyPath string, ok bool) {
	t.Helper()
	out, err := exec.Command("ssh", "-G", host).Output()
	if err != nil {
		return "", "", 0, "", false
	}
	port = 22
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		fields := strings.SplitN(sc.Text(), " ", 2)
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
			return 0, fmt.Errorf("nan")
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, nil
}

// 测试：用 sshpool.Client + forward.Forward 把 ubt:22 (sshd banner) 转发到本地随机端口，
// dial 本地端口应该读到 "SSH-2.0-..." banner。
func TestForward_EndToEnd_UBT_SSHBanner(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh")
	}
	host, user, sshPort, keyPath, ok := resolveSSHConfig(t, "ubt")
	if !ok {
		t.Skip("cannot resolve ubt config")
	}
	if out, err := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "ubt", "true").CombinedOutput(); err != nil {
		t.Skipf("ssh ubt unreachable: %v %s", err, out)
	}

	cli := sshpool.NewClient(config.Server{
		Host: host, Port: sshPort, User: user,
		AuthMethod: "ssh_key", KeyPath: keyPath, HostKey: "insecure",
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cli.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer cli.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	localPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	f := &forward.Forward{
		RemotePort: 22, // sshd 一定在
		LocalPort:  localPort,
		Bind:       "127.0.0.1",
	}
	go func() { _ = f.Run(ctx, cli) }()

	// 等监听就位。
	var conn net.Conn
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("cannot dial forwarded port: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read banner: %v", err)
	}
	banner := string(buf[:n])
	if !strings.HasPrefix(banner, "SSH-") {
		t.Errorf("expected ssh banner, got %q", banner)
	}
	t.Logf("got banner via forward: %q", strings.TrimSpace(banner))
}

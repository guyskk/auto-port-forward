// Command verify_e2e 是端到端验证脚本：不依赖 wails GUI，直接装配 engine + sshpool +
// scan 跑真实链路，连远端 ubt，扫描端口，启动 forward，并实际 TCP 连接本地转发端口验证通路。
//
// 用法：
//
//	go run ./cmd/verify_e2e -host 192.168.31.55 -user ubuntu -offset 50000
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/engine"
	"auto-port-forward/internal/events"
	"auto-port-forward/internal/model"
	"auto-port-forward/internal/scan"
	"auto-port-forward/internal/sshpool"
)

// stdoutEmitter 在 stdout 打印所有事件，方便观察。
type stdoutEmitter struct{}

func (stdoutEmitter) Emit(_ context.Context, name string, data any) {
	switch name {
	case events.EventForwardUpdate, events.EventServerStatus, events.EventScanError:
		fmt.Printf("[EVT] %-18s %+v\n", name, data)
	}
}

func main() {
	var (
		host    = flag.String("host", "192.168.31.55", "remote ssh host")
		port    = flag.Int("port", 22, "remote ssh port")
		user    = flag.String("user", "ubuntu", "remote ssh user")
		auth    = flag.String("auth", "ssh_agent", "auth method: ssh_agent | ssh_key | password")
		keyPath = flag.String("key", "", "private key path (auth=ssh_key)")
		offset  = flag.Int("offset", 50000, "local port offset")
		timeout = flag.Duration("timeout", 60*time.Second, "overall E2E timeout")
	)
	flag.Parse()

	cfg := config.Config{
		ScanIntervalSec: 5,
		Servers: []config.Server{
			{
				ID:         "verify-ubt",
				Name:       "verify-ubt",
				Host:       *host,
				Port:       *port,
				User:       *user,
				AuthMethod: *auth,
				KeyPath:    *keyPath,
				HostKey:    "insecure", // 简化验证；生产建议 known_hosts
				Enabled:    true,
			},
		},
		Rules: config.Rules{
			ExcludePorts:    []int{22, 53, 80, 443, 111, 631},
			LocalPortOffset: *offset,
		},
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	ctx, cancelT := context.WithTimeout(ctx, *timeout)
	defer cancelT()

	eng := engine.New(cfg, stdoutEmitter{}, engine.Deps{
		ClientFactory: func(s config.Server) engine.ClientHandle { return sshpool.NewClient(s) },
		LocalScan:     scan.ScanLocal,
		IsRoot:        os.Geteuid() == 0,
	})

	log.Printf("[E2E] starting engine — connect to %s@%s:%d offset=+%d", *user, *host, *port, *offset)
	if err := eng.StartAll(ctx); err != nil {
		log.Fatalf("StartAll: %v", err)
	}
	defer eng.StopAll()

	// 等连接就绪再 ScanNow（最多 8s）。
	if err := waitConnected(ctx, eng, 8*time.Second); err != nil {
		log.Fatalf("wait connected: %v", err)
	}
	log.Println("[E2E] ssh connected; triggering scan")

	scanCtx, sc := context.WithTimeout(ctx, 10*time.Second)
	if err := eng.ScanNow(scanCtx); err != nil {
		sc()
		log.Fatalf("ScanNow: %v", err)
	}
	sc()

	// 等几秒让 forward 进入 forwarding。
	deadline := time.Now().Add(15 * time.Second)
	var ok []model.Forward
	for time.Now().Before(deadline) {
		snap := eng.Snapshot()
		ok = filterForwarding(snap)
		if len(ok) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// 打印所有 forward 状态。
	full := eng.Snapshot()
	sort.Slice(full, func(i, j int) bool { return full[i].RemotePort < full[j].RemotePort })
	log.Printf("[E2E] snapshot (%d entries):", len(full))
	for _, f := range full {
		log.Printf("  remote=%d local=%d status=%s err=%q", f.RemotePort, f.LocalPort, f.Status, f.Error)
	}

	if len(ok) == 0 {
		log.Fatalf("[E2E] FAIL: no forward reached `forwarding` state")
	}

	// 用 net.Dial 实际连接每条转发的本地端口，验证 TCP 可通。
	pass := 0
	for _, f := range ok {
		addr := fmt.Sprintf("127.0.0.1:%d", f.LocalPort)
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			log.Printf("[E2E] dial %s (remote %d): FAIL %v", addr, f.RemotePort, err)
			continue
		}
		// 试读 banner（SSH/Redis/Postgres 等有些会主动推送）。
		_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		conn.Close()
		banner := ""
		if n > 0 {
			banner = strings.TrimRight(string(buf[:n]), "\r\n")
		}
		log.Printf("[E2E] dial %s (remote %d): OK banner=%q", addr, f.RemotePort, banner)
		pass++
	}
	if pass == 0 {
		log.Fatalf("[E2E] FAIL: all dials to local forwarded ports failed")
	}
	log.Printf("[E2E] PASS: %d/%d forwarded ports verified end-to-end", pass, len(ok))
}

// waitConnected 阻塞直到 engine 能成功 ScanNow（说明 ssh 已建立）。
// 失败时返回最后一次错误；timeout 后返回 context.DeadlineExceeded。
func waitConnected(ctx context.Context, eng *engine.Engine, max time.Duration) error {
	deadline := time.Now().Add(max)
	var lastErr error
	for time.Now().Before(deadline) {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := eng.ScanNow(c)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return context.DeadlineExceeded
}

// filterForwarding 返回 status=forwarding 的条目。
func filterForwarding(snap []model.Forward) []model.Forward {
	var out []model.Forward
	for _, f := range snap {
		if f.Status == model.StatusForwarding {
			out = append(out, f)
		}
	}
	return out
}

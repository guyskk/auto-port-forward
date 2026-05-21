// Command verify_e2e 是端到端验证脚本：不依赖 wails GUI，直接装配 engine + sshctl +
// scan 跑真实链路，连远端 host，扫描端口，启动 forward，并实际 TCP 连接本地转发端口验证通路。
//
// 用法（以 ssh config 里的别名为单位 — 与 GUI 同源）：
//
//	go run ./cmd/verify_e2e -alias ubt
//
// 前提：~/.ssh/config 里有该别名，且 `ssh <alias>` 能直接连通（host key 已信任、认证已配好）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/engine"
	"auto-port-forward/internal/events"
	"auto-port-forward/internal/model"
	"auto-port-forward/internal/scan"
	"auto-port-forward/internal/sshcfg"
	"auto-port-forward/internal/sshctl"
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
		alias   = flag.String("alias", "ubt", "ssh config alias to connect")
		timeout = flag.Duration("timeout", 60*time.Second, "overall E2E timeout")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	ctx, cancelT := context.WithTimeout(ctx, *timeout)
	defer cancelT()

	// 1. 解析 alias → effective host 配置（通过系统 ssh -G）。
	runner := sshcfg.NewDefaultRunner()
	host, err := sshcfg.Resolve(ctx, runner, *alias)
	if err != nil {
		log.Fatalf("resolve alias %q: %v", *alias, err)
	}
	log.Printf("[E2E] resolved %q → %s@%s:%d", *alias, host.User, host.HostName, host.Port)

	// 2. controlDir：与 GUI 默认路径一致，便于排错。
	userCfgDir, err := os.UserConfigDir()
	if err != nil {
		userCfgDir = "."
	}
	controlDir := filepath.Join(userCfgDir, "auto-port-forward", "ctl")

	// 3. 装配 engine — ClientFactory 直接产 sshctl.NewClient。
	cfg := config.Config{
		ScanIntervalSec: 5,
		Rules: config.Rules{
			ExcludePorts: []int{22, 53, 80, 443, 111, 631},
		},
		EnabledHosts: []string{*alias},
	}
	sshctlRunner := sshctl.NewDefaultRunner()
	eng := engine.New(cfg, stdoutEmitter{}, engine.Deps{
		ClientFactory: func(h sshcfg.Host) engine.ClientHandle {
			return sshctl.NewClient(h, sshctlRunner, controlDir)
		},
		LocalScan: scan.ScanLocal,
		IsRoot:    os.Geteuid() == 0,
	})
	// 把 host 注入 enabled 集合 → engine 会 Connect 之。
	if err := eng.ApplyServers([]sshcfg.Host{host}); err != nil {
		log.Fatalf("ApplyServers: %v", err)
	}

	log.Printf("[E2E] starting engine")
	if err := eng.StartAll(ctx); err != nil {
		log.Fatalf("StartAll: %v", err)
	}
	defer eng.StopAll()

	// 等连接就绪再 ScanNow（最多 12s — ControlMaster 首连可能比单次 dial 慢）。
	if err := waitConnected(ctx, eng, 12*time.Second); err != nil {
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

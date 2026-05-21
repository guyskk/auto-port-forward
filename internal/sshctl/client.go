package sshctl

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"auto-port-forward/internal/sshcfg"
)

const (
	// master 进程通用 ssh 选项 — 连接健壮性参数（不涉及认证策略）
	optServerAliveInterval = "ServerAliveInterval=15"
	optServerAliveCountMax = "ServerAliveCountMax=3"
	optConnectTimeout      = "ConnectTimeout=10"

	// master 探活轮询参数
	connectCheckInterval = 200 * time.Millisecond
	connectCheckTimeout  = 10 * time.Second
	checkRunTimeout      = 3 * time.Second
	exitRunTimeout       = 3 * time.Second
)

// Client 通过 ssh ControlMaster 管理与单个 host 的连接。
//
// 一个 Client 持有一个 master 进程的句柄 + socket 路径 + Runner。
// Run / AddForward / CancelForward 都通过 socket 复用 master 连接，无需重新认证。
type Client struct {
	alias      string
	sockPath   string
	runner     Runner
	controlDir string

	mu     sync.Mutex
	master Process
	doneCh chan struct{}
}

// NewClient 构造一个 Client；不立刻连接。
func NewClient(host sshcfg.Host, runner Runner, controlDir string) *Client {
	return &Client{
		alias:      host.Alias,
		sockPath:   SocketPath(controlDir, host.Alias),
		runner:     runner,
		controlDir: controlDir,
	}
}

// Alias 返回客户端绑定的 ssh config 别名。
func (c *Client) Alias() string { return c.alias }

// SocketPath 返回 master ControlMaster socket 的完整路径。
func (c *Client) SocketPath() string { return c.sockPath }

// Connect 启动 master 进程并轮询 -O check，直到成功或在超时内失败。
//
// 失败路径：
//   - 进程启动失败 → 立即返回 err
//   - master 进程提前退出 → 返回 err
//   - 10s 内 check 始终失败 → 返回 timeout err，并尽力清理
//   - ctx 被取消 → 返回 ctx.Err()
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.master != nil {
		c.mu.Unlock()
		return nil
	}
	if err := os.MkdirAll(c.controlDir, 0o700); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("sshctl: create control dir: %w", err)
	}
	args := []string{
		"-M", "-S", c.sockPath, "-N",
		"-o", optServerAliveInterval,
		"-o", optServerAliveCountMax,
		"-o", optConnectTimeout,
		c.alias,
	}
	p, err := c.runner.Start(ctx, "ssh", args...)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("sshctl: start master: %w", err)
	}
	c.master = p
	c.doneCh = make(chan struct{})
	done := c.doneCh
	c.mu.Unlock()

	// 进程退出 → 关闭 doneCh（信号供 supervise 用）
	go func() {
		<-p.Wait()
		c.mu.Lock()
		defer c.mu.Unlock()
		select {
		case <-done:
		default:
			close(done)
		}
	}()

	// 轮询 -O check
	deadline := time.Now().Add(connectCheckTimeout)
	ticker := time.NewTicker(connectCheckInterval)
	defer ticker.Stop()
	for {
		if c.checkAlive(ctx) {
			return nil
		}
		// master 进程提前退出 → 立即返回错误
		select {
		case <-done:
			return fmt.Errorf("sshctl: master process exited during connect")
		default:
		}
		if time.Now().After(deadline) {
			_ = c.Close()
			return fmt.Errorf("sshctl: connect timeout after %v", connectCheckTimeout)
		}
		select {
		case <-ctx.Done():
			_ = c.Close()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) checkAlive(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, checkRunTimeout)
	defer cancel()
	_, err := c.runner.Run(cctx, "ssh", "-S", c.sockPath, "-O", "check", c.alias)
	return err == nil
}

// Run 在远端通过 master 执行命令并返回 stdout。实现 scan.Executor 契约。
func (c *Client) Run(ctx context.Context, cmd string) ([]byte, error) {
	return c.runner.Run(ctx, "ssh", "-S", c.sockPath, c.alias, cmd)
}

// AddForward 加一条本地转发：127.0.0.1:port → 远端 127.0.0.1:port。
//
// 失败时（如本地端口已被占用），将 stderr 摘要附在 error 上，便于上层分类。
func (c *Client) AddForward(ctx context.Context, port int) error {
	spec := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", port, port)
	out, err := c.runner.Run(ctx, "ssh", "-S", c.sockPath, "-O", "forward", "-L", spec, c.alias)
	if err != nil {
		return fmt.Errorf("sshctl: add forward port=%d: %w; output=%s", port, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CancelForward 取消一条已建立的本地转发。
func (c *Client) CancelForward(ctx context.Context, port int) error {
	spec := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", port, port)
	out, err := c.runner.Run(ctx, "ssh", "-S", c.sockPath, "-O", "cancel", "-L", spec, c.alias)
	if err != nil {
		return fmt.Errorf("sshctl: cancel forward port=%d: %w; output=%s", port, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Close 优雅关闭 master 进程（先 -O exit，再等 Wait 返回或超时 Kill）。
//
// 重复调用安全；返回的 error 总是 nil（实际错误已被 swallowed），与 io.Closer 语义对齐。
func (c *Client) Close() error {
	c.mu.Lock()
	p := c.master
	c.master = nil
	c.mu.Unlock()
	if p == nil {
		return nil
	}
	// 用独立 ctx 跑 -O exit：调用方 ctx 已取消时仍需清理。
	exitCtx, cancel := context.WithTimeout(context.Background(), exitRunTimeout)
	_, _ = c.runner.Run(exitCtx, "ssh", "-S", c.sockPath, "-O", "exit", c.alias)
	cancel()
	select {
	case <-p.Wait():
	case <-time.After(exitRunTimeout):
		_ = p.Kill()
	}
	return nil
}

// Done 返回一个 channel：master 进程退出时关闭。
// Connect 之前返回 nil — 调用方 select 中收到 nil channel 会永久阻塞。
func (c *Client) Done() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doneCh
}

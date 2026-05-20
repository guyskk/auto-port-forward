// Package sshpool 管理单个 server 的 SSH 客户端生命周期。
package sshpool

import (
	"context"
	"errors"
	"net"

	"golang.org/x/crypto/ssh"

	"autoportforward/internal/config"
)

// State 表示池子里某 server 的连接状态。
type State string

const (
	StateIdle      State = "idle"      // 未连接
	StateDialing   State = "dialing"   // 正在 dial
	StateConnected State = "connected" // 连上
	StateBroken    State = "broken"    // 断开等待重连
	StateDegraded  State = "degraded"  // 长时间断开（默认 >15min）
)

// Client 包装 *ssh.Client，便于上层做 Run（执行命令）和 Dial（开 channel）。
type Client struct {
	cfg   config.Server
	conn  *ssh.Client
	state State
}

// NewClient 构造，不立刻连接。
func NewClient(cfg config.Server) *Client { return &Client{cfg: cfg, state: StateIdle} }

// ErrNotConnected 表示尚未连上。
var ErrNotConnected = errors.New("ssh client not connected")

// Connect 建立连接（带超时/认证/hostkey 校验）。
// TODO(M4): 根据 cfg.AuthMethod 构造 ssh.AuthMethod（password / publickey / agent）。
// TODO(M4): cfg.HostKey == "insecure" → InsecureIgnoreHostKey，否则用 known_hosts。
// TODO(M4): keepalive 5s 一次心跳，失败标记 broken。
func (c *Client) Connect(ctx context.Context) error {
	_ = ctx
	return ErrNotConnected
}

// Close 关闭底层 ssh.Client。
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Run 在远端执行命令并返回 stdout，实现 scan.Executor。
// TODO(M4): NewSession → CombinedOutput；分离 stdout/stderr，区分 exit code。
func (c *Client) Run(ctx context.Context, cmd string) ([]byte, error) {
	_ = ctx
	_ = cmd
	return nil, ErrNotConnected
}

// Dial 在远端发起一个 tcp 通道（用于 forward.handle 复用 ssh 连接）。
// TODO(M4): 返回 c.conn.DialContext(ctx, "tcp", addr)；conn 为 nil 时返回 ErrNotConnected。
func (c *Client) Dial(ctx context.Context, addr string) (net.Conn, error) {
	_ = ctx
	_ = addr
	return nil, ErrNotConnected
}

// State 返回当前状态。
func (c *Client) State() State { return c.state }

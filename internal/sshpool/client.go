// Package sshpool 管理单个 server 的 SSH 客户端生命周期。
package sshpool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"autoportforward/internal/config"
)

// State 表示池子里某 server 的连接状态。
type State string

const (
	StateIdle      State = "idle"
	StateDialing   State = "dialing"
	StateConnected State = "connected"
	StateBroken    State = "broken"
	StateDegraded  State = "degraded"
)

// Client 包装 *ssh.Client，支持 Run（执行命令）与 Dial（开 channel）。
// 单实例并发安全：所有 ssh.Client 操作都被 mu 保护。
type Client struct {
	cfg config.Server

	mu    sync.RWMutex
	conn  *ssh.Client
	state State
}

// NewClient 构造，不立刻连接。
func NewClient(cfg config.Server) *Client {
	return &Client{cfg: cfg, state: StateIdle}
}

// ErrNotConnected 表示尚未连上。
var ErrNotConnected = errors.New("ssh client not connected")

// Connect 建立连接（带超时/认证/hostkey 校验）。
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	c.state = StateDialing
	c.mu.Unlock()

	auths, err := buildAuthMethods(c.cfg)
	if err != nil {
		c.setState(StateBroken)
		return err
	}
	port := c.cfg.Port
	if port == 0 {
		port = 22
	}
	sshCfg := &ssh.ClientConfig{
		User:            c.cfg.User,
		Auth:            auths,
		HostKeyCallback: buildHostKeyCallback(c.cfg),
		Timeout:         10 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", c.cfg.Host, port)
	d := net.Dialer{Timeout: 10 * time.Second}
	tcpConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		c.setState(StateBroken)
		return err
	}
	conn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, sshCfg)
	if err != nil {
		_ = tcpConn.Close()
		c.setState(StateBroken)
		return err
	}
	client := ssh.NewClient(conn, chans, reqs)

	c.mu.Lock()
	c.conn = client
	c.state = StateConnected
	c.mu.Unlock()

	go c.keepalive()
	return nil
}

// keepalive 每 30s 发一次 ssh keepalive；失败标记 broken。
func (c *Client) keepalive() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()
		if conn == nil {
			return
		}
		_, _, err := conn.SendRequest("keepalive@autoportforward", true, nil)
		if err != nil {
			c.setState(StateBroken)
			_ = c.Close()
			return
		}
	}
}

// Close 关闭底层 ssh.Client。
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.state = StateIdle
	return err
}

// Run 在远端执行命令并返回 stdout，实现 scan.Executor。
func (c *Client) Run(ctx context.Context, cmd string) ([]byte, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		return nil, ErrNotConnected
	}
	sess, err := conn.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	var out, stderr bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &stderr

	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		done <- result{err: sess.Run(cmd)}
	}()
	select {
	case <-ctx.Done():
		_ = sess.Close()
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return out.Bytes(), fmt.Errorf("ssh exec %q: %w; stderr=%s", cmd, r.err, stderr.String())
		}
		return out.Bytes(), nil
	}
}

// Dial 在远端发起一个 tcp 通道（用于 forward.handle 复用 ssh 连接）。
func (c *Client) Dial(ctx context.Context, addr string) (net.Conn, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		return nil, ErrNotConnected
	}
	// ssh.Client.Dial 没有 ctx 版；ctx 取消由调用方关闭 net.Conn 实现。
	_ = ctx
	return conn.Dial("tcp", addr)
}

// State 返回当前状态。
func (c *Client) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Client) setState(s State) {
	c.mu.Lock()
	c.state = s
	c.mu.Unlock()
}

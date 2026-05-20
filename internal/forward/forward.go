// Package forward 实现一条远端 port → 本地 port 的 TCP 转发。
package forward

import (
	"context"
	"net"
)

// Dialer 把上层 SSH 客户端的 Dial 能力注入进来，避免在本包硬依赖 sshpool。
type Dialer interface {
	Dial(ctx context.Context, addr string) (net.Conn, error)
}

// ReportFunc 上报状态变化（status、error）。
type ReportFunc func(status string, err error)

// Forward 是一条转发的运行实例。
type Forward struct {
	RemotePort int
	LocalPort  int
	Bind       string // 默认 "127.0.0.1"
	Report     ReportFunc
}

// Run 启动 listener 并接受连接；ctx 取消时关闭 listener 并停止 accept。
// 该函数阻塞直至 ctx 结束或 listener 出错。
// TODO(M4): net.Listen → accept loop → go f.handle(ctx, d, c)。
// TODO(M4): listen 失败 → Report(conflict|conflict_priv, err) 并返回。
func (f *Forward) Run(ctx context.Context, d Dialer) error {
	_ = ctx
	_ = d
	return nil
}

// handle 处理一个本地连接：cli.Dial 远端再 bridge。
// TODO(M4): d.Dial(ctx, "127.0.0.1:RemotePort") → bridge。
func (f *Forward) handle(ctx context.Context, d Dialer, local net.Conn) {
	_ = ctx
	_ = d
	_ = local
}

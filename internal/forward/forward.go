// Package forward 实现一条远端 port → 本地 port 的 TCP 转发。
package forward

import (
	"context"
	"fmt"
	"net"
)

// Dialer 把上层 SSH 客户端的 Dial 能力注入进来，避免在本包硬依赖 sshpool。
type Dialer interface {
	Dial(ctx context.Context, addr string) (net.Conn, error)
}

// ReportFunc 上报状态变化（status、error）。
// status 取 model.PortStatus 的字符串值，本包不直接依赖 model 包以避免循环导入。
type ReportFunc func(status string, err error)

// Forward 是一条转发的运行实例。
type Forward struct {
	RemotePort int
	LocalPort  int
	Bind       string // 默认 "127.0.0.1"
	Report     ReportFunc
}

func (f *Forward) report(status string, err error) {
	if f.Report != nil {
		f.Report(status, err)
	}
}

func (f *Forward) bindHost() string {
	if f.Bind == "" {
		return "127.0.0.1"
	}
	return f.Bind
}

// Run 启动 listener 并接受连接；ctx 取消时关闭 listener 并停止 accept。
// Listen 失败 → Report("conflict", err) 并返回错误。
// 函数阻塞直至 ctx 结束或 listener 出错。
func (f *Forward) Run(ctx context.Context, d Dialer) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", f.bindHost(), f.LocalPort))
	if err != nil {
		f.report("conflict", err)
		return err
	}
	f.report("forwarding", nil)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go f.handle(ctx, d, conn)
	}
}

// handle 处理一个本地连接：d.Dial 远端再 bridge。
func (f *Forward) handle(ctx context.Context, d Dialer, local net.Conn) {
	remote, err := d.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", f.RemotePort))
	if err != nil {
		f.report("error", err)
		_ = local.Close()
		return
	}
	bridge(ctx, local, remote)
}

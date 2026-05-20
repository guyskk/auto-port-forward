// Package scan 远端 / 本地端口扫描。
//
// remote.go 是远端扫描入口，按探测链顺序尝试 ss → /proc/net/tcp → netstat，
// 首个成功即缓存其方法名于 Scanner 内部，下次直接走该路径。
package scan

import (
	"context"
	"errors"

	"autoportforward/internal/model"
)

// Executor 抽象 SSH 远端命令执行；engine 用 sshpool.Client 适配实现。
type Executor interface {
	// Run 在远端执行命令并返回 stdout 字节；exit != 0 时返回 stderr 包装的 error。
	Run(ctx context.Context, cmd string) ([]byte, error)
}

// RemoteScanner 持有探测方法缓存。零值可用。
type RemoteScanner struct {
	preferred string // "ss" | "ss_no_p" | "proc" | "netstat"
}

// NewRemoteScanner 构造一个 Scanner。
func NewRemoteScanner() *RemoteScanner { return &RemoteScanner{} }

// ErrNoMethod 表示所有探测方法均失败。
var ErrNoMethod = errors.New("no working remote port enumeration method")

// Scan 执行远端端口扫描。
// TODO(M2): 按 preferred 顺序尝试 ss -tlnp / ss -tln / cat /proc/net/tcp{,6} / netstat。
// TODO(M2): 第一次成功后缓存到 s.preferred。
// TODO(M2): 解析委托给 ParseSS / ParseProcNetTCP / ParseNetstat 等纯函数。
func (s *RemoteScanner) Scan(ctx context.Context, ex Executor) ([]model.RemotePort, error) {
	_ = ctx
	_ = ex
	return nil, ErrNoMethod
}

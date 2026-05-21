// Package scan 远端 / 本地端口扫描。
//
// remote.go 是远端扫描入口，按探测链顺序尝试 ss → /proc/net/tcp → netstat，
// 首个成功即缓存其方法名于 Scanner 内部，下次直接走该路径。
package scan

import (
	"context"
	"errors"

	"auto-port-forward/internal/model"
)

// Executor 抽象 SSH 远端命令执行；engine 用 sshpool.Client 适配实现。
type Executor interface {
	// Run 在远端执行命令并返回 stdout 字节；exit != 0 时返回 stderr 包装的 error。
	Run(ctx context.Context, cmd string) ([]byte, error)
}

// methodKey 探测链方法标识。
type methodKey string

const (
	methodSS    methodKey = "ss"
	methodSSNoP methodKey = "ss_no_p"
	methodProc  methodKey = "proc"
)

// methodSpec 描述探测链中的一种方法。
type methodSpec struct {
	key   methodKey
	cmd   string
	parse func(out []byte) []model.RemotePort
}

// methods 是探测链的固定顺序。
var methods = []methodSpec{
	{methodSS, "ss -H -tlnp 2>/dev/null", func(b []byte) []model.RemotePort { return ParseSS(b) }},
	{methodSSNoP, "ss -H -tln 2>/dev/null", func(b []byte) []model.RemotePort { return ParseSS(b) }},
	{methodProc, "cat /proc/net/tcp /proc/net/tcp6 2>/dev/null", parseProcDual},
}

// parseProcDual 解析 v4 和 v6 拼接后的 /proc/net/tcp 输出。
// 行首字段（"sl"）和数据行格式可区分 v4/v6 长度：v4 IP 是 8 hex，v6 是 32 hex。
// 这里简单地按行扫描，分别尝试 v4 / v6 两种解码（决定哪种由 hex 长度判断）。
func parseProcDual(out []byte) []model.RemotePort {
	if len(out) == 0 {
		return nil
	}
	v4 := ParseProcNetTCP(out, false)
	v6 := ParseProcNetTCP(out, true)
	return append(v4, v6...)
}

// RemoteScanner 持有探测方法缓存。零值可用。
type RemoteScanner struct {
	preferred methodKey
}

// NewRemoteScanner 构造一个 Scanner。
func NewRemoteScanner() *RemoteScanner { return &RemoteScanner{} }

// ErrNoMethod 表示所有探测方法均失败。
var ErrNoMethod = errors.New("no working remote port enumeration method")

// Scan 执行远端端口扫描。
//
// 首次调用按 methods 顺序尝试，第一个成功的方法（err == nil 且至少返回一条结果）
// 被缓存为 preferred；之后调用直接走 preferred，失败时退回未知态重新探测。
//
// 成功的判据是 "执行没出错"。空结果（远端确实没监听）也算成功。
func (s *RemoteScanner) Scan(ctx context.Context, ex Executor) ([]model.RemotePort, error) {
	// 先尝试缓存。
	if s.preferred != "" {
		if m, ok := lookupMethod(s.preferred); ok {
			out, err := ex.Run(ctx, m.cmd)
			if err == nil {
				return m.parse(out), nil
			}
			// 缓存失效 → 回退到全链探测。
			s.preferred = ""
		}
	}
	var lastErr error
	for _, m := range methods {
		out, err := ex.Run(ctx, m.cmd)
		if err != nil {
			lastErr = err
			continue
		}
		s.preferred = m.key
		return m.parse(out), nil
	}
	if lastErr == nil {
		lastErr = ErrNoMethod
	}
	return nil, errors.Join(ErrNoMethod, lastErr)
}

func lookupMethod(key methodKey) (methodSpec, bool) {
	for _, m := range methods {
		if m.key == key {
			return m, true
		}
	}
	return methodSpec{}, false
}

package forward

import (
	"context"
	"io"
	"net"
)

// bridge 双向拷贝 a <-> b，任一方向 EOF 或 ctx 取消则关闭两端。
// 返回拷贝的字节数（a→b, b→a）便于测试断言。
//
// TODO(M4): 实现 — 两个 goroutine + sync.WaitGroup + ctx cancel 关闭。
func bridge(ctx context.Context, a, b net.Conn) (int64, int64) {
	_ = ctx
	_ = a
	_ = b
	_ = io.Copy
	return 0, 0
}

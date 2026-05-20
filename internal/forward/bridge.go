package forward

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// bridge 双向拷贝 a <-> b，任一方向 EOF / 出错 / ctx 取消时关闭两端，
// 等待另一方向收尾后返回。返回拷贝字节数 (a→b, b→a)。
func bridge(ctx context.Context, a, b net.Conn) (int64, int64) {
	var ab, ba int64
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = a.Close()
			_ = b.Close()
		})
	}

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			closeBoth()
		case <-stop:
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		n, _ := io.Copy(b, a)
		atomic.StoreInt64(&ab, n)
		closeBoth() // EOF/err 触发对端唤醒
	}()
	go func() {
		defer wg.Done()
		n, _ := io.Copy(a, b)
		atomic.StoreInt64(&ba, n)
		closeBoth()
	}()

	wg.Wait()
	close(stop)
	return atomic.LoadInt64(&ab), atomic.LoadInt64(&ba)
}

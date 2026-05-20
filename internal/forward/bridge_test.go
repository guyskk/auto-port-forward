package forward

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// fakeConn 包装 net.Pipe 的一端，让上层可以用 buffer 模拟读写。
// 这里直接用 net.Pipe — 它是同步的内存连接，正适合 bridge 测试。

func TestBridge_copiesBothDirections(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	defer a1.Close()
	defer a2.Close()
	defer b1.Close()
	defer b2.Close()

	// bridge a2 <-> b1：写到 a1 的数据会从 b2 读到，反向亦然。
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var ab, ba int64
	go func() {
		ab, ba = bridge(ctx, a2, b1)
		close(done)
	}()

	// a → b 方向。
	want1 := []byte("hello from a")
	if _, err := a1.Write(want1); err != nil {
		t.Fatal(err)
	}
	buf1 := make([]byte, len(want1))
	if _, err := io.ReadFull(b2, buf1); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf1, want1) {
		t.Errorf("a→b got %q, want %q", buf1, want1)
	}

	// b → a 方向。
	want2 := []byte("hello from b")
	if _, err := b2.Write(want2); err != nil {
		t.Fatal(err)
	}
	buf2 := make([]byte, len(want2))
	if _, err := io.ReadFull(a1, buf2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf2, want2) {
		t.Errorf("b→a got %q, want %q", buf2, want2)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not exit on ctx cancel")
	}
	if ab < int64(len(want1)) || ba < int64(len(want2)) {
		t.Errorf("byte counts ab=%d ba=%d", ab, ba)
	}
}

func TestBridge_closesWhenOneSideEOF(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	defer b1.Close()
	defer b2.Close()

	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		bridge(ctx, a2, b1)
		close(done)
	}()

	// 关闭 a1 → a2 读 EOF → bridge 应整个收尾。
	a1.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not exit when a1 closed")
	}
	a2.Close()
}

func TestBridge_returnsOnContextCancel(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	defer a1.Close()
	defer a2.Close()
	defer b1.Close()
	defer b2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		bridge(ctx, a2, b1)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not return on ctx cancel")
	}
}

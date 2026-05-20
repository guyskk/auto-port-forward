package events

import (
	"context"
	"testing"
)

// 反映新契约（M5 修复）：WailsEmitter 必须持有 wails Startup 提供的 ctx，
// 调用方传的 ctx 一律忽略。当持有的 ctx 缺失或没有 wails "events" key 时，
// Emit 应静默 short-circuit —— 不能 panic、也不能触发 wails runtime 的 log.Fatalf。
//
// 这条契约让 engine 可以在任何 goroutine 里用任意派生 ctx 调用 emit.Emit，
// 而不必担心 wails runtime 拒绝 ctx chain。

func TestWailsEmitter_NilCtor_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	w := NewWailsEmitter(nil)
	w.Emit(context.Background(), "x", 1)
}

func TestWailsEmitter_CtxMissingEventsKey_DoesNotCallRuntime(t *testing.T) {
	// 没有 events key 时 runtime.EventsEmit 会 log.Fatalf 杀掉进程。
	// 这里要求 WailsEmitter 自己检测并跳过。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	ctx := context.Background()
	w := NewWailsEmitter(ctx)
	w.Emit(nil, "x", 1) // 调用方传 nil ctx 不影响
}

func TestWailsEmitter_IgnoresCallerCtx(t *testing.T) {
	// 调用方传的 ctx 被忽略 —— 即便是带 events key 的也不该被使用。
	// 我们通过：构造时传 "无 events" ctx，调用时传 "看似有 events" 的 ctx —
	// 仍然要 short-circuit（说明用的是构造时的）。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	w := NewWailsEmitter(context.Background()) // 构造 ctx 无 events key
	//nolint:staticcheck // 测试场景需用裸 string key
	callerCtx := context.WithValue(context.Background(), "events", "looks-real")
	w.Emit(callerCtx, "x", 1)
}

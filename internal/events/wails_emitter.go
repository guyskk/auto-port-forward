// wails_emitter.go 提供 wails runtime 适配器，把 events.Emitter 接口桥到 runtime.EventsEmit。
//
// 仅在 main 包构建（嵌入 wails runtime）时被实际使用；测试场景仍可用 NopEmitter。
package events

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// WailsEmitter 把事件发射委托给 wails runtime。
//
// wails runtime.EventsEmit 要求 ctx 是 OnStartup 注入的那一个原始 ctx（要能 ctx.Value("events")
// 取到 wails 内部的 Events 适配器）。engine 内部的 goroutine 通常在派生 ctx 上跑，
// 直接把派生 ctx 传给 wails 会触发 `log.Fatalf("An invalid context was passed.")`，杀死进程。
//
// 解决：构造时记住 Startup 提供的原始 ctx，所有 Emit 都用它；调用方传的 ctx 被忽略。
type WailsEmitter struct {
	ctx context.Context
}

// NewWailsEmitter 用 wails Startup 时拿到的 ctx 构造。ctx 为 nil 时 Emit 静默 no-op。
func NewWailsEmitter(ctx context.Context) *WailsEmitter {
	return &WailsEmitter{ctx: ctx}
}

// Emit 实现 Emitter 接口。调用方 ctx 一律忽略，使用构造时持有的 wails ctx。
// 当持有 ctx 缺失或不含 wails "events" key（例如还没 Startup）时，静默 short-circuit。
//
//nolint:staticcheck // wails runtime 用裸 string 作 ctx key
func (w *WailsEmitter) Emit(_ context.Context, name string, data any) {
	if w == nil || w.ctx == nil {
		return
	}
	if w.ctx.Value("events") == nil {
		// wails runtime 还没准备好（测试 / 启动早期 / 已 Shutdown）。
		return
	}
	runtime.EventsEmit(w.ctx, name, data)
}

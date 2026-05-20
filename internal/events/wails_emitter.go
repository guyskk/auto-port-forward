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
// 必须使用 wails 在 OnStartup 注入的 ctx 才能正确路由到前端；其他 ctx 会被 runtime 丢弃。
type WailsEmitter struct{}

// Emit 实现 Emitter 接口。
func (WailsEmitter) Emit(ctx context.Context, name string, data any) {
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, name, data)
}

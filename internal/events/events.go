// Package events 定义事件名常量与 Emitter 接口。
// 接口隔离 wails runtime，便于 engine 单测注入 fake emitter。
package events

import "context"

// 事件名常量 — 前后端共享。
const (
	EventStateUpdate   = "state:update"    // 转发快照变化
	EventServerStatus  = "server:status"   // 某 server 连接状态变化
	EventScanError     = "scan:error"      // 扫描失败
	EventForwardUpdate = "forward:update"  // 单条转发状态变化
)

// ServerStatus 是 EventServerStatus 事件的 payload。
// State 取值对齐 sshpool.State：dialing / connected / broken / degraded。
type ServerStatus struct {
	ServerID       string `json:"server_id"`
	State          string `json:"state"`
	Attempt        int    `json:"attempt"`
	DisconnectedMs int64  `json:"disconnected_ms"`
	Error          string `json:"error,omitempty"`
}

// Emitter 抽象事件发射器；wails 实现包装 runtime.EventsEmit。
type Emitter interface {
	Emit(ctx context.Context, name string, data any)
}

// NopEmitter 用于测试和未启动 wails runtime 时占位。
type NopEmitter struct{}

// Emit 忽略所有事件。
func (NopEmitter) Emit(ctx context.Context, name string, data any) {
	_, _, _ = ctx, name, data
}

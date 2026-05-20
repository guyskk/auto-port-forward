package sshpool

import "time"

// BackoffParams 用于参数化重连退避策略，便于单测。
type BackoffParams struct {
	Initial  time.Duration // 默认 500ms
	Max      time.Duration // 默认 60s
	Degraded time.Duration // 累计断开多久后标记 degraded，默认 15min
}

// DefaultBackoff 是默认参数。
func DefaultBackoff() BackoffParams {
	return BackoffParams{
		Initial:  500 * time.Millisecond,
		Max:      60 * time.Second,
		Degraded: 15 * time.Minute,
	}
}

// NextDelay 返回第 attempt 次重连等待的时长（attempt 从 0 起算）。
// 算法：Initial * 2^attempt，封顶 Max。纯函数，便于单测。
//
// TODO(M4): 实现 — 单测覆盖 attempt=0 / 翻倍 / 封顶 / 负值兜底。
func NextDelay(p BackoffParams, attempt int) time.Duration {
	_ = p
	_ = attempt
	return 0
}

// IsDegraded 判断累计断开时长是否已经触发 degraded。
// TODO(M4): 实现 — disconnected > p.Degraded。
func IsDegraded(p BackoffParams, disconnected time.Duration) bool {
	_ = p
	_ = disconnected
	return false
}

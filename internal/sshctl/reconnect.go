package sshctl

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
// 算法：Initial * 2^attempt，封顶 Max。负数 attempt 当作 0。
//
// 该函数纯函数，无 I/O，便于单测。
func NextDelay(p BackoffParams, attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	// 上限：先用左移检测溢出，超出 Max 直接返回 Max。
	// 32 次翻倍 ≈ 5.7e10 倍 Initial，远超任何合理 Max。
	if attempt > 32 {
		return p.Max
	}
	d := p.Initial << uint(attempt)
	if d <= 0 || d > p.Max {
		return p.Max
	}
	return d
}

// IsDegraded 判断累计断开时长是否已经触发 degraded。
func IsDegraded(p BackoffParams, disconnected time.Duration) bool {
	return disconnected > p.Degraded
}

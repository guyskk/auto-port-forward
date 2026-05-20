// Package conflict 根据规则与本地占用情况计算每个远端端口的状态。
//
// Classify 是纯函数 — 不发起任何 I/O，便于单元测试覆盖各分支。
package conflict

import (
	"autoportforward/internal/config"
	"autoportforward/internal/model"
)

// Input 是 Classify 的入参集合。
type Input struct {
	Remote        model.RemotePort
	LocalPort     int
	LocalOccupied bool   // 本地同号端口是否被占用
	OccupiedBySelf bool  // 该占用是否就是本程序自己的 listen
	IsRoot        bool   // 当前进程是否以 root 运行
	Rules         config.Rules
}

// Classify 返回端口应当被赋予的 PortStatus。优先级从高到低：
//  1. 命中 ExcludePorts/Ranges 或 OnlyPublicBind && bind ∈ 回环   → excluded
//  2. localPort < 1024 && !isRoot                                 → conflict_priv
//  3. localOccupied && !occupiedBySelf                            → conflict
//  4. 默认                                                        → pending
//
// 注：StatusForwarding 由调用方在 Listen 成功后翻转，不在此处返回。
// TODO(M3): 实现以上规则，本文件需有 conflict_test.go 完整覆盖。
func Classify(in Input) model.PortStatus {
	_ = in
	return model.StatusPending
}

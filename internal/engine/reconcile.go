package engine

import (
	"autoportforward/internal/conflict"
	"autoportforward/internal/config"
	"autoportforward/internal/model"
)

// LocalOwnership 记录本地某端口的占用情况，由 engine 在调度前预计算。
type LocalOwnership struct {
	Occupied bool // 是否被占用
	BySelf   bool // 占用者是否就是本程序自己的 forward 监听
}

// Inputs 是 Reconcile 的所有依赖。
type Inputs struct {
	ServerID       string
	Remote         []model.RemotePort       // 本轮扫到的远端 LISTEN 端口
	LocalOccupied  map[int]LocalOwnership   // local port → 占用情况
	CurrentForward map[int]bool             // remote port → 是否已有 forward 正在跑
	Rules          config.Rules
	IsRoot         bool
}

// Outputs 是 Reconcile 的结果。
type Outputs struct {
	Snapshot     []model.Forward // 给 UI 的快照
	DesiredPorts []int           // 本轮应当存在的 forward 端口集合（remote port 维度）
	Diff         []DiffOp        // 相对 CurrentForward 的增删指令
}

// Reconcile 是 engine 的核心纯函数：
// 给定本轮扫描结果与规则，计算每个端口的状态、应有的转发集合、相对当前的迁移。
//
// 该函数不发起任何 I/O，便于在不启动真实 ssh/forward 的情况下做完整业务单测。
func Reconcile(in Inputs) Outputs {
	out := Outputs{}
	desiredSet := make(map[int]struct{})

	for _, r := range in.Remote {
		localPort := r.Port + in.Rules.LocalPortOffset
		// TODO(M6+): 同一 remote.Port 在 IPv4 0.0.0.0 + IPv6 [::] 上各报一次时，会产出两条
		// Snapshot 条目（desiredSet 已去重，forward 只启一次，但 UI 表格显示重复）。
		// 修复方向：按 (ServerID, RemotePort) 聚合后取 BindAddr 优先级最高的一条。
		own := in.LocalOccupied[localPort]
		// 端口已经在跑 → 视为 forwarding，且占用就是自己 — 此时 LocalOccupied 标记可能不准，强制 BySelf。
		alreadyRunning := in.CurrentForward[r.Port]
		if alreadyRunning {
			own = LocalOwnership{Occupied: true, BySelf: true}
		}
		st := conflict.Classify(conflict.Input{
			Remote:         r,
			LocalPort:      localPort,
			LocalOccupied:  own.Occupied,
			OccupiedBySelf: own.BySelf,
			IsRoot:         in.IsRoot,
			Rules:          in.Rules,
		})
		if alreadyRunning && st == model.StatusPending {
			st = model.StatusForwarding
		}
		f := model.Forward{
			ServerID:   in.ServerID,
			RemotePort: r.Port,
			LocalPort:  localPort,
			Status:     st,
			Remote:     r,
		}
		out.Snapshot = append(out.Snapshot, f)
		if st == model.StatusPending || st == model.StatusForwarding {
			desiredSet[r.Port] = struct{}{}
		}
	}

	out.DesiredPorts = setToSortedSlice(desiredSet)
	current := keysAsSlice(in.CurrentForward)
	out.Diff = Diff(current, out.DesiredPorts)
	return out
}

func setToSortedSlice(s map[int]struct{}) []int {
	if len(s) == 0 {
		return nil
	}
	out := make([]int, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sortInts(out)
	return out
}

func keysAsSlice(m map[int]bool) []int {
	if len(m) == 0 {
		return nil
	}
	out := make([]int, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return out
}

func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}

package engine

import (
	"auto-port-forward/internal/config"
	"auto-port-forward/internal/conflict"
	"auto-port-forward/internal/model"
)

// LocalOwnership 记录本地某端口的占用情况，由 engine 在调度前预计算。
type LocalOwnership struct {
	Occupied bool // 是否被占用
	BySelf   bool // 占用者是否就是本程序自己的 forward 监听
}

// Inputs 是 Reconcile 的所有依赖。
type Inputs struct {
	ServerID       string
	Remote         []model.RemotePort     // 本轮扫到的远端 LISTEN 端口
	LocalOccupied  map[int]LocalOwnership // local port → 占用情况
	CurrentForward map[int]bool           // remote port → 是否已有 forward 正在跑
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
//
// 本地端口固定与远端端口相同（不再支持 LocalPortOffset）。
func Reconcile(in Inputs) Outputs {
	out := Outputs{}
	desiredSet := make(map[int]struct{})

	for _, r := range dedupRemote(in.Remote) {
		localPort := r.Port
		own := in.LocalOccupied[localPort]
		// 端口已经在跑 → 视为 forwarding，且占用就是自己 — 此时 LocalOccupied 标记可能不准，强制 BySelf。
		alreadyRunning := in.CurrentForward[r.Port]
		if alreadyRunning {
			own = LocalOwnership{Occupied: true, BySelf: true}
		}
		st := conflict.Classify(conflict.Input{
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

// dedupRemote 把同一端口在多个 BindAddr 上的重复条目合并为一条，
// 取 BindAddr 优先级最高者，保持端口首次出现的顺序。
// 典型场景：同一服务同时在 IPv4 0.0.0.0 与 IPv6 [::] 监听，扫描会各报一条，
// 但 forward 只需启一次，UI 也只应显示一行。
func dedupRemote(remote []model.RemotePort) []model.RemotePort {
	if len(remote) == 0 {
		return nil
	}
	idxByPort := make(map[int]int, len(remote))
	out := make([]model.RemotePort, 0, len(remote))
	for _, r := range remote {
		if i, ok := idxByPort[r.Port]; ok {
			if bindAddrPriority(r.BindAddr) > bindAddrPriority(out[i].BindAddr) {
				out[i] = r
			}
			continue
		}
		idxByPort[r.Port] = len(out)
		out = append(out, r)
	}
	return out
}

// bindAddrPriority 返回 BindAddr 的展示优先级，数值越大越优先。
// 偏好可对外的通配地址（0.0.0.0 > ::），其次环回（127.0.0.1 > ::1）。
func bindAddrPriority(addr string) int {
	switch addr {
	case "0.0.0.0":
		return 4
	case "::":
		return 3
	case "127.0.0.1":
		return 2
	case "::1":
		return 1
	default:
		return 0
	}
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

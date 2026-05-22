package engine

import (
	"reflect"
	"sort"
	"testing"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/model"
)

// 辅助：构造一个简单 RemotePort。
func rp(port int, bind string) model.RemotePort {
	return model.RemotePort{Port: port, BindAddr: bind, IPVersion: "IPv4"}
}

func sortFwds(fs []model.Forward) []model.Forward {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].RemotePort < fs[j].RemotePort })
	return fs
}

func TestReconcile_pendingForNewPort(t *testing.T) {
	out := Reconcile(Inputs{
		ServerID:       "ubt",
		Remote:         []model.RemotePort{rp(9527, "0.0.0.0")},
		LocalOccupied:  nil,
		CurrentForward: nil,
		Rules:          config.Rules{},
		IsRoot:         false,
	})
	if len(out.Snapshot) != 1 || out.Snapshot[0].Status != model.StatusPending {
		t.Errorf("snapshot=%#v", out.Snapshot)
	}
	if len(out.DesiredPorts) != 1 || out.DesiredPorts[0] != 9527 {
		t.Errorf("desired=%#v", out.DesiredPorts)
	}
	// localPort 必须等于 remotePort（不再支持 offset）
	if out.Snapshot[0].LocalPort != 9527 {
		t.Errorf("localPort = %d, want 9527", out.Snapshot[0].LocalPort)
	}
}

func TestReconcile_excludedDoesNotEnterDesired(t *testing.T) {
	out := Reconcile(Inputs{
		ServerID: "ubt",
		Remote:   []model.RemotePort{rp(22, "0.0.0.0"), rp(9527, "0.0.0.0")},
		Rules:    config.Rules{ExcludePorts: []int{22}},
	})
	got := sortFwds(out.Snapshot)
	if len(got) != 2 {
		t.Fatalf("snapshot len=%d", len(got))
	}
	var port22Status, port9527Status model.PortStatus
	for _, f := range got {
		if f.RemotePort == 22 {
			port22Status = f.Status
		}
		if f.RemotePort == 9527 {
			port9527Status = f.Status
		}
	}
	if port22Status != model.StatusExcluded {
		t.Errorf("22 → %q", port22Status)
	}
	if port9527Status != model.StatusPending {
		t.Errorf("9527 → %q", port9527Status)
	}
	if !reflect.DeepEqual(out.DesiredPorts, []int{9527}) {
		t.Errorf("desired = %v, want [9527]", out.DesiredPorts)
	}
}

func TestReconcile_conflictWhenLocalOccupiedByOther(t *testing.T) {
	out := Reconcile(Inputs{
		ServerID:      "ubt",
		Remote:        []model.RemotePort{rp(8080, "0.0.0.0")},
		LocalOccupied: map[int]LocalOwnership{8080: {Occupied: true, BySelf: false}},
	})
	if out.Snapshot[0].Status != model.StatusConflict {
		t.Errorf("status = %q", out.Snapshot[0].Status)
	}
	if len(out.DesiredPorts) != 0 {
		t.Errorf("desired should be empty for conflict, got %v", out.DesiredPorts)
	}
}

func TestReconcile_forwardingWhenAlreadyRunning(t *testing.T) {
	// 已有转发 → 状态应为 forwarding。
	out := Reconcile(Inputs{
		ServerID:       "ubt",
		Remote:         []model.RemotePort{rp(9527, "0.0.0.0")},
		CurrentForward: map[int]bool{9527: true},
	})
	if out.Snapshot[0].Status != model.StatusForwarding {
		t.Errorf("status = %q, want forwarding", out.Snapshot[0].Status)
	}
	if !reflect.DeepEqual(out.DesiredPorts, []int{9527}) {
		t.Errorf("desired = %v, want [9527]", out.DesiredPorts)
	}
}

func TestReconcile_diffComputesAddDel(t *testing.T) {
	out := Reconcile(Inputs{
		ServerID:       "ubt",
		Remote:         []model.RemotePort{rp(9527, "0.0.0.0"), rp(5432, "0.0.0.0")},
		CurrentForward: map[int]bool{5432: true, 8080: true}, // 8080 不在 remote → del
	})
	ops := sortOps(out.Diff)
	want := []DiffOp{
		{Kind: "add", Port: 9527},
		{Kind: "del", Port: 8080},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("diff = %#v, want %#v", ops, want)
	}
}

func TestReconcile_dedupSamePortIPv4AndIPv6(t *testing.T) {
	// 同一端口同时在 IPv4 0.0.0.0 与 IPv6 [::] 监听：扫描会各报一条，
	// Snapshot 应聚合为一条，取 BindAddr 优先级最高者（0.0.0.0）。
	out := Reconcile(Inputs{
		ServerID: "ubt",
		Remote: []model.RemotePort{
			{Port: 8080, BindAddr: "::", IPVersion: "IPv6"},
			{Port: 8080, BindAddr: "0.0.0.0", IPVersion: "IPv4"},
		},
	})
	if len(out.Snapshot) != 1 {
		t.Fatalf("snapshot len = %d, want 1: %#v", len(out.Snapshot), out.Snapshot)
	}
	if out.Snapshot[0].Remote.BindAddr != "0.0.0.0" {
		t.Errorf("chosen bind = %q, want 0.0.0.0", out.Snapshot[0].Remote.BindAddr)
	}
	if !reflect.DeepEqual(out.DesiredPorts, []int{8080}) {
		t.Errorf("desired = %v, want [8080]", out.DesiredPorts)
	}
}

func TestReconcile_dedupKeepsDistinctPorts(t *testing.T) {
	// 不同端口不应被合并。
	out := Reconcile(Inputs{
		ServerID: "ubt",
		Remote: []model.RemotePort{
			rp(8080, "0.0.0.0"),
			rp(9090, "0.0.0.0"),
		},
	})
	if len(out.Snapshot) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(out.Snapshot))
	}
}

func TestReconcile_dedupPrefersWildcardOverLoopback(t *testing.T) {
	// 同端口 127.0.0.1 + 0.0.0.0：取 0.0.0.0（可对外）。
	out := Reconcile(Inputs{
		ServerID: "ubt",
		Remote: []model.RemotePort{
			{Port: 3000, BindAddr: "127.0.0.1", IPVersion: "IPv4"},
			{Port: 3000, BindAddr: "0.0.0.0", IPVersion: "IPv4"},
		},
	})
	if len(out.Snapshot) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(out.Snapshot))
	}
	if out.Snapshot[0].Remote.BindAddr != "0.0.0.0" {
		t.Errorf("chosen bind = %q, want 0.0.0.0", out.Snapshot[0].Remote.BindAddr)
	}
}

func TestReconcile_privilegedConflictNonRoot(t *testing.T) {
	out := Reconcile(Inputs{
		ServerID: "ubt",
		Remote:   []model.RemotePort{rp(80, "0.0.0.0")},
		IsRoot:   false,
	})
	if out.Snapshot[0].Status != model.StatusConflictPriv {
		t.Errorf("status = %q, want conflict_priv", out.Snapshot[0].Status)
	}
	if len(out.DesiredPorts) != 0 {
		t.Errorf("desired should be empty for conflict_priv, got %v", out.DesiredPorts)
	}
}

// 禁用端口：UI 上仍然可见，但状态为 excluded，不进 DesiredPorts。
func TestReconcile_disabledPortBecomesExcluded(t *testing.T) {
	out := Reconcile(Inputs{
		ServerID:      "ubt",
		Remote:        []model.RemotePort{rp(8080, "0.0.0.0"), rp(9090, "0.0.0.0")},
		DisabledPorts: map[int]bool{8080: true},
		IsRoot:        true,
	})
	got := sortFwds(out.Snapshot)
	if len(got) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(got))
	}
	var s8080, s9090 model.PortStatus
	for _, f := range got {
		if f.RemotePort == 8080 {
			s8080 = f.Status
		}
		if f.RemotePort == 9090 {
			s9090 = f.Status
		}
	}
	if s8080 != model.StatusExcluded {
		t.Errorf("8080 status = %q, want excluded", s8080)
	}
	if s9090 != model.StatusPending {
		t.Errorf("9090 status = %q, want pending", s9090)
	}
	if !reflect.DeepEqual(out.DesiredPorts, []int{9090}) {
		t.Errorf("desired = %v, want [9090]", out.DesiredPorts)
	}
}

// 禁用某端口同时它当前在跑 → 必须 diff 出 del，以便 engine 停掉它。
func TestReconcile_disabledRunningPortGetsDeleted(t *testing.T) {
	out := Reconcile(Inputs{
		ServerID:       "ubt",
		Remote:         []model.RemotePort{rp(8080, "0.0.0.0")},
		CurrentForward: map[int]bool{8080: true},
		DisabledPorts:  map[int]bool{8080: true},
		IsRoot:         true,
	})
	if out.Snapshot[0].Status != model.StatusExcluded {
		t.Errorf("status = %q, want excluded", out.Snapshot[0].Status)
	}
	if len(out.Diff) != 1 || out.Diff[0].Kind != "del" || out.Diff[0].Port != 8080 {
		t.Errorf("diff = %#v, want one del op for 8080", out.Diff)
	}
}

// 禁用列表优先于其他状态（即使被本地占用，也是禁用而不是 conflict —— 用户意图覆盖一切）。
func TestReconcile_disabledTakesPrecedenceOverConflict(t *testing.T) {
	out := Reconcile(Inputs{
		ServerID:      "ubt",
		Remote:        []model.RemotePort{rp(8080, "0.0.0.0")},
		LocalOccupied: map[int]LocalOwnership{8080: {Occupied: true, BySelf: false}},
		DisabledPorts: map[int]bool{8080: true},
		IsRoot:        true,
	})
	if out.Snapshot[0].Status != model.StatusExcluded {
		t.Errorf("status = %q, want excluded", out.Snapshot[0].Status)
	}
}

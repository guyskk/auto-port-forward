package scan

import (
	"testing"
)

// 真实 ss -H -tlnp 样本（取自 miniubt / ubt）。
const ssSampleBasic = `LISTEN 0      511        127.0.0.1:41548 0.0.0.0:* users:(("node",pid=20703,fd=50))
LISTEN 0      511          0.0.0.0:9527  0.0.0.0:* users:(("MainThread",pid=1106083,fd=29))
LISTEN 0      4096         0.0.0.0:9308  0.0.0.0:*
LISTEN 0      4096            [::]:22       [::]:*
LISTEN 0      4096               *:9097        *:* users:(("mihomo",pid=2401,fd=3))
LISTEN 0      4096           [::1]:631      [::]:*
LISTEN 0      4096   127.0.0.53%lo:53    0.0.0.0:*
`

func findPort(rows []rPort, port int) (rPort, bool) {
	for _, p := range rows {
		if p.Port == port {
			return p, true
		}
	}
	return rPort{}, false
}

// rPort 是为测试方便定义的中间结构 — 直接断言 ParseSS 返回 model.RemotePort 各字段。
// 实际函数返回 []model.RemotePort；这里只是为了减少 import 噪音。
type rPort = struct {
	Port      int
	BindAddr  string
	IPVersion string
	PID       int
	Process   string
}

func toRows(t *testing.T, out []byte) []rPort {
	t.Helper()
	rps := ParseSS(out)
	rows := make([]rPort, 0, len(rps))
	for _, r := range rps {
		rows = append(rows, rPort{
			Port: r.Port, BindAddr: r.BindAddr, IPVersion: r.IPVersion,
			PID: r.PID, Process: r.Process,
		})
	}
	return rows
}

func TestParseSS_basicLoopbackWithProcess(t *testing.T) {
	rows := toRows(t, []byte(ssSampleBasic))
	r, ok := findPort(rows, 41548)
	if !ok {
		t.Fatalf("expected port 41548 in rows: %#v", rows)
	}
	if r.BindAddr != "127.0.0.1" {
		t.Errorf("bind addr = %q, want 127.0.0.1", r.BindAddr)
	}
	if r.IPVersion != "IPv4" {
		t.Errorf("ip version = %q, want IPv4", r.IPVersion)
	}
	if r.Process != "node" || r.PID != 20703 {
		t.Errorf("process = %q pid = %d, want (node, 20703)", r.Process, r.PID)
	}
}

func TestParseSS_v4PublicBindWithProcess(t *testing.T) {
	rows := toRows(t, []byte(ssSampleBasic))
	r, ok := findPort(rows, 9527)
	if !ok {
		t.Fatalf("expected port 9527")
	}
	if r.BindAddr != "0.0.0.0" {
		t.Errorf("bind = %q, want 0.0.0.0", r.BindAddr)
	}
	if r.Process != "MainThread" || r.PID != 1106083 {
		t.Errorf("process = %q pid = %d, want (MainThread, 1106083)", r.Process, r.PID)
	}
}

func TestParseSS_publicBindWithoutUsers(t *testing.T) {
	rows := toRows(t, []byte(ssSampleBasic))
	r, ok := findPort(rows, 9308)
	if !ok {
		t.Fatalf("expected port 9308")
	}
	if r.BindAddr != "0.0.0.0" {
		t.Errorf("bind = %q", r.BindAddr)
	}
	if r.Process != "" || r.PID != 0 {
		t.Errorf("expected empty process/pid, got (%q, %d)", r.Process, r.PID)
	}
}

func TestParseSS_ipv6Sshd(t *testing.T) {
	rows := toRows(t, []byte(ssSampleBasic))
	r, ok := findPort(rows, 22)
	if !ok {
		t.Fatalf("expected port 22")
	}
	if r.BindAddr != "::" {
		t.Errorf("bind = %q, want ::", r.BindAddr)
	}
	if r.IPVersion != "IPv6" {
		t.Errorf("ip version = %q, want IPv6", r.IPVersion)
	}
}

func TestParseSS_dualStackStar(t *testing.T) {
	rows := toRows(t, []byte(ssSampleBasic))
	r, ok := findPort(rows, 9097)
	if !ok {
		t.Fatalf("expected port 9097")
	}
	// `*` 在 ss 输出里代表双栈 (v4 + v6)。
	if r.IPVersion != "dual" {
		t.Errorf("ip version = %q, want dual", r.IPVersion)
	}
	if r.BindAddr != "*" {
		t.Errorf("bind = %q", r.BindAddr)
	}
	if r.Process != "mihomo" || r.PID != 2401 {
		t.Errorf("process = (%q,%d)", r.Process, r.PID)
	}
}

func TestParseSS_ipv6Loopback(t *testing.T) {
	rows := toRows(t, []byte(ssSampleBasic))
	r, ok := findPort(rows, 631)
	if !ok {
		t.Fatalf("expected port 631")
	}
	if r.BindAddr != "::1" {
		t.Errorf("bind = %q, want ::1", r.BindAddr)
	}
	if r.IPVersion != "IPv6" {
		t.Errorf("ip version = %q, want IPv6", r.IPVersion)
	}
}

func TestParseSS_zoneIndexedLoopback(t *testing.T) {
	// systemd-resolved 监听 127.0.0.53%lo:53。bind 里带 %lo 区域索引。
	rows := toRows(t, []byte(ssSampleBasic))
	r, ok := findPort(rows, 53)
	if !ok {
		t.Fatalf("expected port 53")
	}
	if r.BindAddr != "127.0.0.53" {
		t.Errorf("bind = %q, want 127.0.0.53 (zone stripped)", r.BindAddr)
	}
	if r.IPVersion != "IPv4" {
		t.Errorf("ip version = %q, want IPv4", r.IPVersion)
	}
}

func TestParseSS_ipv6MappedV4(t *testing.T) {
	in := `LISTEN 0 128 [::ffff:127.0.0.1]:8080 [::]:* users:(("java",pid=99,fd=4))
`
	rows := toRows(t, []byte(in))
	r, ok := findPort(rows, 8080)
	if !ok {
		t.Fatalf("expected port 8080")
	}
	if r.BindAddr != "::ffff:127.0.0.1" {
		t.Errorf("bind = %q", r.BindAddr)
	}
	if r.Process != "java" || r.PID != 99 {
		t.Errorf("process = (%q, %d)", r.Process, r.PID)
	}
}

func TestParseSS_emptyInput(t *testing.T) {
	if got := ParseSS(nil); len(got) != 0 {
		t.Errorf("nil → %d rows, want 0", len(got))
	}
	if got := ParseSS([]byte("")); len(got) != 0 {
		t.Errorf("empty → %d rows, want 0", len(got))
	}
}

func TestParseSS_headerRowIsIgnored(t *testing.T) {
	// 用户万一不小心带上了表头：ss 没加 -H。第一列就不是 LISTEN，应跳过。
	in := `State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
LISTEN 0 128 127.0.0.1:5000 0.0.0.0:*
`
	rows := toRows(t, []byte(in))
	if len(rows) != 1 || rows[0].Port != 5000 {
		t.Errorf("rows = %#v, want one row port=5000", rows)
	}
}

func TestParseSS_skipNonListen(t *testing.T) {
	in := `ESTAB 0 0 127.0.0.1:5000 127.0.0.1:5001
LISTEN 0 128 127.0.0.1:7000 0.0.0.0:*
TIME-WAIT 0 0 127.0.0.1:5002 127.0.0.1:5003
`
	rows := toRows(t, []byte(in))
	if len(rows) != 1 || rows[0].Port != 7000 {
		t.Errorf("rows = %#v, want one row port=7000", rows)
	}
}

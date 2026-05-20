package scan

import "testing"

// /proc/net/tcp 真实样本（含表头）。
// 0100007F:A24C st=0A → 127.0.0.1:41548 LISTEN
// 00000000:2537 st=0A → 0.0.0.0:9527 LISTEN
// 3500007F:0035 st=0A → 127.0.0.53:53 LISTEN
const procV4Sample = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:A24C 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 90808 1 0000000000000000 100 0 0 10 0
   1: 00000000:2537 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 5043079 1 0000000000000000 100 0 0 10 0
   2: 3500007F:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000   991        0 10855 1 0000000000000000 100 0 0 10 5
   3: 0100007F:B377 00000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 91187 1 0000000000000000 100 0 0 10 0
`

// /proc/net/tcp6 真实样本。
// 00000000000000000000000001000000:0277 st=0A → ::1:631
// 00000000000000000000000000000000:0016 st=0A → :::22
// 0000000000000000FFFF00000100007F:1ED9 st=01 → 应该跳过 (非 LISTEN)
const procV6Sample = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:0277 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 5291378 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000000000000:0016 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 8088 1 0000000000000000 100 0 0 10 0
   2: 0000000000000000FFFF00000100007F:1ED9 0000000000000000FFFF00000100007F:C4FC 01 00000000:00000000 02:00000489 00000000  1000        0 5767653 2 0000000000000000 20 4 0 10 -1
`

func toProcRows(t *testing.T, out []byte, v6 bool) []rPort {
	t.Helper()
	rps := ParseProcNetTCP(out, v6)
	rows := make([]rPort, 0, len(rps))
	for _, r := range rps {
		rows = append(rows, rPort{
			Port: r.Port, BindAddr: r.BindAddr, IPVersion: r.IPVersion,
			PID: r.PID, Process: r.Process,
		})
	}
	return rows
}

func TestParseProc_v4Loopback(t *testing.T) {
	rows := toProcRows(t, []byte(procV4Sample), false)
	r, ok := findPort(rows, 41548)
	if !ok {
		t.Fatalf("expected port 41548 in rows: %#v", rows)
	}
	if r.BindAddr != "127.0.0.1" {
		t.Errorf("bind = %q, want 127.0.0.1", r.BindAddr)
	}
	if r.IPVersion != "IPv4" {
		t.Errorf("ip version = %q", r.IPVersion)
	}
}

func TestParseProc_v4Public(t *testing.T) {
	rows := toProcRows(t, []byte(procV4Sample), false)
	r, ok := findPort(rows, 9527)
	if !ok {
		t.Fatalf("expected port 9527")
	}
	if r.BindAddr != "0.0.0.0" {
		t.Errorf("bind = %q", r.BindAddr)
	}
}

func TestParseProc_skipsNonListen(t *testing.T) {
	rows := toProcRows(t, []byte(procV4Sample), false)
	if _, ok := findPort(rows, 0xB377); ok {
		t.Errorf("port 0xB377 (st=01) should not be LISTEN")
	}
}

func TestParseProc_v4DnsBind(t *testing.T) {
	// 0x35 = 53 (DNS), IP 是 3500007F → 127.0.0.53。
	rows := toProcRows(t, []byte(procV4Sample), false)
	r, ok := findPort(rows, 53)
	if !ok {
		t.Fatalf("expected port 53")
	}
	if r.BindAddr != "127.0.0.53" {
		t.Errorf("bind = %q, want 127.0.0.53", r.BindAddr)
	}
}

func TestParseProc_v6Loopback(t *testing.T) {
	rows := toProcRows(t, []byte(procV6Sample), true)
	r, ok := findPort(rows, 631)
	if !ok {
		t.Fatalf("expected port 631 in v6 rows: %#v", rows)
	}
	if r.BindAddr != "::1" {
		t.Errorf("bind = %q, want ::1", r.BindAddr)
	}
	if r.IPVersion != "IPv6" {
		t.Errorf("ip version = %q", r.IPVersion)
	}
}

func TestParseProc_v6AnyAddr(t *testing.T) {
	rows := toProcRows(t, []byte(procV6Sample), true)
	r, ok := findPort(rows, 22)
	if !ok {
		t.Fatalf("expected port 22 in v6 rows: %#v", rows)
	}
	if r.BindAddr != "::" {
		t.Errorf("bind = %q, want ::", r.BindAddr)
	}
}

func TestParseProc_v6SkipsNonListen(t *testing.T) {
	rows := toProcRows(t, []byte(procV6Sample), true)
	if _, ok := findPort(rows, 0x1ED9); ok {
		t.Errorf("port 0x1ED9 (st=01) should be skipped")
	}
}

func TestParseProc_emptyAndMalformed(t *testing.T) {
	if got := ParseProcNetTCP(nil, false); len(got) != 0 {
		t.Errorf("nil → %d rows", len(got))
	}
	bad := []byte("sl  local_address rem_address\nnonsense\n  0: BADIP:NOPORT garbage\n")
	if got := ParseProcNetTCP(bad, false); len(got) != 0 {
		t.Errorf("malformed → %d rows, want 0", len(got))
	}
}

package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// 前端期望 EnabledHosts() 返回 JSON `[]` 而非 `null`，避免 Vue 模板调用
// `.includes(alias)` 时在 null 上抛 TypeError 导致整个监控页空白。
func TestStore_EnabledHosts_nilSliceMarshalsAsEmptyJSONArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	got := s.EnabledHosts()
	if got == nil {
		t.Fatalf("EnabledHosts() must not return nil slice (Go nil → JSON null trips up frontend); got nil")
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("EnabledHosts JSON = %s, want []", b)
	}
}

// Snapshot() 内的所有 slice 字段必须非 nil，保证前端 TS 代码可以安全
// 调用数组方法（includes / map / filter）。
func TestStore_Snapshot_neverHasNilSlices(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	snap := s.Snapshot()
	if snap.EnabledHosts == nil {
		t.Errorf("Snapshot.EnabledHosts is nil; want []string{}")
	}
	if snap.Rules.ExcludePorts == nil {
		t.Errorf("Snapshot.Rules.ExcludePorts is nil; want []int{}")
	}
	if snap.Rules.ExcludeRanges == nil {
		t.Errorf("Snapshot.Rules.ExcludeRanges is nil; want []Span{}")
	}
}

// 前端 types.ts 期望字段名为 lowercase + underscore（scan_interval_sec / rules /
// enabled_hosts / exclude_ports / exclude_ranges）。Go 字段缺 JSON tag 时会
// 输出 PascalCase 字段名，导致 Settings 页读不到值。
func TestConfig_JSONFieldNames_areLowercaseUnderscore(t *testing.T) {
	c := DefaultConfig()
	c.EnabledHosts = []string{"ubt"}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	wantKeys := []string{
		`"scan_interval_sec"`,
		`"rules"`,
		`"enabled_hosts"`,
		`"exclude_ports"`,
		`"exclude_ranges"`,
	}
	for _, k := range wantKeys {
		if !strings.Contains(s, k) {
			t.Errorf("missing JSON key %s in: %s", k, s)
		}
	}
	unwantKeys := []string{
		`"ScanIntervalSec"`,
		`"Rules"`,
		`"EnabledHosts"`,
		`"ExcludePorts"`,
		`"ExcludeRanges"`,
	}
	for _, k := range unwantKeys {
		if strings.Contains(s, k) {
			t.Errorf("unexpected PascalCase JSON key %s in: %s", k, s)
		}
	}
}

// Span 也要有 JSON tag (lo / hi)。
func TestSpan_JSONFieldNames_areLowercase(t *testing.T) {
	b, err := json.Marshal(Span{Lo: 1000, Hi: 2000})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"lo":1000`) || !strings.Contains(s, `"hi":2000`) {
		t.Errorf("Span JSON = %s, want lo/hi", s)
	}
}

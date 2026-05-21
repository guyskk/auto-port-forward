package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig_hasExpectedDefaults(t *testing.T) {
	c := DefaultConfig()
	if c.ScanIntervalSec != 15 {
		t.Errorf("ScanIntervalSec = %d, want 15", c.ScanIntervalSec)
	}
	wantExcludes := map[int]bool{22: true, 53: true, 80: true, 443: true, 111: true, 631: true}
	for _, p := range c.Rules.ExcludePorts {
		delete(wantExcludes, p)
	}
	if len(wantExcludes) != 0 {
		t.Errorf("missing default excludes: %v", wantExcludes)
	}
	if c.EnabledHosts != nil {
		t.Errorf("EnabledHosts = %v, want nil", c.EnabledHosts)
	}
}

func TestLoad_returnsDefaultWhenMissing(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(filepath.Join(dir, "no.toml"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	def := DefaultConfig()
	if c.ScanIntervalSec != def.ScanIntervalSec {
		t.Errorf("missing-file Load did not return defaults")
	}
}

func TestSaveLoad_roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	in := Config{
		ScanIntervalSec: 7,
		Rules: Rules{
			ExcludePorts:  []int{22, 53},
			ExcludeRanges: []Span{{Lo: 9000, Hi: 9099}},
		},
		EnabledHosts: []string{"ubt", "prod-db"},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ScanIntervalSec != 7 {
		t.Errorf("ScanIntervalSec = %d", got.ScanIntervalSec)
	}
	if len(got.Rules.ExcludePorts) != 2 || got.Rules.ExcludePorts[0] != 22 {
		t.Errorf("ExcludePorts roundtrip lost: %#v", got.Rules.ExcludePorts)
	}
	if len(got.Rules.ExcludeRanges) != 1 || got.Rules.ExcludeRanges[0].Hi != 9099 {
		t.Errorf("ExcludeRanges roundtrip lost: %#v", got.Rules.ExcludeRanges)
	}
	if len(got.EnabledHosts) != 2 || got.EnabledHosts[0] != "ubt" || got.EnabledHosts[1] != "prod-db" {
		t.Errorf("EnabledHosts roundtrip lost: %#v", got.EnabledHosts)
	}
}

func TestSave_atomicTempFileIsCleaned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := Save(path, DefaultConfig()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Errorf("temp file leaked: %v", entries)
	}
}

func TestLoad_fillsDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// 只写 enabled_hosts，缺 scan_interval_sec 与 rules。
	min := `enabled_hosts = ["a", "b"]
`
	if err := os.WriteFile(path, []byte(min), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ScanIntervalSec != 15 {
		t.Errorf("ScanIntervalSec = %d, want defaulted 15", got.ScanIntervalSec)
	}
	if len(got.Rules.ExcludePorts) == 0 {
		t.Errorf("ExcludePorts should have defaults")
	}
	if len(got.EnabledHosts) != 2 {
		t.Errorf("EnabledHosts = %#v, want [a b]", got.EnabledHosts)
	}
}

func TestLoad_badTOMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("this = not = valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Errorf("expected error for bad TOML")
	}
}

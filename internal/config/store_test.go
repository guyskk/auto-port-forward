package config

import (
	"os"
	"path/filepath"
	"testing"
)

// NewStore 加载已有配置；缺失时用默认值并不立刻写盘。
func TestStore_NewStore_loadsExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := Save(path, Config{
		ScanIntervalSec: 9,
		EnabledHosts:    []string{"alpha"},
	}); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	got := s.Snapshot()
	if got.ScanIntervalSec != 9 {
		t.Errorf("ScanIntervalSec = %d, want 9", got.ScanIntervalSec)
	}
	if len(got.EnabledHosts) != 1 || got.EnabledHosts[0] != "alpha" {
		t.Errorf("EnabledHosts = %#v", got.EnabledHosts)
	}
}

// NewStore 路径不存在 → 使用默认值，不立刻创建文件。
func TestStore_NewStore_missingFileGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no.toml")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	got := s.Snapshot()
	def := DefaultConfig()
	if got.ScanIntervalSec != def.ScanIntervalSec {
		t.Errorf("ScanIntervalSec did not default: %d", got.ScanIntervalSec)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("NewStore must not create file when missing")
	}
}

// UpdateRules 替换并持久化。
func TestStore_UpdateRules_persists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.UpdateRules(Rules{ExcludePorts: []int{8080}}); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := Load(path)
	if len(reloaded.Rules.ExcludePorts) != 1 || reloaded.Rules.ExcludePorts[0] != 8080 {
		t.Errorf("rules not persisted: %#v", reloaded.Rules)
	}
}

// UpdateScanInterval 替换扫描周期秒数并持久化。
func TestStore_UpdateScanInterval_persists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.UpdateScanInterval(30); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := Load(path)
	if reloaded.ScanIntervalSec != 30 {
		t.Errorf("ScanIntervalSec = %d, want 30", reloaded.ScanIntervalSec)
	}
}

// UpdateScanInterval <=0 时强制改为 15。
func TestStore_UpdateScanInterval_zeroFallsBackTo15(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.UpdateScanInterval(0); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().ScanIntervalSec; got != 15 {
		t.Errorf("ScanIntervalSec = %d, want 15", got)
	}
}

// Snapshot 返回深拷贝，调用方修改不影响内部。
func TestStore_Snapshot_isDeepCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetHostEnabled("alpha", true)
	snap := s.Snapshot()
	snap.EnabledHosts[0] = "tampered"
	again := s.Snapshot()
	if again.EnabledHosts[0] == "tampered" {
		t.Errorf("Snapshot leaked internal state")
	}
}

// SetHostEnabled(on=true) 把别名加入启用集合并持久化。
func TestStore_SetHostEnabled_onPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.SetHostEnabled("ubt", true); err != nil {
		t.Fatalf("SetHostEnabled: %v", err)
	}
	reloaded, _ := Load(path)
	if len(reloaded.EnabledHosts) != 1 || reloaded.EnabledHosts[0] != "ubt" {
		t.Errorf("EnabledHosts not persisted: %#v", reloaded.EnabledHosts)
	}
}

// SetHostEnabled(on=true) 重复添加不会重复出现。
func TestStore_SetHostEnabled_onIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetHostEnabled("ubt", true)
	_ = s.SetHostEnabled("ubt", true)
	got := s.Snapshot().EnabledHosts
	if len(got) != 1 || got[0] != "ubt" {
		t.Errorf("EnabledHosts duplicated: %#v", got)
	}
}

// SetHostEnabled(on=false) 把别名移除并持久化。
func TestStore_SetHostEnabled_offRemoves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetHostEnabled("a", true)
	_ = s.SetHostEnabled("b", true)
	_ = s.SetHostEnabled("a", false)
	got := s.Snapshot().EnabledHosts
	if len(got) != 1 || got[0] != "b" {
		t.Errorf("EnabledHosts after off = %#v, want [b]", got)
	}
}

// SetHostEnabled(on=false) 对不存在的别名是 no-op，不报错。
func TestStore_SetHostEnabled_offMissingIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.SetHostEnabled("ghost", false); err != nil {
		t.Errorf("off-missing should not error: %v", err)
	}
}

// EnabledHosts() 返回拷贝，调用方修改不影响内部。
func TestStore_EnabledHosts_returnsCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetHostEnabled("a", true)
	got := s.EnabledHosts()
	got[0] = "tampered"
	if s.Snapshot().EnabledHosts[0] == "tampered" {
		t.Errorf("EnabledHosts leaked internal state")
	}
}

// 孤儿别名状态保留：ssh config 里这个 host 消失，APP 也不清掉它的启用状态；
// 同名再现自动恢复（语义上 store 不参与 ssh config 查询，只按字符串存集合）。
func TestStore_SetHostEnabled_orphanStatePreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetHostEnabled("transient-host", true)
	// 模拟"重启 APP" → 重新打开 store
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Snapshot().EnabledHosts
	if len(got) != 1 || got[0] != "transient-host" {
		t.Errorf("orphan state lost: %#v", got)
	}
}

// SetForwardEnabled(on=false) 把端口加入禁用列表并持久化。
func TestStore_SetForwardEnabled_offPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.SetForwardEnabled("ubt", 8080, false); err != nil {
		t.Fatalf("SetForwardEnabled: %v", err)
	}
	reloaded, _ := Load(path)
	got := reloaded.DisabledPorts["ubt"]
	if len(got) != 1 || got[0] != 8080 {
		t.Errorf("DisabledPorts not persisted: %#v", reloaded.DisabledPorts)
	}
}

// SetForwardEnabled(on=false) 重复禁用不会重复出现，且禁用列表升序排序。
func TestStore_SetForwardEnabled_offIsIdempotentAndSorted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetForwardEnabled("ubt", 9090, false)
	_ = s.SetForwardEnabled("ubt", 8080, false)
	_ = s.SetForwardEnabled("ubt", 9090, false) // 重复
	got := s.DisabledPorts("ubt")
	if len(got) != 2 || got[0] != 8080 || got[1] != 9090 {
		t.Errorf("DisabledPorts = %#v, want sorted [8080 9090] without duplicates", got)
	}
}

// SetForwardEnabled(on=true) 把端口从禁用列表中移除。
func TestStore_SetForwardEnabled_onRemoves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetForwardEnabled("ubt", 8080, false)
	_ = s.SetForwardEnabled("ubt", 9090, false)
	if err := s.SetForwardEnabled("ubt", 8080, true); err != nil {
		t.Fatal(err)
	}
	got := s.DisabledPorts("ubt")
	if len(got) != 1 || got[0] != 9090 {
		t.Errorf("DisabledPorts after on = %#v, want [9090]", got)
	}
}

// SetForwardEnabled(on=true) 对未禁用端口是 no-op，不报错。
func TestStore_SetForwardEnabled_onMissingIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.SetForwardEnabled("ubt", 8080, true); err != nil {
		t.Errorf("on-missing should not error: %v", err)
	}
	if got := s.DisabledPorts("ubt"); len(got) != 0 {
		t.Errorf("DisabledPorts should be empty, got %#v", got)
	}
}

// 多 alias 隔离：禁用 a 的端口不影响 b。
func TestStore_SetForwardEnabled_aliasIsolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetForwardEnabled("a", 8080, false)
	_ = s.SetForwardEnabled("b", 9090, false)
	if got := s.DisabledPorts("a"); len(got) != 1 || got[0] != 8080 {
		t.Errorf("a DisabledPorts = %#v", got)
	}
	if got := s.DisabledPorts("b"); len(got) != 1 || got[0] != 9090 {
		t.Errorf("b DisabledPorts = %#v", got)
	}
}

// 跨重启持久化：禁用列表能正常 round-trip。
func TestStore_SetForwardEnabled_orphanStatePreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetForwardEnabled("transient", 9000, false)
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.DisabledPorts("transient")
	if len(got) != 1 || got[0] != 9000 {
		t.Errorf("orphan disabled port lost: %#v", got)
	}
}

// DisabledPorts(alias) 返回拷贝，调用方修改不影响内部。
func TestStore_DisabledPorts_returnsCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetForwardEnabled("ubt", 8080, false)
	got := s.DisabledPorts("ubt")
	got[0] = 9999
	if s.DisabledPorts("ubt")[0] == 9999 {
		t.Errorf("DisabledPorts leaked internal state")
	}
}

// 当某 alias 的所有禁用端口都被启用回来后，map key 应被清除（避免空数组堆积）。
func TestStore_SetForwardEnabled_emptyAliasIsCleanedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_ = s.SetForwardEnabled("ubt", 8080, false)
	_ = s.SetForwardEnabled("ubt", 8080, true)
	snap := s.Snapshot()
	if _, ok := snap.DisabledPorts["ubt"]; ok {
		t.Errorf("empty alias should be removed from DisabledPorts, got %#v", snap.DisabledPorts)
	}
}

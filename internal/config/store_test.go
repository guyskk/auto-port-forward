package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 测试 1: NewStore 加载已有配置；缺失时用默认值并不立刻写盘。
func TestStore_NewStore_loadsExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := Save(path, Config{
		ScanIntervalSec: 9,
		Servers:         []Server{{ID: "abc", Host: "h", Port: 22, Enabled: true}},
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
	if len(got.Servers) != 1 || got.Servers[0].ID != "abc" {
		t.Errorf("Servers = %#v", got.Servers)
	}
}

// 测试 2: NewStore 路径不存在 → 使用默认值，不立刻创建文件。
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

// 测试 3: AddServer 给空 ID 自动生成 ID 并持久化到磁盘。
func TestStore_AddServer_generatesIDAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	created, err := s.AddServer(Server{Host: "host1", User: "u", Port: 22, Enabled: true})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if created.ID == "" {
		t.Errorf("ID was not generated")
	}
	// 验证磁盘内容。
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Servers) != 1 || reloaded.Servers[0].ID != created.ID {
		t.Errorf("persisted servers mismatch: %#v", reloaded.Servers)
	}
}

// 测试 4: AddServer 保留调用方提供的非空 ID。
func TestStore_AddServer_preservesProvidedID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	created, err := s.AddServer(Server{ID: "my-id", Host: "h", Port: 22})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "my-id" {
		t.Errorf("ID = %q, want preserved", created.ID)
	}
}

// 测试 5: AddServer 同 ID 重复返回错误。
func TestStore_AddServer_rejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_, _ = s.AddServer(Server{ID: "dup", Host: "h"})
	_, err := s.AddServer(Server{ID: "dup", Host: "h2"})
	if err == nil {
		t.Errorf("expected duplicate ID error")
	}
}

// 测试 6: UpdateServer 按 ID 替换。
func TestStore_UpdateServer_replacesByID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	created, _ := s.AddServer(Server{Host: "h1", Port: 22})
	created.Host = "h2"
	created.Port = 2222
	if err := s.UpdateServer(created); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}
	reloaded, _ := Load(path)
	if reloaded.Servers[0].Host != "h2" || reloaded.Servers[0].Port != 2222 {
		t.Errorf("update did not persist: %#v", reloaded.Servers[0])
	}
}

// 测试 7: UpdateServer 找不到 ID → 错误。
func TestStore_UpdateServer_missingIDReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.UpdateServer(Server{ID: "nope", Host: "h"}); err == nil {
		t.Errorf("expected not-found error")
	}
}

// 测试 8: DeleteServer 按 ID 删除。
func TestStore_DeleteServer_removesByID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	a, _ := s.AddServer(Server{Host: "h1"})
	b, _ := s.AddServer(Server{Host: "h2"})
	if err := s.DeleteServer(a.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	got := s.Snapshot().Servers
	if len(got) != 1 || got[0].ID != b.ID {
		t.Errorf("after delete, servers = %#v", got)
	}
}

// 测试 9: UpdateRules 替换并持久化。
func TestStore_UpdateRules_persists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	if err := s.UpdateRules(Rules{ExcludePorts: []int{8080}, LocalPortOffset: 20000}); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := Load(path)
	if reloaded.Rules.LocalPortOffset != 20000 {
		t.Errorf("rules not persisted: %#v", reloaded.Rules)
	}
}

// 测试 10: Snapshot 返回深拷贝，调用方修改不影响内部。
func TestStore_Snapshot_isDeepCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStore(path)
	_, _ = s.AddServer(Server{Host: "h1"})
	snap := s.Snapshot()
	snap.Servers[0].Host = "tampered"
	again := s.Snapshot()
	if again.Servers[0].Host == "tampered" {
		t.Errorf("Snapshot leaked internal state")
	}
}

// 测试 11: GenerateID 返回非空且包含字母数字。
func TestGenerateID_isNonEmpty(t *testing.T) {
	id := GenerateID()
	if id == "" {
		t.Errorf("GenerateID returned empty")
	}
	if strings.ContainsAny(id, " \t\n/") {
		t.Errorf("GenerateID returned invalid characters: %q", id)
	}
}

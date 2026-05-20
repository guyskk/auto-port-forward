package main

import (
	"context"
	"path/filepath"
	"testing"

	"autoportforward/internal/config"
	"autoportforward/internal/events"
)

func newTestApp() *App {
	a := NewApp()
	a.emit = events.NopEmitter{}
	return a
}

// 测试: AddServer + ListServers 端到端：新建 server → 持久化 → 重新加载能看到。
func TestApp_AddServer_persistsAndLists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	a := newTestApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer a.Shutdown(ctx)

	got, err := a.AddServer(config.Server{Host: "h1", User: "u", Enabled: true})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if got.ID == "" {
		t.Fatal("AddServer returned empty ID")
	}

	list := a.ListServers()
	if len(list) != 1 || list[0].ID != got.ID {
		t.Errorf("ListServers = %#v", list)
	}

	// 重启 App，确认持久化。
	a2 := newTestApp()
	if err := a2.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a2.Shutdown(ctx)
	if list2 := a2.ListServers(); len(list2) != 1 || list2[0].ID != got.ID {
		t.Errorf("after reload, ListServers = %#v", list2)
	}
}

// 测试: DeleteServer 清除持久化。
func TestApp_DeleteServer_removesFromConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	a := newTestApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)

	got, _ := a.AddServer(config.Server{Host: "h1"})
	if err := a.DeleteServer(got.ID); err != nil {
		t.Fatal(err)
	}
	if list := a.ListServers(); len(list) != 0 {
		t.Errorf("after delete, ListServers = %#v", list)
	}
	// 重新 setup 验证落盘。
	a2 := newTestApp()
	_ = a2.setup(ctx, path)
	defer a2.Shutdown(ctx)
	if list := a2.ListServers(); len(list) != 0 {
		t.Errorf("after reload, ListServers should be empty: %#v", list)
	}
}

// 测试: UpdateRules 落盘 + GetConfig 反映新规则。
func TestApp_UpdateRules_persists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	a := newTestApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)

	if err := a.UpdateRules(config.Rules{ExcludePorts: []int{8080}, LocalPortOffset: 5000}); err != nil {
		t.Fatal(err)
	}
	if got := a.GetConfig().Rules.LocalPortOffset; got != 5000 {
		t.Errorf("LocalPortOffset = %d", got)
	}
	a2 := newTestApp()
	_ = a2.setup(ctx, path)
	defer a2.Shutdown(ctx)
	if got := a2.GetConfig().Rules.LocalPortOffset; got != 5000 {
		t.Errorf("after reload, LocalPortOffset = %d", got)
	}
}

// 测试: TestServer 对不存在的 ID 返回错误。
func TestApp_TestServer_missingID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	a := newTestApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)

	if err := a.TestServer("nope"); err == nil {
		t.Errorf("expected error for missing server id")
	}
}

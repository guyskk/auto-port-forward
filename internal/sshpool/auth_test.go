package sshpool

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"auto-port-forward/internal/config"
)

func TestBuildAuthMethods_passwordMissing(t *testing.T) {
	_, err := buildAuthMethods(config.Server{AuthMethod: "password"})
	if !errors.Is(err, ErrMissingAuth) {
		t.Errorf("err = %v, want ErrMissingAuth", err)
	}
}

func TestBuildAuthMethods_passwordOK(t *testing.T) {
	got, err := buildAuthMethods(config.Server{AuthMethod: "password", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("len=%d, want 1", len(got))
	}
}

func TestBuildAuthMethods_keyMissing(t *testing.T) {
	_, err := buildAuthMethods(config.Server{AuthMethod: "ssh_key"})
	if !errors.Is(err, ErrMissingAuth) {
		t.Errorf("err = %v, want ErrMissingAuth", err)
	}
}

func TestBuildAuthMethods_keyOK(t *testing.T) {
	// 生成一把 ed25519 临时 key 写到磁盘。
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, block); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	methods, err := buildAuthMethods(config.Server{AuthMethod: "ssh_key", KeyPath: keyPath})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(methods) != 1 {
		t.Errorf("len=%d, want 1", len(methods))
	}
}

func TestBuildAuthMethods_unknownReturnsError(t *testing.T) {
	_, err := buildAuthMethods(config.Server{AuthMethod: "telepathy"})
	if !errors.Is(err, ErrMissingAuth) {
		t.Errorf("err = %v, want ErrMissingAuth", err)
	}
}

func TestBuildAuthMethods_agentNoSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := buildAuthMethods(config.Server{AuthMethod: "ssh_agent"})
	if !errors.Is(err, ErrMissingAuth) {
		t.Errorf("err = %v, want ErrMissingAuth", err)
	}
}

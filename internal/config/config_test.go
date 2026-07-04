package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFromEnv(t *testing.T) {
	runtimeDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("HOME", home)
	t.Setenv("WRAPPER_NOTIFY_BACKEND", "off")
	t.Setenv("WRAPPER_NOTIFY_CALL_TIMEOUT", "3s")
	t.Setenv("WRAPPER_NOTIFY_EXPIRE_TIMEOUT", "7s")
	t.Setenv("WRAPPER_PARENT_DEPTH", "2")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.ListenSocket != filepath.Join(runtimeDir, "bitwarden-ssh-agent-wrapper.sock") {
		t.Fatalf("ListenSocket = %q", cfg.ListenSocket)
	}
	if cfg.UpstreamSocket != filepath.Join(home, ".var", "app", "com.bitwarden.desktop", "data", ".bitwarden-ssh-agent.sock") {
		t.Fatalf("UpstreamSocket = %q", cfg.UpstreamSocket)
	}
	if cfg.NotifyBackend != "off" {
		t.Fatalf("NotifyBackend = %q", cfg.NotifyBackend)
	}
	if cfg.NotifyCallTimeout != 3*time.Second {
		t.Fatalf("NotifyCallTimeout = %s", cfg.NotifyCallTimeout)
	}
	if cfg.NotifyExpireTimeout != 7*time.Second {
		t.Fatalf("NotifyExpireTimeout = %s", cfg.NotifyExpireTimeout)
	}
	if cfg.ParentDepth != 2 {
		t.Fatalf("ParentDepth = %d", cfg.ParentDepth)
	}
}

func TestValidateRejectsUnsupportedNotifyBackend(t *testing.T) {
	cfg := Config{
		ListenSocket:        "/tmp/listen.sock",
		UpstreamSocket:      "/tmp/upstream.sock",
		NotifyBackend:       "notify-send",
		NotifyCallTimeout:   time.Second,
		NotifyExpireTimeout: time.Second,
		ParentDepth:         1,
		LogLevel:            DefaultLogLevel,
		LogFormat:           DefaultLogFormat,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("Validate() error = nil, want error")
	}
}

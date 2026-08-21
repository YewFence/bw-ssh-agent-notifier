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
	t.Setenv("WRAPPER_NOTIFY_FULL_PROCESS_TREE", "true")
	t.Setenv("WRAPPER_PARENT_DEPTH", "2")
	t.Setenv("WRAPPER_GATE", "confirm")
	t.Setenv("WRAPPER_GATE_TIMEOUT", "90s")
	t.Setenv("WRAPPER_CONTROL_SOCKET", "/tmp/control.sock")

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
	if !cfg.NotifyFullTree {
		t.Fatalf("NotifyFullTree = false, want true")
	}
	if cfg.ParentDepth != 2 {
		t.Fatalf("ParentDepth = %d", cfg.ParentDepth)
	}
	if cfg.GateMode != GateModeConfirm {
		t.Fatalf("GateMode = %q", cfg.GateMode)
	}
	if cfg.GateTimeout != 90*time.Second {
		t.Fatalf("GateTimeout = %s", cfg.GateTimeout)
	}
	if cfg.ControlSocket != "/tmp/control.sock" {
		t.Fatalf("ControlSocket = %q", cfg.ControlSocket)
	}
}

func TestFromEnvGateDefaults(t *testing.T) {
	runtimeDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("HOME", home)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.GateMode != DefaultGateMode {
		t.Fatalf("GateMode = %q, want %q", cfg.GateMode, DefaultGateMode)
	}
	if cfg.GateTimeout != DefaultGateTimeout {
		t.Fatalf("GateTimeout = %s, want %s", cfg.GateTimeout, DefaultGateTimeout)
	}
	if cfg.ControlSocket != filepath.Join(runtimeDir, "bitwarden-ssh-agent-wrapper-control.sock") {
		t.Fatalf("ControlSocket = %q", cfg.ControlSocket)
	}
}

func TestValidateRejectsUnsupportedGateMode(t *testing.T) {
	cfg := Config{
		ListenSocket:   "/tmp/listen.sock",
		UpstreamSocket: "/tmp/upstream.sock",
		NotifyBackend:  "off",
		ParentDepth:    1,
		GateMode:       "yolo",
		GateTimeout:    time.Minute,
		ControlSocket:  "/tmp/control.sock",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for unsupported gate mode")
	}
}

func TestValidateRejectsNonPositiveGateTimeout(t *testing.T) {
	cfg := Config{
		ListenSocket:   "/tmp/listen.sock",
		UpstreamSocket: "/tmp/upstream.sock",
		NotifyBackend:  "off",
		ParentDepth:    1,
		GateMode:       GateModeConfirm,
		GateTimeout:    0,
		ControlSocket:  "/tmp/control.sock",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for zero gate timeout")
	}
}

func TestValidateRequiresControlSocketInConfirmMode(t *testing.T) {
	cfg := Config{
		ListenSocket:   "/tmp/listen.sock",
		UpstreamSocket: "/tmp/upstream.sock",
		NotifyBackend:  "off",
		ParentDepth:    1,
		GateMode:       GateModeConfirm,
		GateTimeout:    time.Minute,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for missing control socket")
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

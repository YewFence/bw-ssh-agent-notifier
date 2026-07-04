package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	DefaultNotifyBackend = "dbus"
	DefaultNotifyTimeout = 2 * time.Second
	DefaultParentDepth   = 5
	DefaultLogLevel      = "info"
	DefaultLogFormat     = "text"
)

type Config struct {
	ListenSocket   string
	UpstreamSocket string
	NotifyBackend  string
	NotifyTimeout  time.Duration
	ParentDepth    int
	LogLevel       string
	LogFormat      string
}

func FromEnv() (Config, error) {
	cfg := Config{
		ListenSocket:   defaultListenSocket(),
		UpstreamSocket: defaultUpstreamSocket(),
		NotifyBackend:  envString("WRAPPER_NOTIFY_BACKEND", DefaultNotifyBackend),
		NotifyTimeout:  DefaultNotifyTimeout,
		ParentDepth:    DefaultParentDepth,
		LogLevel:       DefaultLogLevel,
		LogFormat:      DefaultLogFormat,
	}

	if value := os.Getenv("BITWARDEN_SSH_AGENT_SOCKET"); value != "" {
		cfg.UpstreamSocket = value
	}
	if value := os.Getenv("WRAPPER_SSH_AGENT_SOCKET"); value != "" {
		cfg.ListenSocket = value
	}
	if value := os.Getenv("WRAPPER_NOTIFY_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_NOTIFY_TIMEOUT: %w", err)
		}
		cfg.NotifyTimeout = timeout
	}
	if value := os.Getenv("WRAPPER_PARENT_DEPTH"); value != "" {
		depth, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_PARENT_DEPTH: %w", err)
		}
		cfg.ParentDepth = depth
	}

	return cfg, nil
}

func (cfg Config) Validate() error {
	if cfg.ListenSocket == "" {
		return errors.New("listen socket is required; set XDG_RUNTIME_DIR or WRAPPER_SSH_AGENT_SOCKET")
	}
	if cfg.UpstreamSocket == "" {
		return errors.New("upstream socket is required; set HOME or BITWARDEN_SSH_AGENT_SOCKET")
	}
	switch cfg.NotifyBackend {
	case "dbus", "off":
	default:
		return fmt.Errorf("unsupported notify backend %q", cfg.NotifyBackend)
	}
	if cfg.NotifyTimeout <= 0 {
		return errors.New("notify timeout must be positive")
	}
	if cfg.ParentDepth < 0 {
		return errors.New("parent depth must not be negative")
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported log level %q", cfg.LogLevel)
	}
	switch cfg.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("unsupported log format %q", cfg.LogFormat)
	}
	return nil
}

func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func defaultListenSocket() string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return ""
	}
	return filepath.Join(runtimeDir, "bitwarden-ssh-agent-wrapper.sock")
}

func defaultUpstreamSocket() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".var", "app", "com.bitwarden.desktop", "data", ".bitwarden-ssh-agent.sock")
}

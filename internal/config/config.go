package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultNotifyBackend       = "dbus"
	DefaultNotifyCallTimeout   = 2 * time.Second
	DefaultNotifyExpireTimeout = 4 * time.Second
	DefaultNotifyFullTree      = false
	DefaultParentDepth         = 5
	DefaultLogLevel            = "info"
	DefaultLogFormat           = "text"
	DefaultGateMode            = GateModeOff
	DefaultGateTimeout         = 60 * time.Second
	DefaultGateRememberTTL     = time.Minute
)

const (
	GateModeOff     = "off"
	GateModeConfirm = "confirm"
)

type Config struct {
	ListenSocket        string
	UpstreamSocket      string
	NotifyBackend       string
	NotifyCallTimeout   time.Duration
	NotifyExpireTimeout time.Duration
	NotifyFullTree      bool
	ParentDepth         int
	LogLevel            string
	LogFormat           string
	GateMode            string
	GateTimeout         time.Duration
	GateRememberTTL     time.Duration
	GateCommandGroups   [][]string
	ControlSocket       string
}

func FromEnv() (Config, error) {
	cfg := Config{
		ListenSocket:        defaultListenSocket(),
		UpstreamSocket:      defaultUpstreamSocket(),
		NotifyBackend:       envString("WRAPPER_NOTIFY_BACKEND", DefaultNotifyBackend),
		NotifyCallTimeout:   DefaultNotifyCallTimeout,
		NotifyExpireTimeout: DefaultNotifyExpireTimeout,
		NotifyFullTree:      DefaultNotifyFullTree,
		ParentDepth:         DefaultParentDepth,
		LogLevel:            DefaultLogLevel,
		LogFormat:           DefaultLogFormat,
		GateMode:            envString("WRAPPER_GATE", DefaultGateMode),
		GateTimeout:         DefaultGateTimeout,
		GateRememberTTL:     DefaultGateRememberTTL,
		ControlSocket:       defaultControlSocket(),
	}

	if value := os.Getenv("BITWARDEN_SSH_AGENT_SOCKET"); value != "" {
		cfg.UpstreamSocket = value
	}
	if value := os.Getenv("WRAPPER_SSH_AGENT_SOCKET"); value != "" {
		cfg.ListenSocket = value
	}
	if value := os.Getenv("WRAPPER_NOTIFY_CALL_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_NOTIFY_CALL_TIMEOUT: %w", err)
		}
		cfg.NotifyCallTimeout = timeout
	}
	if value := os.Getenv("WRAPPER_NOTIFY_EXPIRE_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_NOTIFY_EXPIRE_TIMEOUT: %w", err)
		}
		cfg.NotifyExpireTimeout = timeout
	}
	if value := os.Getenv("WRAPPER_NOTIFY_FULL_PROCESS_TREE"); value != "" {
		fullTree, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_NOTIFY_FULL_PROCESS_TREE: %w", err)
		}
		cfg.NotifyFullTree = fullTree
	}
	if value := os.Getenv("WRAPPER_PARENT_DEPTH"); value != "" {
		depth, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_PARENT_DEPTH: %w", err)
		}
		cfg.ParentDepth = depth
	}
	if value := os.Getenv("WRAPPER_GATE_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_GATE_TIMEOUT: %w", err)
		}
		cfg.GateTimeout = timeout
	}
	if value := os.Getenv("WRAPPER_GATE_REMEMBER_TTL"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_GATE_REMEMBER_TTL: %w", err)
		}
		cfg.GateRememberTTL = timeout
	}
	if value := os.Getenv("WRAPPER_GATE_COMMAND_GROUPS"); value != "" {
		groups, err := ParseGateCommandGroups(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WRAPPER_GATE_COMMAND_GROUPS: %w", err)
		}
		cfg.GateCommandGroups = groups
	}
	if value := os.Getenv("WRAPPER_CONTROL_SOCKET"); value != "" {
		cfg.ControlSocket = value
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
	if cfg.NotifyCallTimeout <= 0 {
		return errors.New("notify call timeout must be positive")
	}
	if cfg.NotifyExpireTimeout <= 0 {
		return errors.New("notify expire timeout must be positive")
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
	switch cfg.GateMode {
	case GateModeOff, GateModeConfirm:
	default:
		return fmt.Errorf("unsupported gate mode %q", cfg.GateMode)
	}
	if cfg.GateTimeout <= 0 {
		return errors.New("gate timeout must be positive")
	}
	if cfg.GateRememberTTL < 0 {
		return errors.New("gate remember TTL must not be negative")
	}
	for _, group := range cfg.GateCommandGroups {
		if len(group) == 0 || group[0] == "" {
			return errors.New("gate command group must contain an executable")
		}
		for _, argument := range group {
			if strings.Contains(argument, ",") {
				return errors.New("gate command group arguments must not contain commas")
			}
		}
		if !filepath.IsAbs(group[0]) && filepath.Base(group[0]) != group[0] {
			return errors.New("gate command group executable must be a name or an absolute path")
		}
	}
	if cfg.GateMode == GateModeConfirm && cfg.ControlSocket == "" {
		return errors.New("control socket is required when gate is enabled; set XDG_RUNTIME_DIR or WRAPPER_CONTROL_SOCKET")
	}
	return nil
}

func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func defaultRuntimeDir() string {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir
	}
	// pam_systemd normally exports XDG_RUNTIME_DIR for every login session,
	// but stripped environments (e.g. one-shot ssh commands) still resolve to
	// the same directory through the logind UID layout.
	return filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
}

func defaultListenSocket() string {
	return filepath.Join(defaultRuntimeDir(), "bitwarden-ssh-agent-wrapper.sock")
}

func defaultUpstreamSocket() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".var", "app", "com.bitwarden.desktop", "data", ".bitwarden-ssh-agent.sock")
}

func defaultControlSocket() string {
	return filepath.Join(defaultRuntimeDir(), "bitwarden-ssh-agent-wrapper-control.sock")
}

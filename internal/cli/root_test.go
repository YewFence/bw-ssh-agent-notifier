package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/config"
)

func TestRootCommand(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	called := false
	command := newRootCommand("test", func(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
		called = true
		if cfg.NotifyBackend != "off" {
			t.Fatalf("NotifyBackend = %q, want off", cfg.NotifyBackend)
		}
		return nil
	})
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--notify", "off"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatalf("runner was not called")
	}
}

func TestRootCommandCompletion(t *testing.T) {
	command := NewRootCommand("test")
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"completion", "bash"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := buffer.String(); !strings.Contains(got, "# bash completion V2 for bwsshntfr") {
		t.Fatalf("completion output missing CLI name:\n%s", got)
	}
}

func TestVersionCommand(t *testing.T) {
	command := NewRootCommand("test")
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"version"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := buffer.String(); got != "bwsshntfr test\n" {
		t.Fatalf("output = %q, want version", got)
	}
}

func TestRunCommand(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	called := false
	command := newRootCommand("test", func(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
		called = true
		if cfg.ListenSocket != "/tmp/listen.sock" {
			t.Fatalf("ListenSocket = %q, want flag value", cfg.ListenSocket)
		}
		return nil
	})
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--listen", "/tmp/listen.sock", "--notify", "off", "run"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatalf("runner was not called")
	}
}

func TestSystemdPrintUserService(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	command := NewRootCommand("test")
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--upstream", "/tmp/bitwarden.sock", "systemd", "print-user-service"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	output := buffer.String()
	if strings.Contains(output, "%h/.local/bin/bwsshntfr") {
		t.Fatalf("service output still contains CLI placeholder:\n%s", output)
	}
	if !strings.Contains(output, "ExecStart=") {
		t.Fatalf("service output missing ExecStart:\n%s", output)
	}
	if !strings.Contains(output, "Environment=BITWARDEN_SSH_AGENT_SOCKET=/tmp/bitwarden.sock") {
		t.Fatalf("service output missing upstream socket:\n%s", output)
	}
}

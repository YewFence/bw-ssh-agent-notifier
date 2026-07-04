package process

import (
	"os"
	"strings"
	"testing"
)

func TestInspectSelf(t *testing.T) {
	info, err := Inspect(os.Getpid(), 1)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if info.PID != os.Getpid() {
		t.Fatalf("PID = %d, want %d", info.PID, os.Getpid())
	}
	if info.Exe == "" {
		t.Fatalf("Exe is empty")
	}
}

func TestParentChain(t *testing.T) {
	chain := ParentChain([]Summary{
		{PID: 1, Exe: "/usr/bin/zsh"},
		{PID: 2, Cmdline: []string{"/usr/bin/foot"}},
	})
	if !strings.Contains(chain, "zsh <- foot") {
		t.Fatalf("ParentChain() = %q", chain)
	}
}

func TestProcessChain(t *testing.T) {
	chain := ProcessChain(
		Summary{PID: 1, Exe: "/usr/bin/ssh"},
		[]Summary{
			{PID: 2, Exe: "/usr/bin/git"},
			{PID: 3, Exe: "/usr/bin/zsh"},
		},
	)
	if chain != "ssh <- git <- zsh" {
		t.Fatalf("ProcessChain() = %q, want ssh <- git <- zsh", chain)
	}
}

func TestCompactProcessChainStopsAfterShell(t *testing.T) {
	chain := CompactProcessChain(
		Summary{PID: 1, Exe: "/usr/bin/ssh"},
		[]Summary{
			{PID: 2, Exe: "/usr/bin/git"},
			{PID: 3, Exe: "/usr/bin/zsh"},
			{PID: 4, Exe: "/usr/bin/ghostty"},
			{PID: 5, Exe: "/usr/lib/systemd/systemd"},
		},
	)
	if chain != "ssh <- git <- zsh" {
		t.Fatalf("CompactProcessChain() = %q, want ssh <- git <- zsh", chain)
	}
}

func TestCompactProcessChainStopsBeforeTerminal(t *testing.T) {
	chain := CompactProcessChain(
		Summary{PID: 1, Exe: "/usr/bin/ssh"},
		[]Summary{
			{PID: 2, Exe: "/usr/bin/git"},
			{PID: 3, Exe: "/usr/bin/ghostty"},
		},
	)
	if chain != "ssh <- git" {
		t.Fatalf("CompactProcessChain() = %q, want ssh <- git", chain)
	}
}

func TestCompactProcessChainStopsBeforeSessionProcess(t *testing.T) {
	chain := CompactProcessChain(
		Summary{PID: 1, Exe: "/usr/bin/ssh"},
		[]Summary{
			{PID: 2, Exe: "/usr/bin/git"},
			{PID: 3, Exe: "/usr/lib/systemd/systemd"},
		},
	)
	if chain != "ssh <- git" {
		t.Fatalf("CompactProcessChain() = %q, want ssh <- git", chain)
	}
}

func TestCompactProcessChainStopsBeforeTerminalMultiplexer(t *testing.T) {
	tests := []struct {
		name   string
		parent Summary
	}{
		{name: "tmux", parent: Summary{PID: 3, Exe: "/usr/bin/tmux"}},
		{name: "screen", parent: Summary{PID: 3, Exe: "/usr/bin/screen"}},
		{name: "tmux server", parent: Summary{PID: 3, Cmdline: []string{"tmux: server"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := CompactProcessChain(
				Summary{PID: 1, Exe: "/usr/bin/ssh"},
				[]Summary{
					{PID: 2, Exe: "/usr/bin/git"},
					tt.parent,
				},
			)
			if chain != "ssh <- git" {
				t.Fatalf("CompactProcessChain() = %q, want ssh <- git", chain)
			}
		})
	}
}

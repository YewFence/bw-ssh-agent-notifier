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

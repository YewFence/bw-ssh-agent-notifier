package process

import "testing"

func TestMemoryChainStripsWrappers(t *testing.T) {
	info := Info{
		PID:     42,
		Cmdline: []string{"ssh", "git@github.com"},
		Parents: []Summary{
			{PID: 41, Exe: "/usr/bin/git", Cmdline: []string{"git", "push", "origin", "main:main"}},
			{PID: 40, Exe: "/usr/bin/zsh", Cmdline: []string{"-zsh"}},
			{PID: 39, Exe: "/usr/bin/zellij", Cmdline: []string{"/usr/bin/zellij"}},
			{PID: 38, Exe: "/usr/bin/zsh", Cmdline: []string{"-zsh"}},
			{PID: 37, Exe: "/usr/bin/foot", Cmdline: []string{"foot"}},
		},
	}

	chain := MemoryChain(info)

	want := []string{"ssh git@github.com", "git push origin main:main"}
	if len(chain) != len(want) {
		t.Fatalf("MemoryChain() = %q, want %q", chain, want)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("MemoryChain()[%d] = %q, want %q", i, chain[i], want[i])
		}
	}
}

func TestMemoryChainKeepsPayloadAncestors(t *testing.T) {
	info := Info{
		PID:     42,
		Cmdline: []string{"ssh", "git@github.com"},
		Parents: []Summary{
			{PID: 41, Exe: "/usr/bin/git", Cmdline: []string{"git", "push", "--progress", "origin", "main:main"}},
			{PID: 40, Exe: "/usr/bin/jj", Cmdline: []string{"jj", "git", "push", "-b", "main"}},
		},
	}

	chain := MemoryChain(info)

	want := []string{
		"ssh git@github.com",
		"git push --progress origin main:main",
		"jj git push -b main",
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("MemoryChain()[%d] = %q, want %q", i, chain[i], want[i])
		}
	}
}

func TestMemoryChainDistinguishesBranches(t *testing.T) {
	base := Info{
		PID:     42,
		Cmdline: []string{"ssh", "git@github.com"},
		Parents: []Summary{
			{PID: 41, Exe: "/usr/bin/git", Cmdline: []string{"git", "push", "origin", "main:main"}},
		},
	}
	wrongBranch := base
	wrongBranch.Parents = []Summary{
		{PID: 41, Exe: "/usr/bin/git", Cmdline: []string{"git", "push", "origin", "wrong:wrong"}},
	}

	left := MemoryChain(base)
	right := MemoryChain(wrongBranch)
	if left[1] == right[1] {
		t.Fatalf("branch names collapsed: %q", left[1])
	}
}

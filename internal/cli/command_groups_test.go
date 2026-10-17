package cli

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/config"
)

func TestCommandGroupFlagsOverrideEnvAndAcceptRepeatedGroups(t *testing.T) {
	t.Setenv("WRAPPER_GATE_COMMAND_GROUPS", `[["old-command"]]`)
	called := false
	command := newRootCommand("test", func(_ context.Context, cfg config.Config, _ *slog.Logger) error {
		called = true
		want := [][]string{{"bd", "sync"}, {"jj", "git", "push"}}
		if !reflect.DeepEqual(cfg.GateCommandGroups, want) {
			t.Fatalf("groups = %v, want %v", cfg.GateCommandGroups, want)
		}
		return nil
	})
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"run", "--gate-command-group", `["bd","sync"]`, "--gate-command-group", `["jj","git","push"]`})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("runner was not called")
	}
}

func TestCommandGroupFlagsRejectInvalidGroups(t *testing.T) {
	for _, value := range []string{`bd sync`, `[]`, `[""]`, `["./bd"]`, `["bd",1]`} {
		command := newRootCommand("test", func(_ context.Context, _ config.Config, _ *slog.Logger) error {
			t.Fatal("runner must not be called with invalid groups")
			return nil
		})
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs([]string{"run", "--gate-command-group", value})
		if err := command.Execute(); err == nil {
			t.Fatalf("expected error for command group %q", value)
		}
	}
}
